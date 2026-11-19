package templates

// Render values the generated defaults cannot satisfy for this batch: URLs
// and an email address. TestBuiltinTemplatesPassServiceConfinement renders with
// the sample alone, so every required field is listed here.
func init() {
	builtinSamples["bluesky-pds"] = map[string]any{
		"domain":         "pds.example.test",
		"admin_email":    "admin@example.test",
		"admin_password": "admin-secret",
		"jwt_secret":     "jwt-secret",
	}
	builtinSamples["hoppscotch"] = map[string]any{
		"domain":      "hoppscotch.example.test",
		"db_password": "db-secret",
	}
	builtinSamples["onedev"] = map[string]any{
		"domain":         "onedev.example.test",
		"admin_email":    "admin@example.test",
		"admin_password": "admin-secret",
		"db_password":    "db-secret",
	}
	builtinSamples["zero"] = map[string]any{
		"query_url":      "https://api.example.test/query",
		"mutate_url":     "https://api.example.test/mutate",
		"db_password":    "db-secret",
		"admin_password": "admin-secret",
	}
}
