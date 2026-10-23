package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/justindeelux/gotham/agent/tools"
	"github.com/justindeelux/gotham/buildtool"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultBuildContextLimit = int64(512 << 20)
	defaultDockerfile        = "Dockerfile"
	imageRepoPrefix          = "gotham/"
	maxBuildMetaRefLen       = 128
)

type BuildClient interface {
	EnsureRegistry(ctx context.Context) (string, error)
	Build(ctx context.Context, opts BuildOptions, emit func([]byte) error) error
	RunToolchain(ctx context.Context, engine string, context io.Reader, tag string, buildArgs map[string]string, emit func([]byte) error) error
	TagImage(ctx context.Context, source, repository, tag string) error
	PushImage(ctx context.Context, repository, tag string, emit func([]byte) error) error
	ImageDigest(ctx context.Context, ref string) (string, error)
	PruneAppImages(ctx context.Context, appID, keepDeploy string) error
}

type BuildServer struct {
	agentv1.UnimplementedBuildServiceServer
	docker          BuildClient
	log             *slog.Logger
	maxContextBytes int64
	toolUse         *tools.ToolUseTracker
}

type BuildServerOption func(*BuildServer)

func WithToolUseTracker(tracker *tools.ToolUseTracker) BuildServerOption {
	return func(s *BuildServer) { s.toolUse = tracker }
}

func NewBuildServer(docker BuildClient, log *slog.Logger, options ...BuildServerOption) *BuildServer {
	if log == nil {
		log = slog.Default()
	}
	server := &BuildServer{docker: docker, log: log, maxContextBytes: defaultBuildContextLimit}
	for _, option := range options {
		option(server)
	}
	return server
}

func (s *BuildServer) BuildImage(stream agentv1.BuildService_BuildImageServer) error {
	ctx := stream.Context()

	if s.toolUse != nil {
		if err := s.toolUse.BeginBuild(); err != nil {
			return status.Errorf(codes.Unavailable, "build refused: %v", err)
		}
		defer s.toolUse.EndBuild()
	}

	first, err := stream.Recv()
	if err != nil {
		return receiveError(err, "build stream is missing build meta")
	}
	meta := first.GetMeta()
	if meta == nil {
		return status.Error(codes.InvalidArgument, "first request must carry build meta")
	}
	if err := validateBuildMeta(meta); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	contextFile, err := os.CreateTemp("", "gotham-build-*.tar")
	if err != nil {
		return status.Errorf(codes.Internal, "create build context file: %v", err)
	}
	defer func() {
		_ = contextFile.Close()
		_ = os.Remove(contextFile.Name())
	}()

	written, err := s.receiveContext(stream, contextFile)
	if err != nil {
		return err
	}
	if _, err := contextFile.Seek(0, io.SeekStart); err != nil {
		return status.Errorf(codes.Internal, "rewind build context: %v", err)
	}

	var sendErr error
	emit := func(data []byte) error {
		if sendErr != nil {
			return sendErr
		}
		if err := stream.Send(buildLogResponse(data)); err != nil {
			sendErr = err
			return err
		}
		return nil
	}
	fail := func(err error) error {
		if sendErr != nil {
			return sendErr
		}
		return dockerError("build image", err)
	}

	imageTag := imageRepoPrefix + meta.GetAppId() + ":" + meta.GetDeployId()

	registryAddr, err := s.docker.EnsureRegistry(ctx)
	if err != nil {
		return fail(err)
	}
	registryImage := registryAddr + "/" + imageTag
	if err := emit([]byte(fmt.Sprintf("using node registry %s\n", registryAddr))); err != nil {
		return sendErr
	}

	if engine := normalizeEngine(meta.GetEngine()); engine != "" {
		if err := emit([]byte("running " + engine + " build on the node\n")); err != nil {
			return sendErr
		}
		if err := s.docker.RunToolchain(ctx, engine, contextFile, registryImage, meta.GetBuildArgs(), emit); err != nil {
			return fail(err)
		}
	} else if err := s.docker.Build(ctx, BuildOptions{
		Tag:        registryImage,
		Dockerfile: dockerfilePath(meta),
		BuildArgs:  meta.GetBuildArgs(),
		Context:    contextFile,
	}, emit); err != nil {
		return fail(err)
	}

	if err := s.docker.TagImage(ctx, registryImage, imageRepoPrefix+meta.GetAppId(), meta.GetDeployId()); err != nil {
		return fail(err)
	}

	if err := emit([]byte("pushing " + registryImage + "\n")); err != nil {
		return sendErr
	}
	if err := s.docker.PushImage(ctx, stripImageTag(registryImage), meta.GetDeployId(), emit); err != nil {
		return fail(err)
	}

	digest, err := s.docker.ImageDigest(ctx, registryImage)
	if err != nil {
		return fail(err)
	}

	if err := s.docker.PruneAppImages(ctx, meta.GetAppId(), meta.GetDeployId()); err != nil {
		s.log.Warn("image retention failed",
			slog.String("app", meta.GetAppId()),
			slog.String("error", err.Error()))
	}

	if err := stream.Send(&agentv1.BuildImageResponse{
		Event: &agentv1.BuildImageResponse_Result{
			Result: &agentv1.BuildImageResult{
				ImageTag:      imageTag,
				RegistryImage: registryImage,
				Digest:        digest,
				RegistryAddr:  registryAddr,
			},
		},
	}); err != nil {
		return err
	}

	s.log.Info("image built",
		slog.String("image", registryImage),
		slog.String("digest", digest),
		slog.Int64("context_bytes", written),
	)
	return nil
}

