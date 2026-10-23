package agent

import (
	"context"
	"io"
	"log/slog"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeDockerClient struct {
	containers     []*agentv1.ContainerInfo
	listAll        bool
	listLabels     map[string]string
	version        string
	err            error
	logs           []byte
	logStreamErr   error
	started        []string
	stopped        []string
	restarted      []string
	removed        []string
	removedVolumes []string
	createdID      string
}

func (f *fakeDockerClient) Version(context.Context) (string, error) {
	return f.version, f.err
}

func (f *fakeDockerClient) ListContainers(_ context.Context, all bool, labels map[string]string, resolveDevices bool) ([]*agentv1.ContainerInfo, error) {
	f.listAll = all
	f.listLabels = labels
	if f.err != nil {
		return nil, f.err
	}
	return f.containers, nil
}

func (f *fakeDockerClient) Start(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, id)
	return nil
}

func (f *fakeDockerClient) Stop(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeDockerClient) Restart(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.restarted = append(f.restarted, id)
	return nil
}

func (f *fakeDockerClient) Remove(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeDockerClient) RemoveVolume(_ context.Context, name string) error {
	if f.err != nil {
		return f.err
	}
	f.removedVolumes = append(f.removedVolumes, name)
	return nil
}

func (f *fakeDockerClient) PullImage(context.Context, string, string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "sha256:testdigest", nil
}

func (f *fakeDockerClient) CreateContainer(context.Context, *agentv1.CreateContainerRequest) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.createdID != "" {
		return f.createdID, nil
	}
	return "created", nil
}

func (f *fakeDockerClient) RunImage(_ context.Context, _ *agentv1.CreateContainerRequest) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	id := f.createdID
	if id == "" {
		id = "created"
	}
	f.started = append(f.started, id)
	return id, nil
}

func (f *fakeDockerClient) Logs(context.Context, string, bool, int64) (<-chan LogMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	chunks := make(chan LogMessage, 2)
	if len(f.logs) > 0 {
		chunks <- LogMessage{Data: f.logs}
	}
	if f.logStreamErr != nil {
		chunks <- LogMessage{Err: f.logStreamErr}
	}
	close(chunks)
	return chunks, nil
}
