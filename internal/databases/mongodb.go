package databases

import "time"

// MongoDBEngine maps MongoDB onto its container shape: one root account plus
// the database the shell opens by default, both created by the entrypoint on
// first boot.
type MongoDBEngine struct{}

// NewMongoDBEngine returns the MongoDB engine.
func NewMongoDBEngine() *MongoDBEngine { return &MongoDBEngine{} }

// Compile-time guarantees.
var _ DatabaseEngine = (*MongoDBEngine)(nil)

// Image implements DatabaseEngine.
func (e *MongoDBEngine) Image(version string) string {
	return engineImage("mongo", defaultMongoDBVersion, version)
}

// EnvSpec implements DatabaseEngine.
func (e *MongoDBEngine) EnvSpec(c Credentials) []string {
	return []string{
		"MONGO_INITDB_ROOT_USERNAME=" + c.Username,
		"MONGO_INITDB_ROOT_PASSWORD=" + c.Password,
		"MONGO_INITDB_DATABASE=" + c.Database,
	}
}

// PortSpec implements DatabaseEngine.
func (e *MongoDBEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 27017, Protocol: "tcp"}
}

// VolumeSpec implements DatabaseEngine.
func (e *MongoDBEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/data/db"}
}

// Healthcheck implements DatabaseEngine: an authenticated ping over mongosh
// is the canonical probe for this engine. The control plane still evaluates it
// as a state wait (see ProbeState) because the agent contract has no Exec RPC
// yet — the command is what an Exec-capable agent will run.
func (e *MongoDBEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe: ProbeState,
		Command: []string{
			"mongosh", "--quiet",
			"--username", placeholderUser,
			"--password", placeholderPassword,
			"--authenticationDatabase", "admin",
			"--eval", "db.adminCommand('ping')",
		},
		Timeout: 60 * time.Second,
	}
}
