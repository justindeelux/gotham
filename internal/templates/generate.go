package templates

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

const (
	alphanumericAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	hexAlphabet          = "0123456789abcdef"
)

// Generate describes the value the engine makes for a field that has none.
//
// Length counts output characters for the alphanumeric and hex charsets and
// random bytes for base64, which is standard padded base64 (4*len/3
// characters). Prefix is prepended literally and is not counted in Length.
type Generate struct {
	Length  int
	Charset string
	Prefix  string

	// shorthand marks `generate: true`, serialised back as a boolean so the
	// web client keeps receiving the flag it already knows.
	shorthand bool
}

// MarshalJSON keeps `generate: true` a boolean and sends the object form as
// {length, charset, prefix}.
func (g Generate) MarshalJSON() ([]byte, error) {
	if g.shorthand {
		return []byte("true"), nil
	}
	return json.Marshal(struct {
		Length  int    `json:"length"`
		Charset string `json:"charset"`
		Prefix  string `json:"prefix,omitempty"`
	}{Length: g.Length, Charset: g.Charset, Prefix: g.Prefix})
}

// UnmarshalJSON reads both forms back.
func (g *Generate) UnmarshalJSON(data []byte) error {
	if string(data) == "true" {
		*g = Generate{Length: defaultGeneratedLength, Charset: CharsetAlphanumeric, shorthand: true}
		return nil
	}
	var object struct {
		Length  int    `json:"length"`
		Charset string `json:"charset"`
		Prefix  string `json:"prefix"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	*g = Generate{Length: object.Length, Charset: object.Charset, Prefix: object.Prefix}
	return nil
}

func (g Generate) outputLength() int {
	if g.Charset == CharsetBase64 {
		return len(g.Prefix) + base64.StdEncoding.EncodedLen(g.Length)
	}
	return len(g.Prefix) + g.Length
}

func (g Generate) validate() error {
	switch g.Charset {
	case CharsetAlphanumeric, CharsetHex, CharsetBase64:
	default:
		return fmt.Errorf("generate charset must be %s, %s or %s", CharsetAlphanumeric, CharsetHex, CharsetBase64)
	}
	if g.Length < minGeneratedLength {
		return fmt.Errorf("generate length must be at least %d", minGeneratedLength)
	}
	if g.outputLength() > MaxFieldValue {
		return fmt.Errorf("a generated value of length %d exceeds %d bytes", g.outputLength(), MaxFieldValue)
	}
	return nil
}

// value builds a fresh random value. Callers validate the spec first.
func (g Generate) value() (string, error) {
	if g.Charset == CharsetBase64 {
		raw := make([]byte, g.Length)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		return g.Prefix + base64.StdEncoding.EncodeToString(raw), nil
	}
	alphabet := alphanumericAlphabet
	if g.Charset == CharsetHex {
		alphabet = hexAlphabet
	}
	chars, err := randomChars(alphabet, g.Length)
	if err != nil {
		return "", err
	}
	return g.Prefix + chars, nil
}

// randomChars rejection-samples bytes above the largest multiple of the
// alphabet size, so every character is equally likely.
func randomChars(alphabet string, length int) (string, error) {
	limit := 256 / len(alphabet) * len(alphabet)
	out := make([]byte, 0, length)
	buffer := make([]byte, length)
	for len(out) < length {
		if _, err := rand.Read(buffer); err != nil {
			return "", err
		}
		for _, b := range buffer {
			if int(b) >= limit {
				continue
			}
			out = append(out, alphabet[int(b)%len(alphabet)])
			if len(out) == length {
				break
			}
		}
	}
	return string(out), nil
}

// generateSetting decodes both `generate: true` and the object form.
type generateSetting struct {
	spec *Generate
}

func (s *generateSetting) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		var enabled bool
		if err := node.Decode(&enabled); err != nil {
			return errors.New("generate must be true or a mapping of length, charset and prefix")
		}
		if enabled {
			s.spec = &Generate{Length: defaultGeneratedLength, Charset: CharsetAlphanumeric, shorthand: true}
		}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("generate must be true or a mapping of length, charset and prefix")
	}
	spec := Generate{Length: defaultGeneratedLength, Charset: CharsetAlphanumeric}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key, value := node.Content[index].Value, node.Content[index+1]
		var err error
		switch key {
		case "length":
			err = value.Decode(&spec.Length)
		case "charset":
			err = value.Decode(&spec.Charset)
		case "prefix":
			err = value.Decode(&spec.Prefix)
		default:
			return fmt.Errorf("generate has no key %q", key)
		}
		if err != nil {
			return fmt.Errorf("generate %s: %v", key, err)
		}
	}
	s.spec = &spec
	return nil
}
