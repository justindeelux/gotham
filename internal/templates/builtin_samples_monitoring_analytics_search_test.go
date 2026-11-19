package templates

// Render values for templates whose required fields the generic generated
// defaults cannot satisfy.
func init() {
	const appSecret = "sample-secret"
	builtinSamples["postiz"] = map[string]any{
		"db_password":    appSecret,
		"redis_password": appSecret,
	}
	builtinSamples["umami"] = map[string]any{
		"db_password": appSecret,
	}
	builtinSamples["sessy"] = map[string]any{
		"auth_password": appSecret,
	}
	builtinSamples["sequin"] = map[string]any{
		"db_password": appSecret,
	}
	builtinSamples["openpanel"] = map[string]any{
		"domain":         "analytics.example.test",
		"api_domain":     "api.analytics.example.test",
		"db_password":    appSecret,
		"redis_password": appSecret,
		"cookie_secret":  appSecret,
	}
}
