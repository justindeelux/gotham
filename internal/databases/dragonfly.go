package databases

import "time"

type DragonflyEngine struct{}

func NewDragonflyEngine() *DragonflyEngine { return &DragonflyEngine{} }

var (
	_ DatabaseEngine = (*DragonflyEngine)(nil)
	_ CommandSpec    = (*DragonflyEngine)(nil)
)

func (e *DragonflyEngine) Image(version string) string {
	return engineImage("docker.dragonflydb.io/dragonflydb/dragonfly", defaultDragonflyVersion, version)
}

func (e *DragonflyEngine) EnvSpec(c Credentials) []string {
	return []string{"REDIS_PASSWORD=" + c.Password}
}

func (e *DragonflyEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 6379, Protocol: "tcp"}
}

func (e *DragonflyEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/data"}
}

func (e *DragonflyEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"redis-cli", "-a", placeholderPassword, "ping"},
		Timeout: 30 * time.Second,
	}
}

func (e *DragonflyEngine) Command(_ Credentials) []string {
	return []string{
		"sh", "-c",
		`exec dragonfly --dir /data --requirepass "$REDIS_PASSWORD"`,
	}
}
