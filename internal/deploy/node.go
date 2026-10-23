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
	ComposeUp(ctx context.Context, projectName string, composeYAML []byte) error
	ComposeValidate(ctx context.Context, projectName string, composeYAML []byte) (services []string, enforced bool, err error)
	ComposeDown(ctx context.Context, projectName string, composeYAML []byte) error
	ComposePs(ctx context.Context, projectName string) ([]ComposeContainer, error)
	Run(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	Stop(ctx context.Context, containerID string) error
	Start(ctx context.Context, containerID string) error
	Remove(ctx context.Context, containerID string) error
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

func (n *agentNode) ComposeUp(ctx context.Context, projectName string, composeYAML []byte) error {
	if strings.TrimSpace(projectName) == "" {
		return fmt.Errorf("%w: compose project name is required", ErrValidation)
	}
	if len(composeYAML) == 0 {
		return fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	_, err := n.compose.ComposeUp(ctx, &agentv1.ComposeUpRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
		Unconfined:  false,
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

func (n *agentNode) ComposeDown(ctx context.Context, projectName string, composeYAML []byte) error {
	if strings.TrimSpace(projectName) == "" {
		return fmt.Errorf("%w: compose project name is required", ErrValidation)
	}
	if len(composeYAML) == 0 {
		return fmt.Errorf("%w: compose document is required", ErrValidation)
	}
	_, err := n.compose.ComposeDown(ctx, &agentv1.ComposeDownRequest{
		ProjectName: projectName,
		ComposeYaml: composeYAML,
	})
	if err != nil {
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
	}, contextTar, b.log)
	if err != nil {
		return builds.ImageBuildResult{}, err
	}
	b.outcome = outcome
	b.built = true
	return builds.ImageBuildResult{Digest: outcome.Digest}, nil
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
