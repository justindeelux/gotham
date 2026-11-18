package templates

// Secrets whose format the application enforces cannot use a generated value,
// so the render tests supply values that pass each field's pattern.
func init() {
	const (
		hs256Secret    = "0123456789abcdef0123456789abcdef"
		phoenixSecret  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef=="
		sequinVaultKey = "2Sig69bIpuSm2kv0VQfDekET2qy8qUZGI8v3/h3ASiY="
		appSecret      = "sample-secret"
	)
	builtinSamples["checkmate"] = map[string]any{"jwt_secret": hs256Secret}
	builtinSamples["postiz"] = map[string]any{
		"db_password":    appSecret,
		"redis_password": appSecret,
		"jwt_secret":     hs256Secret,
	}
	builtinSamples["umami"] = map[string]any{
		"db_password": appSecret,
		"app_secret":  hs256Secret,
	}
	builtinSamples["sessy"] = map[string]any{
		"auth_password": appSecret,
		"secret_key":    hs256Secret,
	}
	builtinSamples["sequin"] = map[string]any{
		"db_password": appSecret,
		"secret_key":  phoenixSecret,
		"vault_key":   sequinVaultKey,
	}
	builtinSamples["openpanel"] = map[string]any{
		"domain":         "analytics.example.test",
		"api_domain":     "api.analytics.example.test",
		"db_password":    appSecret,
		"redis_password": appSecret,
		"cookie_secret":  appSecret,
	}
}
