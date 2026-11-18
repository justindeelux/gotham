package templates

import "strings"

// Templates with a secret that must match a format the generated sample value
// cannot satisfy. Entries are complete value sets: builtinSamples is rendered
// as-is by the confinement test.
func init() {
	laravelKey := "base64:" + strings.Repeat("A", 43) + "="
	secret := "sample-secret"

	builtinSamples["bookstack"] = map[string]any{
		"domain":           "app.example.test",
		"app_key":          laravelKey,
		"db_name":          "bookstack",
		"db_user":          "bookstack",
		"db_password":      secret,
		"db_root_password": secret,
	}
	builtinSamples["firefly"] = map[string]any{
		"domain":           "app.example.test",
		"app_key":          laravelKey,
		"cron_token":       strings.Repeat("ab", 16),
		"db_name":          "firefly",
		"db_user":          "firefly",
		"db_password":      secret,
		"db_root_password": secret,
	}
	builtinSamples["paymenter"] = map[string]any{
		"domain":           "app.example.test",
		"app_key":          laravelKey,
		"db_name":          "paymenter",
		"db_user":          "paymenter",
		"db_password":      secret,
		"db_root_password": secret,
		"redis_password":   secret,
	}
	builtinSamples["sure"] = map[string]any{
		"domain":          "app.example.test",
		"secret_key_base": strings.Repeat("ab", 64),
		"db_name":         "sure",
		"db_user":         "sure",
		"db_password":     secret,
	}
	builtinSamples["sparkyfitness"] = map[string]any{
		"domain":             "app.example.test",
		"db_name":            "sparkyfitness",
		"db_user":            "sparky",
		"db_password":        secret,
		"app_db_user":        "sparkyapp",
		"app_db_password":    secret,
		"api_encryption_key": strings.Repeat("ab", 32),
		"auth_secret":        strings.Repeat("a", 48),
		"admin_email":        "admin@example.test",
	}
	builtinSamples["portabase"] = map[string]any{
		"domain":         "app.example.test",
		"project_secret": strings.Repeat("ab", 32),
		"db_name":        "portabase",
		"db_user":        "portabase",
		"db_password":    secret,
	}
}
