package templates

func init() {
	builtinSamples["newt-pangolin"] = map[string]any{
		"endpoint":    "https://pangolin.example.test",
		"newt_id":     "site-id",
		"newt_secret": "site-secret",
	}
}
