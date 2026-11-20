package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const buildChunkSize = 64 << 10

type BuildMeta struct {
	AppID      string
	DeployID   string
	Dockerfile string
	Engine     string
	BuildArgs  map[string]string
	Target     string
	// Tag overrides {deploy_id} in gotham/{app_id}:{deploy_id}.
	Tag string
}

type BuildOutcome struct {
	ImageTag      string
	RegistryImage string
	Digest        string
	RegistryAddr  string
}

type ComposeContainer struct {
	ID      string
	Service string
	Name    string
	Image   string
	State   string
	Status  string
}

type Node interface {
	Build(ctx context.Context, meta BuildMeta, contextTar []byte, log func([]byte)) (BuildOutcome, error)
	Pull(ctx context.Context, image string) error
	PullWithAuth(ctx context.Context, image, username, password string) (string, error)
	ComposeUp(ctx context.Context, projectName string, composeYAML []byte, deploymentID string) error
	ComposeValidate(ctx context.Context, projectName string, composeYAML []byte) (services []string, enforced bool, err error)
	ComposeDown(ctx context.Context, projectName string, composeYAML []byte, removeVolumes bool) ([]string, error)
	ComposePs(ctx context.Context, projectName string) ([]ComposeContainer, error)
	Run(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	Stop(ctx context.Context, containerID string) error
	Start(ctx context.Context, containerID string) error
	Remove(ctx context.Context, containerID string) error
	RemoveVolume(ctx context.Context, volumeName string) error
	Containers(ctx context.Context) ([]*agentv1.ContainerInfo, error)
	ContainersByLabel(ctx context.Context, labels map[string]string, all, resolveVolumes bool) ([]*agentv1.ContainerInfo, error)
	FileRoots(ctx context.Context) ([]*agentv1.FileRoot, error)
	Close() error
}

type DialFunc func(ctx context.Context, serverID uuid.UUID) (Node, error)

type AgentDialer interface {
	DialDockerClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.DockerClient, error)
}

func AgentDial(dialer AgentDialer) DialFunc {
	return func(ctx context.Context, serverID uuid.UUID) (Node, error) {
		client, err := dialer.DialDockerClient(ctx, serverID)
		if err != nil {
			if errors.Is(err, servers.ErrNotFound) {
				return nil, fmt.Errorf("%w: %v", ErrServerNotFound, err)
			}
			return nil, mapRPCError(err)
		}
		if client == nil {
			return nil, fmt.Errorf("%w: dial returned no client", ErrAgentUnavailable)
		}
		return newAgentNode(client), nil
	}
}

type agentNode struct {
	docker  *servers.DockerClient
	build   agentv1.BuildServiceClient
	compose agentv1.ComposeServiceClient
	files   agentv1.FileServiceClient
}

var _ Node = (*agentNode)(nil)

func newAgentNode(client *servers.DockerClient) *agentNode {
	return &agentNode{
		docker:  client,
		build:   agentv1.NewBuildServiceClient(client.Conn()),
		compose: agentv1.NewComposeServiceClient(client.Conn()),
		files:   agentv1.NewFileServiceClient(client.Conn()),
	}
}

func (n *agentNode) Build(ctx context.Context, meta BuildMeta, contextTar []byte, log func([]byte)) (BuildOutcome, error) {
	stream, err := n.build.BuildImage(ctx)
	if err != nil {
		return BuildOutcome{}, mapRPCError(err)
	}

	sendErr := make(chan error, 1)
	go func() {
		sendErr <- n.sendBuild(stream, meta, contextTar)
	}()

	var outcome BuildOutcome
	var result *agentv1.BuildImageResult
	for {
		response, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			<-sendErr
			return BuildOutcome{}, mapRPCError(err)
		}
		switch event := response.GetEvent().(type) {
		case *agentv1.BuildImageResponse_Log:
			if log != nil {
				log(event.Log.GetData())
			}
		case *agentv1.BuildImageResponse_Result:
			result = event.Result
		}
	}
	if err := <-sendErr; err != nil {
		return BuildOutcome{}, err
	}
	if result == nil {
		return BuildOutcome{}, fmt.Errorf("%w: build closed without a result", ErrAgentUnavailable)
	}
	outcome = BuildOutcome{
		ImageTag:      result.GetImageTag(),
		RegistryImage: result.GetRegistryImage(),
		Digest:        result.GetDigest(),
		RegistryAddr:  result.GetRegistryAddr(),
	}
	return outcome, nil
}

