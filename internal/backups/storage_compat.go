package backups

import "github.com/justindeelux/gotham/internal/storages"

// Storage backends moved to internal/storages. These aliases keep the backup
// core (and its tests) on the same types while the implementation lives with
// the storage domain.
type ObjectStore = storages.ObjectStore

const (
	locationFilePrefix = storages.LocationFilePrefix
	locationS3Prefix   = storages.LocationS3Prefix
)

// s3Config is the local spelling used by the backup core; it maps onto the
// storage-owned config.
type s3Config struct {
	endpoint  string
	region    string
	bucket    string
	prefix    string
	accessKey string
	secretKey string
}

func newLocalStore(dir string) (*storages.LocalStore, error) {
	return storages.NewLocalStore(dir)
}

func newS3Store(cfg s3Config) (*storages.S3Store, error) {
	return storages.NewS3Store(storages.S3Config{
		Endpoint:  cfg.endpoint,
		Region:    cfg.region,
		Bucket:    cfg.bucket,
		Prefix:    cfg.prefix,
		AccessKey: cfg.accessKey,
		SecretKey: cfg.secretKey,
	})
}

func splitEndpoint(endpoint string) (string, bool, error) {
	return storages.SplitEndpoint(endpoint)
}
