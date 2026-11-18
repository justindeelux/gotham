package templates

// Render values the generated defaults cannot satisfy for this batch:
// base64 and hex secrets, URLs and an email address. TestBuiltinTemplatesPassServiceConfinement
// renders with the sample alone, so every required field is listed here.
func init() {
	const base64Key = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	builtinSamples["bluesky-pds"] = map[string]any{
		"domain":         "pds.example.test",
		"admin_email":    "admin@example.test",
		"admin_password": "admin-secret",
		"jwt_secret":     "jwt-secret",
		"rotation_key":   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	builtinSamples["hoppscotch"] = map[string]any{
		"domain":      "hoppscotch.example.test",
		"data_key":    base64Key,
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
