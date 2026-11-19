package templates

func init() {
	builtinSamples["newt-pangolin"] = map[string]any{
		"endpoint":    "https://pangolin.example.test",
		"newt_id":     "site-id",
		"newt_secret": "site-secret",
	}
	builtinSamples["authentik"] = map[string]any{
		"db_password": "db-secret",
	}
	builtinSamples["infisical"] = map[string]any{
		"db_password": "db-secret",
	}
	builtinSamples["pocket-id"] = map[string]any{
		"domain": "id.example.test",
	}
	builtinSamples["onetimesecret"] = map[string]any{
		"redis_password": "redis-secret",
	}
}
