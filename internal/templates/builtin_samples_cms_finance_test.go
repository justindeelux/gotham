package templates

// Templates whose remaining required fields the generic generated defaults
// cannot satisfy. Entries are complete value sets: builtinSamples is rendered
// as-is by the confinement test.
func init() {
	secret := "sample-secret"

	builtinSamples["bookstack"] = map[string]any{
		"domain":           "app.example.test",
		"db_name":          "bookstack",
		"db_user":          "bookstack",
		"db_password":      secret,
		"db_root_password": secret,
	}
	builtinSamples["firefly"] = map[string]any{
		"domain":           "app.example.test",
		"db_name":          "firefly",
		"db_user":          "firefly",
		"db_password":      secret,
		"db_root_password": secret,
	}
	builtinSamples["paymenter"] = map[string]any{
		"domain":           "app.example.test",
		"db_name":          "paymenter",
		"db_user":          "paymenter",
		"db_password":      secret,
		"db_root_password": secret,
		"redis_password":   secret,
	}
	builtinSamples["sure"] = map[string]any{
		"domain":      "app.example.test",
		"db_name":     "sure",
		"db_user":     "sure",
		"db_password": secret,
	}
	builtinSamples["sparkyfitness"] = map[string]any{
		"domain":          "app.example.test",
		"db_name":         "sparkyfitness",
		"db_user":         "sparky",
		"db_password":     secret,
		"app_db_user":     "sparkyapp",
		"app_db_password": secret,
		"admin_email":     "admin@example.test",
	}
	builtinSamples["portabase"] = map[string]any{
		"domain":      "app.example.test",
		"db_name":     "portabase",
		"db_user":     "portabase",
		"db_password": secret,
	}
}
