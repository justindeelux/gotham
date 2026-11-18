package templates

const (
	sampleHex32 = "0123456789abcdef0123456789abcdef"
	sampleHex64 = sampleHex32 + sampleHex32
	sampleB64   = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
)

func init() {
	builtinSamples["newt-pangolin"] = map[string]any{
		"endpoint":    "https://pangolin.example.test",
		"newt_id":     "site-id",
		"newt_secret": "site-secret",
	}
	builtinSamples["authentik"] = map[string]any{
		"secret_key":  sampleHex64,
		"db_password": "db-secret",
	}
	builtinSamples["infisical"] = map[string]any{
		"encryption_key": sampleHex32,
		"auth_secret":    sampleB64,
		"db_password":    "db-secret",
	}
	builtinSamples["pocket-id"] = map[string]any{
		"domain":         "id.example.test",
		"encryption_key": sampleB64,
	}
	builtinSamples["onetimesecret"] = map[string]any{
		"secret":         sampleHex64,
		"redis_password": "redis-secret",
	}
}
