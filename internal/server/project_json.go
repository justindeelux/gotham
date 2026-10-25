package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

var errProjectJSONKeys = errors.New("non-canonical or duplicate reference keys")

func projectReferenceKey(key string) string {
	for _, canonical := range []string{"project_id", "environment_id", "application_id", "database_id", "service_id", "deployment_id", "backup_id", "schedule_id", "resource_type", "resource_id", "server_id", "target_id", "private_key_id", "ssh_key_id", "github_app_id", "provider_connection_id"} {
		if strings.EqualFold(key, canonical) {
			return canonical
		}
	}
	return ""
}

// Only top-level references are decoded by the authorizer; nested maps are user data.
func validateProjectJSONKeys(data []byte) error {
	// Valid also enforces encoding/json's nesting limit.
	if !json.Valid(data) {
		return errProjectJSONKeys
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errProjectJSONKeys
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key := token.(string)
		if canonical := projectReferenceKey(key); canonical != "" {
			for _, r := range key {
				if r > 127 {
					return errProjectJSONKeys
				}
			}
			if seen[canonical] {
				return errProjectJSONKeys
			}
			seen[canonical] = true
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	return nil
}
