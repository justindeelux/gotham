package databases

import "time"

type KeyDBEngine struct{}

func NewKeyDBEngine() *KeyDBEngine { return &KeyDBEngine{} }

var (
	_ DatabaseEngine = (*KeyDBEngine)(nil)
	_ CommandSpec    = (*KeyDBEngine)(nil)
)

func (e *KeyDBEngine) Image(version string) string {
	return engineImage("eqalpha/keydb", defaultKeyDBVersion, version)
}

func (e *KeyDBEngine) EnvSpec(c Credentials) []string {
	return []string{"REDIS_PASSWORD=" + c.Password}
}

func (e *KeyDBEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 6379, Protocol: "tcp"}
}

func (e *KeyDBEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/data"}
}

func (e *KeyDBEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe:   ProbeHealth,
		Command: []string{"keydb-cli", "--pass", placeholderPassword, "ping"},
		Timeout: 30 * time.Second,
	}
}

func (e *KeyDBEngine) Command(_ Credentials) []string {
	return []string{
		"sh", "-c",
		`exec keydb-server --appendonly yes --requirepass "$REDIS_PASSWORD"`,
	}
}
