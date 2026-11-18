package templates

func init() {
	builtinSamples["cap"] = map[string]any{
		"db_password":             "cap-db-secret",
		"db_root_password":        "cap-root-secret",
		"database_encryption_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"nextauth_secret":         "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
		"aws_access_key":          "cap-s3-access",
		"aws_secret_key":          "cap-s3-secret",
		"aws_bucket":              "cap-recordings",
		"aws_endpoint":            "https://s3.example.test",
		"s3_internal_endpoint":    "https://s3.example.test",
		"s3_public_endpoint":      "https://media.example.test",
	}
	builtinSamples["castopod"] = map[string]any{
		"db_password":      "castopod-db-secret",
		"db_root_password": "castopod-root-secret",
		"redis_password":   "castopod-redis-secret",
		"analytics_salt":   "castopodanalyticsalt0123456789abcdefghijklmnopqrstuvwxyzABCDEFGH",
	}
	builtinSamples["zipline"] = map[string]any{
		"core_secret": "0123456789abcdef0123456789abcdef",
		"db_password": "zipline-db-secret",
	}
	builtinSamples["ente-photos"] = map[string]any{
		"photos_domain":  "photos.example.test",
		"api_domain":     "api.photos.example.test",
		"albums_domain":  "albums.example.test",
		"db_user":        "pguser",
		"db_password":    "ente-db-secret",
		"encryption_key": "Z290aGFtLWVudGUtZW5jcnlwdGlvbi1rZXktMzJieXQ=",
		"hash_key":       "Z290aGFtLWVudGUtaGFzaC1rZXktMzItYnl0ZXMtb2s=",
		"jwt_secret":     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"s3_access_key":  "ente-s3-access",
		"s3_secret_key":  "ente-s3-secret",
		"s3_endpoint":    "https://s3.example.test",
		"s3_region":      "us-east-1",
		"s3_bucket":      "ente-photos",
	}
}
