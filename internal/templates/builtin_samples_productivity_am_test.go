package templates

// Alexandrie declares three public routes, and the generated value is the same
// for every text field, so the render test needs three distinct hosts.
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
}
