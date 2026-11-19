package templates

func init() {
	builtinSamples["cap"] = map[string]any{
		"db_password":          "cap-db-secret",
		"db_root_password":     "cap-root-secret",
		"aws_access_key":       "cap-s3-access",
		"aws_secret_key":       "cap-s3-secret",
		"aws_bucket":           "cap-recordings",
		"aws_endpoint":         "https://s3.example.test",
		"s3_internal_endpoint": "https://s3.example.test",
		"s3_public_endpoint":   "https://media.example.test",
	}
	builtinSamples["castopod"] = map[string]any{
		"db_password":      "castopod-db-secret",
		"db_root_password": "castopod-root-secret",
		"redis_password":   "castopod-redis-secret",
	}
	builtinSamples["zipline"] = map[string]any{
		"db_password": "zipline-db-secret",
	}
	builtinSamples["ente-photos"] = map[string]any{
		"photos_domain": "photos.example.test",
		"api_domain":    "api.photos.example.test",
		"albums_domain": "albums.example.test",
		"db_user":       "pguser",
		"db_password":   "ente-db-secret",
		"s3_access_key": "ente-s3-access",
		"s3_secret_key": "ente-s3-secret",
		"s3_endpoint":   "https://s3.example.test",
		"s3_region":     "us-east-1",
		"s3_bucket":     "ente-photos",
	}
}
