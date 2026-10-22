package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The helper process exercises real stdin/stdout without replacing global descriptors.
func TestMCPProcessHelper(_ *testing.T) {
	if os.Getenv("GOTHAM_TEST_MCP_PROCESS") != "1" {
		return
	}
	os.Exit(run(strings.Fields(os.Getenv("GOTHAM_TEST_MCP_ARGS"))))
}

func TestMCPDispatchAndStdout(t *testing.T) {
	const team = "11111111-1111-4111-8111-111111111111"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/context" {
			t.Error("unexpected CP request")
		}
		w.Header().Set("X-Gotham-MCP-Contract", "1")
		w.Header().Set("X-Gotham-MCP-Team-Id", team)
		_, _ = fmt.Fprintf(w, `{"team_id":%q,"capabilities":["context","strict_reads"]}`, team)
	}))
	defer api.Close()
	for _, tc := range []struct {
		name, args, token, input string
		want                     int
	}{
		{"usage", "mcp", "gt1_test", "", 2},
		{"JWT rejected", "mcp serve", "eyJhbGciOi.secret", "", 1},
		{"lifecycle", "mcp serve", "gt1_test", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}` + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPProcessHelper$")
			cmd.Env = append(os.Environ(), "GOTHAM_TEST_MCP_PROCESS=1", "GOTHAM_TEST_MCP_ARGS="+tc.args, "GOTHAM_MCP_URL="+api.URL, "GOTHAM_MCP_TOKEN="+tc.token, "GOTHAM_MCP_TEAM_ID="+team, "GOTHAM_DATABASE_DSN=invalid", "GOTHAM_CA_DIR=/invalid")
			var stdout, stderr bytes.Buffer
			cmd.Stderr = &stderr
			var err error
			if tc.want == 0 {
				stdin, pipeErr := cmd.StdinPipe()
				if pipeErr != nil {
					t.Fatal(pipeErr)
				}
				output, pipeErr := cmd.StdoutPipe()
				if pipeErr != nil {
					t.Fatal(pipeErr)
				}
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				scanner := bufio.NewScanner(output)
				for _, line := range strings.Split(strings.TrimSpace(tc.input), "\n") {
					if _, err := fmt.Fprintln(stdin, line); err != nil {
						t.Fatal(err)
					}
					if strings.Contains(line, "notifications/initialized") {
						continue
					}
					if !scanner.Scan() {
						t.Fatal("missing protocol response")
					}
					stdout.Write(scanner.Bytes())
					stdout.WriteByte('\n')
				}
				if err := stdin.Close(); err != nil {
					t.Fatal(err)
				}
				for scanner.Scan() {
					stdout.Write(scanner.Bytes())
					stdout.WriteByte('\n')
				}
				err = cmd.Wait()
			} else {
				cmd.Stdout = &stdout
				err = cmd.Run()
			}
			code := 0
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != tc.want {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			if strings.Contains(stderr.String(), tc.token) || strings.Contains(stdout.String(), tc.token) {
				t.Fatal("credential leaked")
			}
			scanner := bufio.NewScanner(&stdout)
			frames := 0
			for scanner.Scan() {
				var frame map[string]any
				if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil || frame["jsonrpc"] != "2.0" {
					t.Fatalf("stdout corruption: %s", scanner.Text())
				}
				frames++
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if tc.want != 0 && frames != 0 {
				t.Fatal("failure wrote stdout")
			}
			if tc.want == 0 && frames != 2 {
				t.Fatalf("protocol responses=%d want 2", frames)
			}
		})
	}
}
