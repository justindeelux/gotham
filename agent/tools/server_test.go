package tools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/toolcatalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToolServerListTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	response, err := NewToolServer().ListTools(context.Background(), &agentv1.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(response.GetTools()) != len(toolcatalog.List()) {
		t.Fatalf("ListTools returned %d tools; want %d", len(response.GetTools()), len(toolcatalog.List()))
	}
	installed := make(map[string]bool)
	managed := make(map[string]bool)
	for _, tool := range response.GetTools() {
		if tool.GetId() == "" || tool.GetName() == "" || tool.GetDescription() == "" || tool.GetCategory() == "" {
			t.Errorf("tool %q is missing identity fields: %v", tool.GetId(), tool)
		}
		installed[tool.GetId()] = tool.GetInstalled()
		managed[tool.GetId()] = tool.GetInstallable() || tool.GetUninstallable()
	}
	if len(installed) != len(toolcatalog.List()) {
		t.Fatalf("ListTools returned %d distinct ids; want %d", len(installed), len(toolcatalog.List()))
	}
	for id, present := range installed {
		if present {
			t.Errorf("tool %q reported installed on an empty PATH", id)
		}
	}
	if managed[toolcatalog.IDNetworkctl] {
		t.Error("networkctl is offered for install/uninstall; want detect-only")
	}
}

func TestToolServerListToolsOffersNetworkStackPerPackageManager(t *testing.T) {
	network := []string{toolcatalog.IDSystemdNetworkd, toolcatalog.IDSystemdResolved, toolcatalog.IDResolvectl}
	for _, tt := range []struct {
		binary string
		want   bool
	}{
		{"apt-get", true},
		{"dnf", true},
		{"apk", false},
	} {
		t.Run(tt.binary, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tt.binary), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatalf("write fake %s: %v", tt.binary, err)
			}
			t.Setenv("PATH", dir)

			response, err := NewToolServer().ListTools(context.Background(), &agentv1.ListToolsRequest{})
			if err != nil {
				t.Fatalf("ListTools: %v", err)
			}
			seen := 0
			for _, tool := range response.GetTools() {
				if !slices.Contains(network, tool.GetId()) {
					continue
				}
				seen++
				if tool.GetInstallable() != tt.want {
					t.Errorf("%s installable = %v with %s on PATH; want %v", tool.GetId(), tool.GetInstallable(), tt.binary, tt.want)
				}
				if tool.GetUninstallable() {
					t.Errorf("%s is uninstallable; Gotham never removes the host network stack", tool.GetId())
				}
			}
			if seen != len(network) {
				t.Fatalf("ListTools reported %d of %d network tools", seen, len(network))
			}
		})
	}
}

func TestToolServerListToolsReportsDetection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "railpack"), []byte("#!/bin/sh\necho 'railpack 0.4.1'\n"), 0o755); err != nil {
		t.Fatalf("write fake railpack: %v", err)
	}
	t.Setenv("PATH", dir)

	response, err := NewToolServer().ListTools(context.Background(), &agentv1.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range response.GetTools() {
		if tool.GetId() != toolcatalog.IDRailpack {
			continue
		}
		if !tool.GetInstalled() || tool.GetVersion() != "0.4.1" || tool.GetPath() == "" {
			t.Fatalf("railpack = %v; want installed 0.4.1 with a path", tool)
		}
		if !tool.GetVersionOk() {
			t.Error("railpack version_ok = false; want true (no minimum)")
		}
		return
	}
	t.Fatal("ListTools omitted railpack")
}

func TestToolServerListToolsReportsMinVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho 'git version 2.30.0'\n"), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", dir)

	response, err := NewToolServer().ListTools(context.Background(), &agentv1.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range response.GetTools() {
		if tool.GetId() != toolcatalog.IDGit {
			continue
		}
		if tool.GetMinVersion() != "2.37" || tool.GetVersion() != "2.30.0" || tool.GetVersionOk() {
			t.Fatalf("git = %v; want min 2.37, version 2.30.0, version_ok false", tool)
		}
		return
	}
	t.Fatal("ListTools omitted git")
}

func TestToolServerInstallDetectOnly(t *testing.T) {
	server := NewToolServer(withToolRunner(&fakeToolRunner{}))
	request := &agentv1.InstallToolRequest{Id: toolcatalog.IDNetworkctl}
	if err := server.InstallTool(request, newFakeToolStream(context.Background())); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("InstallTool(networkctl) = %v; want FailedPrecondition", err)
	}
	if err := server.UninstallTool(request, newFakeToolStream(context.Background())); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("UninstallTool(networkctl) = %v; want FailedPrecondition", err)
	}
}
