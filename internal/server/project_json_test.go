package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestProjectJSONKeys(t *testing.T) {
	for _, body := range []string{
		`{"ſerver_id":"hidden"}`,
		`{"\u017ferver_id":"hidden"}`,
		`{"resource_id":"granted","reſource_id":"hidden"}`,
		`{"server_id":null,"server_id":"hidden"}`,
		`{"server_id":null,"SERVER_ID":"hidden"}`,
		`{"config":{"nested":{"reſource_id":"hidden"}}}`,
		`{"config":[{"resource_id":"granted","resource_id":"hidden"}]}`,
		`{"env":{"SERVER_ID":"a","server_id":"b"}}`,
		`{"build_args":{"nested":[{"ſerver_id":"hidden"}]}}`,
		`{"resource_id":"a"} {"resource_id":"b"}`,
	} {
		t.Run(body, func(t *testing.T) {
			if err := validateProjectJSONKeys([]byte(body)); err == nil {
				t.Fatal("ambiguous keys accepted")
			}
			req := httptest.NewRequest("PATCH", "/", strings.NewReader(body)).WithContext(context.Background())
			if (&Server{}).authorizeProjectReferences(req, uuid.New(), projectPolicy{}) {
				t.Fatal("ambiguous body reached dispatch")
			}
		})
	}
	for _, body := range []string{`{"name":"café ſ K","env":{"UPPER":"Unicode ✓"}}`, `{"config":[{"key":"value"}],"enabled":true}`, `{"environment_id":"canonical"}`} {
		if err := validateProjectJSONKeys([]byte(body)); err != nil {
			t.Fatalf("valid JSON rejected: %s: %v", body, err)
		}
	}
}
