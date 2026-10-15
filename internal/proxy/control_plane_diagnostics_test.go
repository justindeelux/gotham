package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestControlPlaneDiagnosticsBelongToLocalhost(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		readErr   error
	}{
		{name: "invalid URL", raw: "ftp://panel.example.com"},
		{name: "invalid host", raw: "https://bad_host.example.com"},
		{name: "read error", readErr: errors.New("settings unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := panelFixture(t)
			remote := uuid.New()
			f.service.nodes = panelNodes{[]NamedNode{{ID: f.serverID, Name: "localhost"}, {ID: remote, Name: "remote"}}}
			f.service.controlPlaneURL = func(context.Context) (string, error) { return tc.raw, tc.readErr }
			var logs bytes.Buffer
			f.service.logger = slog.New(slog.NewTextHandler(&logs, nil))
			ctx := context.Background()
			if err := f.service.SyncServer(ctx, remote); err != nil {
				t.Fatalf("remote sync=%v", err)
			}
			rows, err := f.service.ListRouters(ctx, remote)
			if err != nil || len(rows.Diagnostics) != 0 || len(rows.Nodes) != 1 || len(rows.Nodes[0].Diagnostics) != 0 {
				t.Fatalf("remote list=%+v err=%v", rows, err)
			}
			if strings.Contains(logs.String(), "control-plane route skipped") {
				t.Fatalf("remote warning: %s", logs.String())
			}
			results, err := f.service.SyncAll(ctx)
			if err != nil || len(results) != 2 {
				t.Fatalf("SyncAll=%+v err=%v", results, err)
			}
			for _, result := range results {
				if result.ServerID == remote && (result.Error != "" || len(result.Diagnostics) != 0) {
					t.Fatalf("remote result=%+v", result)
				}
				if result.ServerID == f.serverID && (result.Error == "" || len(result.Diagnostics) != 1) {
					t.Fatalf("localhost result=%+v", result)
				}
			}
			if strings.Count(logs.String(), "control-plane route skipped") != 1 {
				t.Fatalf("sync warnings: %s", logs.String())
			}
			logs.Reset()
			rows, err = f.service.ListRouters(ctx, uuid.Nil)
			if err != nil || len(rows.Diagnostics) != 0 || len(rows.Nodes) != 2 {
				t.Fatalf("global list=%+v err=%v", rows, err)
			}
			for _, node := range rows.Nodes {
				if node.ServerID == remote && len(node.Diagnostics) != 0 {
					t.Fatalf("remote diagnostics=%+v", node)
				}
				if node.ServerID == f.serverID && len(node.Diagnostics) != 1 {
					t.Fatalf("localhost diagnostics=%+v", node)
				}
			}
			if strings.Count(logs.String(), "control-plane route skipped") != 1 {
				t.Fatalf("list warnings: %s", logs.String())
			}
		})
	}
}

func TestMissingLocalhostDiagnosticIsGlobal(t *testing.T) {
	f := panelFixture(t)
	f.service.nodes = panelNodes{[]NamedNode{{ID: f.serverID, Name: "remote-a"}, {ID: uuid.New(), Name: "remote-b"}}}
	var logs bytes.Buffer
	f.service.logger = slog.New(slog.NewTextHandler(&logs, nil))
	results, err := f.service.SyncAll(context.Background())
	var partial *PartialError
	if !errors.As(err, &partial) || len(partial.Diagnostics) != 1 || len(results) != 2 {
		t.Fatalf("SyncAll=%+v err=%v", results, err)
	}
	for _, result := range results {
		if result.Error != "" || len(result.Diagnostics) != 0 {
			t.Fatalf("healthy remote=%+v", result)
		}
	}
	if strings.Count(logs.String(), "control-plane route skipped") != 1 {
		t.Fatalf("sync warnings: %s", logs.String())
	}
	logs.Reset()
	rows, err := f.service.ListRouters(context.Background(), uuid.Nil)
	if err != nil || len(rows.Diagnostics) != 1 || len(rows.Nodes) != 2 {
		t.Fatalf("ListRouters=%+v err=%v", rows, err)
	}
	for _, node := range rows.Nodes {
		if node.Error != "" || len(node.Diagnostics) != 0 {
			t.Fatalf("healthy remote=%+v", node)
		}
	}
	if strings.Count(logs.String(), "control-plane route skipped") != 1 {
		t.Fatalf("list warnings: %s", logs.String())
	}
	// The sync endpoint preserves the successful node results alongside the global diagnostic.
	recorder := postSync(t, newRoutes(&fakeProxyService{allResults: results, allErr: &PartialError{Diagnostics: rows.Diagnostics}}), "")
	var response syncResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || len(response.Diagnostics) != 1 || len(response.Results) != 2 {
		t.Fatalf("status=%d response=%+v", recorder.Code, response)
	}
	for _, result := range response.Results {
		if !result.Synced || result.Error != "" || len(result.Diagnostics) != 0 {
			t.Fatalf("remote wire result=%+v", result)
		}
	}
}
