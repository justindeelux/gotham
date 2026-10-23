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

type (
	DockerClient        = docker.DockerClient
	DockerClientOption  = docker.DockerClientOption
	DockerServerOption  = docker.DockerServerOption
	LogMessage          = docker.LogMessage
	ExecOptions         = docker.ExecOptions
	ExecSession         = docker.ExecSession
	BuildServer         = docker.BuildServer
	BuildServerOption   = docker.BuildServerOption
	ComposeServerConfig = docker.ComposeServerConfig
	ComposeServer       = docker.ComposeServer

	ProxyServerConfig = proxy.Config
	ProxyServer       = proxy.Server

	FileServer = files.FileServer

	TerminalConfig = terminal.TerminalConfig
	TerminalRoots  = terminal.TerminalRoots
	ContainerExec  = terminal.ContainerExec
	TerminalServer = terminal.TerminalServer

	ToolServer       = tools.ToolServer
	ToolServerOption = tools.ToolServerOption
	ToolUseTracker   = tools.ToolUseTracker
)

var (
	ErrInvalidPortMapping        = docker.ErrInvalidPortMapping
	ErrDockerNotFound            = docker.ErrDockerNotFound
	ErrDockerUnavailable         = docker.ErrDockerUnavailable
	ErrDockerImageNotFound       = docker.ErrDockerImageNotFound
	ErrDockerContainerNotRunning = docker.ErrDockerContainerNotRunning
	ErrDockerNoShell             = docker.ErrDockerNoShell
	ErrInvalidVolumeBind         = docker.ErrInvalidVolumeBind
)

func NewDockerClient(target string, options ...DockerClientOption) (*DockerClient, error) {
	return docker.NewDockerClient(target, options...)
}

func WithRegistryStateDir(dir string) DockerClientOption {
	return docker.WithRegistryStateDir(dir)
}

func WithLogger(log *slog.Logger) DockerClientOption {
	return docker.WithLogger(log)
}

func NewDockerServer(client docker.Client, log *slog.Logger, options ...DockerServerOption) *docker.DockerServer {
	return docker.NewDockerServer(client, log, options...)
}

func WithManagedVolumeRoot(root string) DockerServerOption {
	return docker.WithManagedVolumeRoot(root)
}

func WithProxyVolumeRoot(root string) DockerServerOption {
	return docker.WithProxyVolumeRoot(root)
}

func NewBuildServer(client docker.BuildClient, log *slog.Logger, options ...BuildServerOption) *docker.BuildServer {
	return docker.NewBuildServer(client, log, options...)
}

func WithToolUseTracker(tracker *ToolUseTracker) BuildServerOption {
	return docker.WithToolUseTracker(tracker)
}

func NewComposeServer(cfg ComposeServerConfig) *docker.ComposeServer {
	return docker.NewComposeServer(cfg)
}

func NewProxyServer(cfg ProxyServerConfig) *proxy.Server {
	return proxy.New(cfg)
}

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

func NewTerminalServer(cfg TerminalConfig) *terminal.TerminalServer {
	return terminal.NewTerminalServer(cfg)
}

func NewToolServer(options ...ToolServerOption) *tools.ToolServer {
	return tools.NewToolServer(options...)
}

func NewToolUseTracker() *tools.ToolUseTracker {
	return tools.NewToolUseTracker()
}

func WithToolUse(tracker *ToolUseTracker) ToolServerOption {
	return tools.WithToolUse(tracker)
}

func ResetUpdateState(cfg Config) error {
	return updates.ResetState(updates.Config{
		PendingPath: cfg.UpdatePendingPath,
		StatusPath:  cfg.UpdateStatusPath,
		RetryPath:   cfg.UpdateRetryPath,
	})
}
