package templates

import "strings"

// init registers render values for templates whose secrets need a fixed
// format, which the generic "sample-secret" value does not satisfy.
func init() {
	builtinSamples["langfuse"] = map[string]any{
		"admin_password":      "admin-secret",
		"db_password":         "db-secret",
		"redis_password":      "redis-secret",
		"clickhouse_password": "clickhouse-secret",
		"salt":                "salt-secret",
		"encryption_key":      strings.Repeat("a", 64),
		"nextauth_secret":     "nextauth-secret",
	}
	builtinSamples["engram"] = map[string]any{
		"db_password":    "db-secret",
		"jwt_secret":     strings.Repeat("1", 64),
		"cloud_token":    "cloud-secret",
		"admin_password": "admin-secret",
		"token_pepper":   "pepper-secret",
	}
	builtinSamples["activepieces"] = map[string]any{
		"api_key":        "api-secret",
		"encryption_key": strings.Repeat("2", 32),
		"jwt_secret":     "jwt-secret",
		"db_password":    "db-secret",
	}
	builtinSamples["librechat"] = map[string]any{
		"mongo_password":     "mongo-secret",
		"meili_key":          "meili-secret",
		"jwt_secret":         "jwt-secret",
		"jwt_refresh_secret": "jwt-refresh-secret",
		"creds_key":          strings.Repeat("b", 64),
		"creds_iv":           strings.Repeat("c", 32),
		"db_password":        "db-secret",
	}
	builtinSamples["lobe-chat"] = map[string]any{
		"storage_domain":    "files.example.test",
		"db_password":       "db-secret",
		"rustfs_password":   "rustfs-secret",
		"key_vaults_secret": strings.Repeat("d", 43) + "=",
		"auth_secret":       strings.Repeat("e", 43) + "=",
	}
	builtinSamples["inngest"] = map[string]any{
		"event_key":   strings.Repeat("f", 32),
		"signing_key": strings.Repeat("0", 32),
		"db_password": "db-secret",
	}
}