func (n *agentNode) sendBuild(stream grpc.BidiStreamingClient[agentv1.BuildImageRequest, agentv1.BuildImageResponse], meta BuildMeta, contextTar []byte) error {
	err := stream.Send(&agentv1.BuildImageRequest{
		Part: &agentv1.BuildImageRequest_Meta{Meta: &agentv1.BuildMeta{
			AppId:      meta.AppID,
			DeployId:   meta.DeployID,
			Dockerfile: meta.Dockerfile,
			Engine:     meta.Engine,
			BuildArgs:  meta.BuildArgs,
			Target:     meta.Target,
			Tag:        meta.Tag,
		}},
	})
	if err != nil {
		return mapRPCError(err)
	}
	for offset := 0; offset < len(contextTar); offset += buildChunkSize {
		end := offset + buildChunkSize
		if end > len(contextTar) {
			end = len(contextTar)
		}
		if err := stream.Send(&agentv1.BuildImageRequest{
			Part: &agentv1.BuildImageRequest_ContextChunk{ContextChunk: contextTar[offset:end]},
		}); err != nil {
			return mapRPCError(err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (n *agentNode) Pull(ctx context.Context, image string) error {
	if strings.TrimSpace(image) == "" {
		return fmt.Errorf("%w: image is required", ErrValidation)
	}
	if _, err := n.docker.PullImage(ctx, &agentv1.PullImageRequest{Image: image}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (n *agentNode) PullWithAuth(ctx context.Context, image, username, password string) (string, error) {
	if strings.TrimSpace(image) == "" {
		return "", fmt.Errorf("%w: image is required", ErrValidation)
	}
	response, err := n.docker.PullImage(ctx, &agentv1.PullImageRequest{
		Image:    image,
		Username: username,
		Password: password,
	})
	if err != nil {
		return "", mapRPCError(err)
	}
	return response.GetDigest(), nil
}

func (n *agentNode) ComposeUp(ctx context.Context, projectName string, composeYAML []byte, deploymentID string) error {
	if strings.TrimSpace(projectName) == "" {
		return fmt.Errorf("%w: compose project name is required", ErrValidation)
	}
	if len(composeYAML) == 0 {
		return fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	_, err := n.compose.ComposeUp(ctx, &agentv1.ComposeUpRequest{
		ProjectName:  projectName,
		ComposeYaml:  composeYAML,
		Unconfined:   false,
		DeploymentId: deploymentID,
	})
	if err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (n *agentNode) ComposePs(ctx context.Context, projectName string) ([]ComposeContainer, error) {
	if strings.TrimSpace(projectName) == "" {
		return nil, fmt.Errorf("%w: compose project name is required", ErrValidation)
	}
	response, err := n.compose.ComposePs(ctx, &agentv1.ComposePsRequest{ProjectName: projectName})
	if err != nil {
		return nil, mapRPCError(err)
	}
	containers := make([]ComposeContainer, 0, len(response.GetContainers()))
	for _, container := range response.GetContainers() {
		containers = append(containers, ComposeContainer{
			ID:      container.GetContainerId(),
			Service: container.GetService(),
			Name:    container.GetName(),
			Image:   container.GetImage(),
			State:   container.GetState(),
			Status:  container.GetStatus(),
		})
	}
	return containers, nil
}

func (n *agentNode) ComposeValidate(ctx context.Context, projectName string, composeYAML []byte) ([]string, bool, error) {
	if strings.TrimSpace(projectName) == "" {
		return nil, false, fmt.Errorf("%w: compose project name is required", ErrValidation)
	}
	if len(composeYAML) == 0 {
		return nil, false, fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	response, err := n.compose.ComposeValidate(ctx, &agentv1.ComposeValidateRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
		Unconfined:  false,
	})
	if err != nil {
		return nil, false, mapRPCError(err)
	}
	return response.GetServices(), response.GetConfinedEnforced(), nil
}

func (n *agentNode) ComposeDown(ctx context.Context, projectName string, composeYAML []byte, removeVolumes bool) ([]string, error) {
	if strings.TrimSpace(projectName) == "" {
		return nil, fmt.Errorf("%w: compose project name is required", ErrValidation)
	}
	if len(composeYAML) == 0 {
		return nil, fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	response, err := n.compose.ComposeDown(ctx, &agentv1.ComposeDownRequest{
		ProjectName:   projectName,
		ComposeYaml:   composeYAML,
		RemoveVolumes: removeVolumes,
	})
	if err != nil {
		return nil, mapRPCError(err)
	}
	return response.GetVolumeWarnings(), nil
}

func (n *agentNode) RemoveVolume(ctx context.Context, volumeName string) error {
	if strings.TrimSpace(volumeName) == "" {
		return fmt.Errorf("%w: volume name is required", ErrValidation)
	}
	if _, err := n.docker.RemoveVolume(ctx, &agentv1.VolumeActionRequest{Name: volumeName}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (n *agentNode) Run(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error) {
	response, err := n.docker.RunImage(ctx, req)
	if err != nil {
		return "", mapRPCError(err)
	}
	return response.GetContainerId(), nil
}

func (n *agentNode) Stop(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	if _, err := n.docker.StopContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (n *agentNode) Start(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	if _, err := n.docker.StartContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (n *agentNode) Remove(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	if _, err := n.docker.RemoveContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

func (n *agentNode) Containers(ctx context.Context) ([]*agentv1.ContainerInfo, error) {
	response, err := n.docker.ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		return nil, mapRPCError(err)
	}
	return response.GetContainers(), nil
}

func (n *agentNode) ContainersByLabel(ctx context.Context, labels map[string]string, all, resolveVolumes bool) ([]*agentv1.ContainerInfo, error) {
	response, err := n.docker.ListContainers(ctx, &agentv1.ListContainersRequest{
		Labels:               labels,
		All:                  all,
		ResolveVolumeDevices: resolveVolumes,
	})
	if err != nil {
		return nil, mapRPCError(err)
	}
	return response.GetContainers(), nil
}

func (n *agentNode) FileRoots(ctx context.Context) ([]*agentv1.FileRoot, error) {
	response, err := n.files.ListRoots(ctx, &agentv1.ListRootsRequest{})
	if err != nil {
		return nil, err
	}
	return response.GetRoots(), nil
}

// ContainerFileNode is the slice of a node a container's file manager needs.
// The concrete agentNode implements it; a node that predates the RPCs does not
// answer them, so the control plane reports that the agent must be updated.
type ContainerFileNode interface {
	ContainerFileCapabilities(ctx context.Context, containerID string) (ContainerFileCapabilities, error)
	ListContainerFiles(ctx context.Context, containerID, path, cursor string) (ContainerFileListing, error)
	StatContainerFile(ctx context.Context, containerID, path string) (ContainerFileEntry, error)
	ReadContainerFile(ctx context.Context, containerID, path string, archive bool, dst io.Writer) error
	WriteContainerFile(ctx context.Context, containerID, path string, size int64, src io.Reader) error
	ChangeContainerFile(ctx context.Context, containerID string, change ContainerFileChange) error
}

var _ ContainerFileNode = (*agentNode)(nil)

func (n *agentNode) ContainerFileCapabilities(ctx context.Context, containerID string) (ContainerFileCapabilities, error) {
	response, err := n.docker.GetContainerFileCapabilities(ctx, &agentv1.ContainerFilesCapabilitiesRequest{ContainerId: containerID})
	if err != nil {
		return ContainerFileCapabilities{}, containerFileRPCError(err)
	}
	return ContainerFileCapabilities{
		Readable:      response.GetReadable(),
		ExecAvailable: response.GetExecAvailable(),
		ExecBlockedBy: response.GetExecBlockedBy(),
		State:         response.GetState(),
	}, nil
}

func (n *agentNode) ListContainerFiles(ctx context.Context, containerID, path, cursor string) (ContainerFileListing, error) {
	response, err := n.docker.ListContainerFiles(ctx, &agentv1.ContainerFilesListRequest{
		ContainerId: containerID,
		Path:        path,
		Cursor:      cursor,
	})
	if err != nil {
		return ContainerFileListing{}, containerFileRPCError(err)
	}
	return ContainerFileListing{
		Directory: containerFileEntryFromProto(response.GetDirectory()),
		Entries:   containerFileEntriesFromProto(response.GetEntries()),
		Truncated: response.GetTruncated(),
		Cursor:    response.GetCursor(),
	}, nil
}

func (n *agentNode) StatContainerFile(ctx context.Context, containerID, path string) (ContainerFileEntry, error) {
	response, err := n.docker.StatContainerFile(ctx, &agentv1.ContainerFilesPathRequest{ContainerId: containerID, Path: path})
	if err != nil {
		return ContainerFileEntry{}, containerFileRPCError(err)
	}
	return containerFileEntryFromProto(response.GetEntry()), nil
}

func (n *agentNode) ReadContainerFile(ctx context.Context, containerID, path string, archive bool, dst io.Writer) error {
	stream, err := n.docker.ReadContainerFile(ctx, &agentv1.ContainerFilesReadRequest{
		ContainerId: containerID,
		Path:        path,
		Archive:     archive,
	})
	if err != nil {
		return containerFileRPCError(err)
	}
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			return nil
		}
		if recvErr != nil {
			return containerFileRPCError(recvErr)
		}
		if data := chunk.GetData(); len(data) > 0 {
			if _, writeErr := dst.Write(data); writeErr != nil {
				return writeErr
			}
		}
	}
}

func (n *agentNode) WriteContainerFile(ctx context.Context, containerID, path string, size int64, src io.Reader) error {
	stream, err := n.docker.WriteContainerFile(ctx)
	if err != nil {
		return containerFileRPCError(err)
	}
	if err := stream.Send(&agentv1.ContainerFilesWriteRequest{
		Part: &agentv1.ContainerFilesWriteRequest_Header{Header: &agentv1.ContainerFilesWriteHeader{
			ContainerId: containerID,
			Path:        path,
			Size:        size,
			Uid:         -1,
			Gid:         -1,
		}},
	}); err != nil {
		return containerFileRPCError(err)
	}
	buffer := make([]byte, buildChunkSize)
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			if err := stream.Send(&agentv1.ContainerFilesWriteRequest{
				Part: &agentv1.ContainerFilesWriteRequest_Data{Data: buffer[:n]},
			}); err != nil {
				return containerFileRPCError(err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("deploy: read the upload: %w", readErr)
		}
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		return containerFileRPCError(err)
	}
	return nil
}

func (n *agentNode) ChangeContainerFile(ctx context.Context, containerID string, change ContainerFileChange) error {
	action, ok := containerFileActionProto(change.Action)
	if !ok {
		return fmt.Errorf("%w: unknown file action %q", ErrValidation, change.Action)
	}
	_, err := n.docker.ChangeContainerFile(ctx, &agentv1.ContainerFilesChangeRequest{
		ContainerId: containerID,
		Action:      action,
		Path:        change.Path,
		Destination: change.Destination,
		Mode:        change.Mode,
		Overwrite:   change.Overwrite,
	})
	return containerFileRPCError(err)
}

func containerFileActionProto(action ContainerFileAction) (agentv1.ContainerFileAction, bool) {
	switch action {
	case ContainerFileActionMkdir:
		return agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MKDIR, true
	case ContainerFileActionRename:
		return agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_RENAME, true
	case ContainerFileActionMove:
		return agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_MOVE, true
	case ContainerFileActionCopy:
		return agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_COPY, true
	case ContainerFileActionDelete:
		return agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_DELETE, true
	case ContainerFileActionChmod:
		return agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_CHMOD, true
	default:
		return agentv1.ContainerFileAction_CONTAINER_FILE_ACTION_UNSPECIFIED, false
	}
}

func containerFileEntryFromProto(entry *agentv1.ContainerFileEntry) ContainerFileEntry {
	converted := ContainerFileEntry{
		Path:          entry.GetPath(),
		Name:          entry.GetName(),
		Kind:          containerFileKind(entry.GetKind()),
		Size:          entry.GetSize(),
		Mode:          entry.GetMode(),
		Uid:           entry.GetUid(),
		Gid:           entry.GetGid(),
		SymlinkTarget: entry.GetSymlinkTarget(),
	}
	if modified := entry.GetModifiedAt(); modified != nil {
		at := modified.AsTime()
		converted.ModifiedAt = &at
	}
	return converted
}

func containerFileEntriesFromProto(entries []*agentv1.ContainerFileEntry) []ContainerFileEntry {
	converted := make([]ContainerFileEntry, 0, len(entries))
	for _, entry := range entries {
		converted = append(converted, containerFileEntryFromProto(entry))
	}
	return converted
}

func containerFileKind(kind agentv1.FileKind) string {
	switch kind {
	case agentv1.FileKind_FILE_KIND_DIRECTORY:
		return "directory"
	case agentv1.FileKind_FILE_KIND_SYMLINK:
		return "symlink"
	case agentv1.FileKind_FILE_KIND_SPECIAL:
		return "special"
	default:
		return "file"
	}
}

func containerFileRPCError(err error) error {
	if err == nil {
		return nil
	}
	message := status.Convert(err).Message()
	switch status.Code(err) {
	case codes.AlreadyExists, codes.FailedPrecondition:
		return fmt.Errorf("%w: %s", ErrConflict, message)
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %s", ErrValidation, message)
	case codes.ResourceExhausted:
		return fmt.Errorf("%w: %s", ErrTooLarge, message)
	case codes.Unimplemented:
		return fmt.Errorf("%w: this node agent does not support container files; update the node agent", ErrAgentUnavailable)
	default:
		return mapRPCError(err)
	}
}

func (n *agentNode) Close() error { return n.docker.Close() }

type nodeImageBuilder struct {
	node     Node
	appID    uuid.UUID
	deployID uuid.UUID
	log      func([]byte)
	outcome  BuildOutcome
	built    bool
}

var _ builds.ImageBuilder = (*nodeImageBuilder)(nil)

func newNodeBuilder(node Node, appID, deployID uuid.UUID, log func([]byte)) *nodeImageBuilder {
	return &nodeImageBuilder{node: node, appID: appID, deployID: deployID, log: log}
}

func (b *nodeImageBuilder) Build(ctx context.Context, contextTar []byte, opts builds.ImageBuildOptions) (builds.ImageBuildResult, error) {
	if b.node == nil {
		return builds.ImageBuildResult{}, fmt.Errorf("%w: no node configured for the build", ErrAgentUnavailable)
	}
	outcome, err := b.node.Build(ctx, BuildMeta{
		AppID:      b.appID.String(),
		DeployID:   b.deployID.String(),
		Dockerfile: opts.Dockerfile,
		Engine:     string(opts.Engine),
		BuildArgs:  opts.BuildArgs,
		Target:     opts.Target,
		Tag:        imageTagSuffix(opts.Tag),
	}, contextTar, b.log)
	if err != nil {
		return builds.ImageBuildResult{}, err
	}
	b.outcome = outcome
	b.built = true
	return builds.ImageBuildResult{Digest: outcome.Digest}, nil
}

// imageTagSuffix keeps the tag part of gotham/{app}:{tag}: the agent builds
// the reference from the app and deploy ids itself and only accepts a tag.
func imageTagSuffix(ref string) string {
	index := strings.LastIndex(ref, ":")
	if index < 0 {
		return ref
	}
	return ref[index+1:]
}

func (b *nodeImageBuilder) outcomeOf() (BuildOutcome, bool) {
	if b == nil || !b.built {
		return BuildOutcome{}, false
	}
	return b.outcome, true
}

func mapRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.ResourceExhausted:
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %v", ErrValidation, err)
	case codes.NotFound:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	default:
		return err
	}
}
