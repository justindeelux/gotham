package templates

// Render values for templates whose required fields the generic generated
// defaults cannot satisfy. The confinement test renders a sample map on its
// own, so every such field is listed here.
func init() {
	builtinSamples["buzz"] = map[string]any{
		"postgres_password": "sample-secret",
		"redis_password":    "sample-secret",
		"minio_password":    "sample-secret",
	}
	builtinSamples["bulwark"] = map[string]any{
		"jmap_server_url": "https://jmap.example.test",
	}
	builtinSamples["open-archiver"] = map[string]any{
		"db_password":      "sample-secret",
		"meili_master_key": "sample-secret",
		"valkey_password":  "sample-secret",
	}
	builtinSamples["plunk"] = map[string]any{
		"dashboard_domain":      "app.example.test",
		"api_domain":            "api.example.test",
		"db_password":           "sample-secret",
		"ses_access_key_id":     "AKIAIOSFODNN7EXAMPLE",
		"ses_secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	builtinSamples["usesend"] = map[string]any{
		"domain":         "app.example.test",
		"db_password":    "sample-secret",
		"github_id":      "test-client-id",
		"github_secret":  "sample-secret",
		"aws_access_key": "AKIAIOSFODNN7EXAMPLE",
		"aws_secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	builtinSamples["openwa"] = map[string]any{
		"db_password": "sample-secret",
	}
	builtinSamples["chatwoot"] = map[string]any{
		"domain":         "app.example.test",
		"db_password":    "sample-secret",
		"redis_password": "sample-secret",
	}
}
