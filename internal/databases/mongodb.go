package databases

import "time"

type MongoDBEngine struct{}

func NewMongoDBEngine() *MongoDBEngine { return &MongoDBEngine{} }

var _ DatabaseEngine = (*MongoDBEngine)(nil)

func (e *MongoDBEngine) Image(version string) string {
	return engineImage("mongo", defaultMongoDBVersion, version)
}

func (e *MongoDBEngine) EnvSpec(c Credentials) []string {
	return []string{
		"MONGO_INITDB_ROOT_USERNAME=" + c.Username,
		"MONGO_INITDB_ROOT_PASSWORD=" + c.Password,
		"MONGO_INITDB_DATABASE=" + c.Database,
	}
}

func (e *MongoDBEngine) PortSpec() PortSpec {
	return PortSpec{Internal: 27017, Protocol: "tcp"}
}

func (e *MongoDBEngine) VolumeSpec() VolumeSpec {
	return VolumeSpec{MountPath: "/data/db"}
}

func (e *MongoDBEngine) Healthcheck() Healthcheck {
	return Healthcheck{
		Probe: ProbeHealth,
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
