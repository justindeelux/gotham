package templates

// Sample values for the productivity templates (A-M): Alexandrie declares three
// public routes, and the generated value is the same for every text field, so
// it needs three distinct hosts.
func init() {
	builtinSamples["alexandrie"] = map[string]any{
		"domain":           "markdown.example.test",
		"api_domain":       "api.example.test",
		"cdn_domain":       "files.example.test",
		"db_password":      "db-secret",
		"db_root_password": "root-secret",
		"jwt_secret":       "jwt-secret",
		"storage_password": "storage-secret",
	}
	builtinSamples["docmost"] = map[string]any{
		"db_password": "db-secret",
	}
	builtinSamples["documenso"] = map[string]any{
		"db_password":        "db-secret",
		"signing_passphrase": "signing-secret",
	}
	builtinSamples["getoutline"] = map[string]any{
		"db_password":    "db-secret",
		"redis_password": "redis-secret",
		"utils_secret":   "utils-secret",
	}
	builtinSamples["grist"] = map[string]any{
		"db_password":        "db-secret",
		"session_secret":     "session-secret",
		"oidc_issuer":        "https://auth.example.test/application/o/grist/",
		"oidc_client_id":     "grist",
		"oidc_client_secret": "client-secret",
	}
	builtinSamples["heyform"] = map[string]any{
		"keydb_password": "keydb-secret",
	}
	builtinSamples["hi-events"] = map[string]any{
		"db_password":    "db-secret",
		"redis_password": "redis-secret",
	}
	builtinSamples["leantime"] = map[string]any{
		"db_password":      "db-secret",
		"db_root_password": "root-secret",
	}
}