func (s *BuildServer) receiveContext(stream agentv1.BuildService_BuildImageServer, dst io.Writer) (int64, error) {
	var written int64
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return written, nil
		}
		if err != nil {
			return written, status.Convert(err).Err()
		}
		if request.GetMeta() != nil {
			return written, status.Error(codes.InvalidArgument, "build meta must be the first request")
		}
		chunk := request.GetContextChunk()
		if len(chunk) == 0 {
			continue
		}
		written += int64(len(chunk))
		if written > s.maxContextBytes {
			return written, status.Errorf(codes.InvalidArgument, "build context exceeds %d bytes", s.maxContextBytes)
		}
		if _, err := dst.Write(chunk); err != nil {
			return written, status.Errorf(codes.Internal, "write build context: %v", err)
		}
	}
}

func validateBuildMeta(meta *agentv1.BuildMeta) error {
	if err := validateTagSegment("app_id", meta.GetAppId()); err != nil {
		return err
	}
	if err := validateTagSegment("deploy_id", meta.GetDeployId()); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(meta.GetEngine())) {
	case "", "dockerfile", "static", string(buildtool.Railpack), string(buildtool.Buildpacks):
		return nil
	default:
		return fmt.Errorf("engine %q is not supported", meta.GetEngine())
	}
}

func normalizeEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case string(buildtool.Railpack):
		return string(buildtool.Railpack)
	case string(buildtool.Buildpacks):
		return string(buildtool.Buildpacks)
	default:
		return ""
	}
}

func validateTagSegment(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > maxBuildMetaRefLen {
		return fmt.Errorf("%s exceeds %d bytes", field, maxBuildMetaRefLen)
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
		default:
			return fmt.Errorf("%s must only contain letters, digits, dot, underscore and dash", field)
		}
	}
	return nil
}

func dockerfilePath(meta *agentv1.BuildMeta) string {
	if path := meta.GetDockerfile(); path != "" {
		return path
	}
	return defaultDockerfile
}

func buildLogResponse(data []byte) *agentv1.BuildImageResponse {
	return &agentv1.BuildImageResponse{
		Event: &agentv1.BuildImageResponse_Log{
			Log: &agentv1.BuildLogChunk{Data: data},
		},
	}
}

func receiveError(err error, fallback string) error {
	if errors.Is(err, io.EOF) {
		return status.Error(codes.InvalidArgument, fallback)
	}
	return status.Convert(err).Err()
}
