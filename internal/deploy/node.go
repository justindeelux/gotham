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

// buildChunkSize is the build context payload size sent per BuildImage
// message. Small enough for the default gRPC window, large enough to keep the
// stream efficient.
const buildChunkSize = 64 << 10

// BuildMeta mirrors agentv1.BuildMeta for the Node seam so the interface
// stays independent of the generated oneof plumbing.
type BuildMeta struct {
	AppID      string
	DeployID   string
	Dockerfile string
	// Engine names the build engine the node must run. Empty and "dockerfile"
	// mean a Dockerfile build; "railpack"/"buildpacks" run that toolchain on
	// the node.
	Engine    string
	BuildArgs map[string]string
}

// BuildOutcome is the result of a build performed on a node.
type BuildOutcome struct {
	ImageTag      string
	RegistryImage string
	Digest        string
	RegistryAddr  string
}

// Node is the subset of the node agent the orchestrator uses: build the
// image, confirm it in the node registry, run the container and read its
// state back for the healthcheck. Production is agentNode over the mTLS
// DockerService/BuildService clients; tests substitute a mock.
type Node interface {
	// Build streams a build context to the agent's BuildImage RPC, forwarding
	// raw log chunks to log as they arrive.
	Build(ctx context.Context, meta BuildMeta, contextTar []byte, log func([]byte)) (BuildOutcome, error)
	// Pull confirms an image is reachable from the node's registry.
	Pull(ctx context.Context, image string) error
	// Run creates and starts a container, returning its ID.
	Run(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	// Stop stops a running container (used to retire the previous release and
	// for the manual stop endpoint).
	Stop(ctx context.Context, containerID string) error
	// Start starts a stopped container (manual start endpoint).
	Start(ctx context.Context, containerID string) error
	// Remove deletes a container, forcing a stop when it still runs. Anonymous
	// volumes go with it; named volumes and host bind directories are preserved
	// (the agent's Remove contract), so deleting a resource keeps its data.
	Remove(ctx context.Context, containerID string) error
	// Containers lists every container on the node, including stopped ones.
	Containers(ctx context.Context) ([]*agentv1.ContainerInfo, error)
	// Close releases the underlying agent connection.
	Close() error
}

// DialFunc opens the agent of the given server. It is satisfied by
// AgentDial over *servers.ServerService in production.
type DialFunc func(ctx context.Context, serverID uuid.UUID) (Node, error)

// AgentDialer is the mTLS dial implemented by *servers.ServerService. The
// HTTP layer type-asserts its server registry to this interface, so tests
// that pass a fake registry simply leave the deploy dialer unwired.
type AgentDialer interface {
	DialDockerClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.DockerClient, error)
}

// AgentDial adapts an AgentDialer to DialFunc. Each call owns its connection
// and closes it when the deployment run ends.
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

// agentNode is the production Node over one mTLS agent connection.
type agentNode struct {
	docker *servers.DockerClient
	build  agentv1.BuildServiceClient
}

// Compile-time guarantee that agentNode satisfies the Node seam.
var _ Node = (*agentNode)(nil)

// newAgentNode binds an open agent connection to both services it exposes:
// DockerService (the eight container RPCs) and BuildService (BuildImage).
func newAgentNode(client *servers.DockerClient) *agentNode {
	return &agentNode{
		docker: client,
		build:  agentv1.NewBuildServiceClient(client.Conn()),
	}
}

// Build implements Node by streaming the build parameters, the context
// tarball and the half-close on one goroutine while the response stream is
// consumed on another, so build logs keep flowing while the context uploads.
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

// sendBuild writes the meta message first, then the context tarball in
// chunks, then half-closes the send side to tell the agent the upload is done.
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

// Pull implements Node.
func (n *agentNode) Pull(ctx context.Context, image string) error {
	if strings.TrimSpace(image) == "" {
		return fmt.Errorf("%w: image is required", ErrValidation)
	}
	if _, err := n.docker.PullImage(ctx, &agentv1.PullImageRequest{Image: image}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

// Run implements Node.
func (n *agentNode) Run(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error) {
	response, err := n.docker.RunImage(ctx, req)
	if err != nil {
		return "", mapRPCError(err)
	}
	return response.GetContainerId(), nil
}

// Stop implements Node.
func (n *agentNode) Stop(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	if _, err := n.docker.StopContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

// Start implements Node.
func (n *agentNode) Start(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	if _, err := n.docker.StartContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

// Remove implements Node. The agent's Remove is idempotent: a container that is
// already gone is reported as success, so a retried cleanup is safe.
func (n *agentNode) Remove(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	if _, err := n.docker.RemoveContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

// Containers implements Node.
func (n *agentNode) Containers(ctx context.Context) ([]*agentv1.ContainerInfo, error) {
	response, err := n.docker.ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		return nil, mapRPCError(err)
	}
	return response.GetContainers(), nil
}

// Close implements Node.
func (n *agentNode) Close() error { return n.docker.Close() }

// nodeImageBuilder adapts Node.Build to the builds.ImageBuilder seam, so the
// four build engines stream their context to the node unchanged. One instance
// belongs to a single deployment run: it captures the registry reference the
// node reported for that run.
type nodeImageBuilder struct {
	node     Node
	appID    uuid.UUID
	deployID uuid.UUID
	log      func([]byte)
	outcome  BuildOutcome
	built    bool
}

// Compile-time guarantee that nodeImageBuilder satisfies the seam.
var _ builds.ImageBuilder = (*nodeImageBuilder)(nil)

// newNodeBuilder binds a builder to one run of one deployment.
func newNodeBuilder(node Node, appID, deployID uuid.UUID, log func([]byte)) *nodeImageBuilder {
	return &nodeImageBuilder{node: node, appID: appID, deployID: deployID, log: log}
}

// Build implements builds.ImageBuilder. Log output is forwarded live to the
// deploy log stream, so ImageBuildResult.Logs stays empty and engines that
// write result.Logs to their LogWriter do not duplicate it.
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

// outcomeOf returns the node-reported registry reference of the last
// successful build, if this builder performed one.
func (b *nodeImageBuilder) outcomeOf() (BuildOutcome, bool) {
	if b == nil || !b.built {
		return BuildOutcome{}, false
	}
	return b.outcome, true
}

// mapRPCError translates agent transport failures to deploy sentinels:
// Unavailable/DeadlineExceeded/Canceled mean the agent could not serve the
// call (the only retryable class), InvalidArgument means bad input, NotFound
// means a missing Docker resource, and every other status is a terminal
// failure. Unrecognised errors pass through unchanged.
func mapRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %v", ErrValidation, err)
	case codes.NotFound:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	default:
		return err
	}
}
