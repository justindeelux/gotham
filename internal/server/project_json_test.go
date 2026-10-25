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
		`{"private_Key_id":"hidden"}`,
		`{"\u017ferver_id":"hidden"}`,
		`{"resource_id":"granted","reſource_id":"hidden"}`,
		`{"server_id":null,"server_id":"hidden"}`,
		`{"server_id":null,"SERVER_ID":"hidden"}`,
		`{"resource_id":"a"} {"resource_id":"b"}`,
		`{"config":` + strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001) + "}",
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
	for _, body := range []string{`{"env":{"http_proxy":"a","HTTP_PROXY":"b","ſerver_id":"literal"},"labels":{"A":1,"a":2},"headers":{"X-Test":"a","x-test":"b"}}`, `{"config":{"resource_id":"a","resource_id":"b"},"Name":"a","name":"b","café":true}`, `{"name":"café ſ K","env":{"UPPER":"Unicode ✓"}}`, `{"config":[{"key":"value"}],"enabled":true}`, `{"environment_id":"canonical"}`} {
		if err := validateProjectJSONKeys([]byte(body)); err != nil {
			t.Fatalf("valid JSON rejected: %s: %v", body, err)
		}
	}
}
