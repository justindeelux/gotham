package templates

// Render values for templates whose secrets have a fixed format: the generic
// generated value is too short or not hex. The confinement test renders a
// sample map on its own, so every required field is listed here too.
func init() {
	builtinSamples["buzz"] = map[string]any{
		"postgres_password": "sample-secret",
		"redis_password":    "sample-secret",
		"minio_password":    "sample-secret",
		"relay_private_key": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"git_hmac_secret":   "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	builtinSamples["once-campfire"] = map[string]any{
		"secret_key_base": "0123456789abcdef0123456789abcdef",
	}
	builtinSamples["bulwark"] = map[string]any{
		"jmap_server_url": "https://jmap.example.test",
		"session_secret":  "0123456789abcdef0123456789abcdef",
	}
	builtinSamples["open-archiver"] = map[string]any{
		"db_password":            "sample-secret",
		"meili_master_key":       "sample-secret",
		"valkey_password":        "sample-secret",
		"encryption_key":         "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"storage_encryption_key": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		"jwt_secret":             "0123456789abcdef0123456789abcdef",
	}
	builtinSamples["plunk"] = map[string]any{
		"dashboard_domain":      "app.example.test",
		"api_domain":            "api.example.test",
		"db_password":           "sample-secret",
		"jwt_secret":            "0123456789abcdef0123456789abcdef",
		"ses_access_key_id":     "AKIAIOSFODNN7EXAMPLE",
		"ses_secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	builtinSamples["usesend"] = map[string]any{
		"domain":          "app.example.test",
		"db_password":     "sample-secret",
		"github_id":       "test-client-id",
		"github_secret":   "sample-secret",
		"nextauth_secret": "0123456789abcdef0123456789abcdef",
		"aws_access_key":  "AKIAIOSFODNN7EXAMPLE",
		"aws_secret_key":  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	builtinSamples["chatwoot"] = map[string]any{
		"domain":          "app.example.test",
		"db_password":     "sample-secret",
		"redis_password":  "sample-secret",
		"secret_key_base": "0123456789abcdef0123456789abcdef",
	}
}
