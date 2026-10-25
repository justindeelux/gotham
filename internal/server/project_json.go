package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
)

var errProjectJSONKeys = errors.New("non-canonical or duplicate JSON keys")

// Check keys with encoding/json before decoding into maps can discard duplicates.
// ASCII keys make the authorizer's case folding agree with Go's struct decoder;
// string values may still contain Unicode.
func validateProjectJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateProjectJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errProjectJSONKeys
	}
	return nil
}

func validateProjectJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return errProjectJSONKeys
			}
			for _, r := range key {
				if r > unicode.MaxASCII {
					return errProjectJSONKeys
				}
			}
			folded := strings.ToLower(key)
			if seen[folded] {
				return errProjectJSONKeys
			}
			seen[folded] = true
			if err := validateProjectJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := validateProjectJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errProjectJSONKeys
	}
	_, err = decoder.Token()
	return err
}
