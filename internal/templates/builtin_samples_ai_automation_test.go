package templates

// init registers render values for templates whose required fields the generic
// generated defaults cannot satisfy.
func init() {
	builtinSamples["langfuse"] = map[string]any{
		"admin_password":      "admin-secret",
		"db_password":         "db-secret",
		"redis_password":      "redis-secret",
		"clickhouse_password": "clickhouse-secret",
		"salt":                "salt-secret",
		"nextauth_secret":     "nextauth-secret",
	}
	builtinSamples["engram"] = map[string]any{
		"db_password":    "db-secret",
		"cloud_token":    "cloud-secret",
		"admin_password": "admin-secret",
		"token_pepper":   "pepper-secret",
	}
	builtinSamples["activepieces"] = map[string]any{
		"api_key":     "api-secret",
		"jwt_secret":  "jwt-secret",
		"db_password": "db-secret",
	}
	builtinSamples["librechat"] = map[string]any{
		"mongo_password":     "mongo-secret",
		"meili_key":          "meili-secret",
		"jwt_secret":         "jwt-secret",
		"jwt_refresh_secret": "jwt-refresh-secret",
		"db_password":        "db-secret",
	}
	builtinSamples["lobe-chat"] = map[string]any{
		"storage_domain":  "files.example.test",
		"db_password":     "db-secret",
		"rustfs_password": "rustfs-secret",
	}
	builtinSamples["inngest"] = map[string]any{
		"db_password": "db-secret",
	}
}
