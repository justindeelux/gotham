package agent

import (
	"log/slog"

	"github.com/justindeelux/gotham/agent/docker"
	"github.com/justindeelux/gotham/agent/files"
	"github.com/justindeelux/gotham/agent/proxy"
	"github.com/justindeelux/gotham/agent/terminal"
	"github.com/justindeelux/gotham/agent/tools"
	"github.com/justindeelux/gotham/agent/updates"
)

// Type aliases keep the root package a usable composition surface for the
// entrypoint, integration tests and external callers while the implementations
// live in their functional subpackages.
type (
	// DockerClient is the node's Docker Engine client.
	DockerClient = docker.DockerClient
	// DockerClientOption tunes a DockerClient.
	DockerClientOption = docker.DockerClientOption
	// DockerServerOption tunes the DockerService implementation.
	DockerServerOption = docker.DockerServerOption
	// LogMessage is one decoded Docker log chunk.
	LogMessage = docker.LogMessage
	// ExecOptions configures a container exec session.
	ExecOptions = docker.ExecOptions
	// ExecSession is a running container exec.
	ExecSession = docker.ExecSession
	// BuildServer is the BuildService implementation.
	BuildServer = docker.BuildServer
	// BuildServerOption tunes a BuildServer.
	BuildServerOption = docker.BuildServerOption
	// ComposeServerConfig wires a ComposeServer.
	ComposeServerConfig = docker.ComposeServerConfig
	// ComposeServer is the ComposeService implementation.
	ComposeServer = docker.ComposeServer

	// ProxyServerConfig wires a ProxyServer.
	ProxyServerConfig = proxy.Config
	// ProxyServer is the ProxyService implementation.
	ProxyServer = proxy.Server

	// FileServer is the confined FileService implementation.
	FileServer = files.FileServer

	// TerminalConfig wires a TerminalServer.
	TerminalConfig = terminal.TerminalConfig
	// TerminalRoots resolves a host terminal's initial directory.
	TerminalRoots = terminal.TerminalRoots
	// ContainerExec is the container exec surface the terminal service uses.
	ContainerExec = terminal.ContainerExec
	// TerminalServer is the TerminalService implementation.
	TerminalServer = terminal.TerminalServer

	// ToolServer is the ToolService implementation.
	ToolServer = tools.ToolServer
	// ToolServerOption tunes a ToolServer.
	ToolServerOption = tools.ToolServerOption
	// ToolUseTracker records which host tools are busy.
	ToolUseTracker = tools.ToolUseTracker
)

// ErrInvalidPortMapping and the other Docker errors are re-exported for
// callers that map them onto control-plane responses.
var (
	ErrInvalidPortMapping        = docker.ErrInvalidPortMapping
	ErrDockerNotFound            = docker.ErrDockerNotFound
	ErrDockerUnavailable         = docker.ErrDockerUnavailable
	ErrDockerImageNotFound       = docker.ErrDockerImageNotFound
	ErrDockerContainerNotRunning = docker.ErrDockerContainerNotRunning
	ErrDockerNoShell             = docker.ErrDockerNoShell
	ErrInvalidVolumeBind         = docker.ErrInvalidVolumeBind
)

// NewDockerClient returns a Docker Engine client for target.
func NewDockerClient(target string, options ...DockerClientOption) (*DockerClient, error) {
	return docker.NewDockerClient(target, options...)
}

// WithRegistryStateDir sets the directory registry credentials are stored in.
func WithRegistryStateDir(dir string) DockerClientOption {
	return docker.WithRegistryStateDir(dir)
}

// NewDockerServer returns the DockerService implementation.
func NewDockerServer(client docker.Client, log *slog.Logger, options ...DockerServerOption) *docker.DockerServer {
	return docker.NewDockerServer(client, log, options...)
}

// WithManagedVolumeRoot confines application binds to <root>/<app id>.
func WithManagedVolumeRoot(root string) DockerServerOption {
	return docker.WithManagedVolumeRoot(root)
}

// WithProxyVolumeRoot overrides the directory a proxy container may mount.
func WithProxyVolumeRoot(root string) DockerServerOption {
	return docker.WithProxyVolumeRoot(root)
}

// NewBuildServer returns the BuildService implementation.
func NewBuildServer(client docker.BuildClient, log *slog.Logger, options ...BuildServerOption) *docker.BuildServer {
	return docker.NewBuildServer(client, log, options...)
}

// WithToolUseTracker makes a build refuse while a toolchain is being removed.
func WithToolUseTracker(tracker *ToolUseTracker) BuildServerOption {
	return docker.WithToolUseTracker(tracker)
}

// NewComposeServer returns the ComposeService implementation.
func NewComposeServer(cfg ComposeServerConfig) *docker.ComposeServer {
	return docker.NewComposeServer(cfg)
}

// NewProxyServer returns the ProxyService implementation.
func NewProxyServer(cfg ProxyServerConfig) *proxy.Server {
	return proxy.New(cfg)
}

// NewFileServer validates and opens the configured file roots.
func NewFileServer(cfg Config, log *slog.Logger) (*files.FileServer, error) {
	return files.NewFileServer(files.Config{
		FileFullAccess:           cfg.FileFullAccess,
		ManagedVolumeRoot:        cfg.ManagedVolumeRoot,
		ComposeRoot:              cfg.ComposeRoot,
		FileRoots:                cfg.FileRoots,
		CertDir:                  cfg.CertDir,
		KeyFile:                  cfg.KeyFile,
		CA:                       cfg.CA,
		BinaryPath:               cfg.BinaryPath,
		AgentStateRoot:           defaultAgentStateRoot,
		UpdateScript:             cfg.UpdateScript,
		UpdateStatusPath:         cfg.UpdateStatusPath,
		UpdatePendingPath:        cfg.UpdatePendingPath,
		UpdateLockPath:           cfg.UpdateLockPath,
		UpdateRetryPath:          cfg.UpdateRetryPath,
		UpdateBackoffPath:        cfg.UpdateBackoffPath,
		FileMaxUpload:            cfg.FileMaxUpload,
		FileMaxTransfers:         cfg.FileMaxTransfers,
		FileArchiveMaxBytes:      cfg.FileArchiveMaxBytes,
		FileArchiveMaxEntries:    cfg.FileArchiveMaxEntries,
		FileArchiveMaxInputBytes: cfg.FileArchiveMaxInputBytes,
	}, log)
}

// NewTerminalServer returns the TerminalService implementation.
func NewTerminalServer(cfg TerminalConfig) *terminal.TerminalServer {
	return terminal.NewTerminalServer(cfg)
}

// NewToolServer returns the ToolService implementation.
func NewToolServer(options ...ToolServerOption) *tools.ToolServer {
	return tools.NewToolServer(options...)
}

// NewToolUseTracker returns an empty tool-use tracker.
func NewToolUseTracker() *tools.ToolUseTracker {
	return tools.NewToolUseTracker()
}

// WithToolUse wires the tracker so an uninstall refuses while a tool is busy.
func WithToolUse(tracker *ToolUseTracker) ToolServerOption {
	return tools.WithToolUse(tracker)
}

// ResetUpdateState clears the pending marker, status and failed-update backoff.
func ResetUpdateState(cfg Config) error {
	return updates.ResetState(updates.Config{
		PendingPath: cfg.UpdatePendingPath,
		StatusPath:  cfg.UpdateStatusPath,
		RetryPath:   cfg.UpdateRetryPath,
	})
}
