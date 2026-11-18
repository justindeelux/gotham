package templates

// Sample values for the productivity templates (A-M) whose secrets have a
// required format the generic generator cannot produce. Alexandrie also
// declares three public routes, and the generated value is the same for every
// text field, so it needs three distinct hosts.
func init() {
	const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const hex32 = "0123456789abcdef0123456789abcdef"
	const base64Key = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

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
		"app_secret":  hex64,
	}
	builtinSamples["documenso"] = map[string]any{
		"db_password":              "db-secret",
		"auth_secret":              hex64,
		"encryption_key":           hex64,
		"secondary_encryption_key": hex64,
		"signing_passphrase":       "signing-secret",
	}
	builtinSamples["fizzy"] = map[string]any{
		"secret_key_base": hex64 + hex64,
		"master_key":      hex32,
	}
	builtinSamples["getoutline"] = map[string]any{
		"db_password":    "db-secret",
		"redis_password": "redis-secret",
		"secret_key":     hex64,
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
		"session_secret": hex64,
		"form_secret":    hex64,
		"keydb_password": "keydb-secret",
	}
	builtinSamples["hi-events"] = map[string]any{
		"db_password":    "db-secret",
		"redis_password": "redis-secret",
		"app_key":        base64Key,
		"jwt_secret":     hex64,
	}
	builtinSamples["leantime"] = map[string]any{
		"db_password":      "db-secret",
		"db_root_password": "root-secret",
		"session_password": hex64,
	}
}
