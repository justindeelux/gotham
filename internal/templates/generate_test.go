package templates

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"testing"

	builtin "github.com/justindeelux/gotham/templates"
)

func TestGenerateSpecValue(t *testing.T) {
	cases := []struct {
		name    string
		spec    Generate
		pattern string
	}{
		{"alphanumeric", Generate{Length: 24, Charset: CharsetAlphanumeric}, `^[A-Za-z0-9]{24}$`},
		{"hex", Generate{Length: 32, Charset: CharsetHex}, `^[0-9a-f]{32}$`},
		{"base64", Generate{Length: 32, Charset: CharsetBase64}, `^[A-Za-z0-9+/]{43}=$`},
		{"base64 with prefix", Generate{Length: 32, Charset: CharsetBase64, Prefix: "base64:"}, `^base64:[A-Za-z0-9+/]{43}=$`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			value, err := testCase.spec.value()
			if err != nil {
				t.Fatalf("value: %v", err)
			}
			if !regexp.MustCompile(testCase.pattern).MatchString(value) {
				t.Errorf("value = %q, want %s", value, testCase.pattern)
			}
			if len(value) != testCase.spec.outputLength() {
				t.Errorf("value length = %d, want %d", len(value), testCase.spec.outputLength())
			}
			second, err := testCase.spec.value()
			if err != nil {
				t.Fatalf("value: %v", err)
			}
			if second == value {
				t.Errorf("two generated values are identical: %q", value)
			}
		})
	}
}

func TestGenerateSpecValidation(t *testing.T) {
	cases := []struct {
		name string
		spec Generate
		want string
	}{
		{"unknown charset", Generate{Length: 32, Charset: "emoji"}, "generate charset must be"},
		{"short alphanumeric", Generate{Length: 15, Charset: CharsetAlphanumeric}, "generate length must be at least 16"},
		{"short base64", Generate{Length: 15, Charset: CharsetBase64}, "generate length must be at least 16"},
		{"too long", Generate{Length: MaxFieldValue, Charset: CharsetBase64}, "exceeds 1024 bytes"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.spec.validate()
			if err == nil || !regexp.MustCompile(testCase.want).MatchString(err.Error()) {
				t.Errorf("validate = %v, want %q", err, testCase.want)
			}
		})
	}
	if err := (Generate{Length: 16, Charset: CharsetBase64}).validate(); err != nil {
		t.Errorf("16 base64 bytes = %v, want no error", err)
	}
}

func TestFieldResolveGeneratesMissingValue(t *testing.T) {
	field := Field{Key: "password", Type: FieldSecret, Generate: &Generate{Length: 32, Charset: CharsetHex}}
	value, err := field.resolve(map[string]any{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(value) {
		t.Errorf("value = %q, want 32 hex characters", value)
	}
	provided, err := field.resolve(map[string]any{"password": "mine"})
	if err != nil || provided != "mine" {
		t.Errorf("resolve(provided) = (%q, %v), want the provided value", provided, err)
	}
}

func TestGenerateSettingJSON(t *testing.T) {
	shorthand := Generate{Length: 24, Charset: CharsetAlphanumeric, shorthand: true}
	encoded, err := json.Marshal(shorthand)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(encoded) != "true" {
		t.Errorf("shorthand JSON = %s, want true", encoded)
	}
	object := Generate{Length: 64, Charset: CharsetHex}
	encoded, err = json.Marshal(object)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(encoded) != `{"length":64,"charset":"hex"}` {
		t.Errorf("object JSON = %s", encoded)
	}
	var decoded Generate
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Length != 64 || decoded.Charset != CharsetHex || decoded.Prefix != "" {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestBuiltinGeneratedFieldsMatchTheirFormat(t *testing.T) {
	catalog, err := Load(builtin.FS)
	if err != nil {
		t.Fatalf("Load(builtin.FS): %v", err)
	}
	for _, template := range catalog.List() {
		t.Run(template.Slug, func(t *testing.T) {
			values := builtinValues(template)
			result, err := catalog.Render(template.Slug, values)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for _, field := range template.Fields {
				if field.Generate == nil || field.Type != FieldSecret {
					continue
				}
				value, generated := result.Env[field.Key]
				if !generated {
					t.Fatalf("generated secret %q is missing from env", field.Key)
				}
				if _, provided := values[field.Key]; provided {
					continue
				}
				if len(value) != field.Generate.outputLength() {
					t.Errorf("%s: generated %d characters, want %d", field.Key, len(value), field.Generate.outputLength())
				}
				body := value[len(field.Generate.Prefix):]
				valid := regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(body)
				switch field.Generate.Charset {
				case CharsetHex:
					valid = regexp.MustCompile(`^[0-9a-f]+$`).MatchString(body)
				case CharsetBase64:
					_, err := base64.StdEncoding.DecodeString(body)
					valid = err == nil
				}
				if !valid {
					t.Errorf("%s: %q is not valid %s", field.Key, value, field.Generate.Charset)
				}
			}
		})
	}
}
