package deploy

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeWorkingDir(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty is the repository root", "", ""},
		{"blank is the repository root", "   ", ""},
		{"one folder", "docs", "docs"},
		{"nested folder", "apps/web", "apps/web"},
		{"surrounding blanks", "  apps/web\t", "apps/web"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeWorkingDir(tc.in)
			if err != nil {
				t.Fatalf("normalizeWorkingDir(%q) = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("normalizeWorkingDir(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeWorkingDirRejectsEscapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"parent", ".."},
		{"parent prefix", "../x"},
		{"parent segment", "apps/../../x"},
		{"absolute", "/etc"},
		{"backslash", `apps\web`},
		{"git folder", ".git"},
		{"git subfolder", "apps/.git/objects"},
		{"empty segment", "apps//web"},
		{"dot segment", "./apps"},
		{"too long", strings.Repeat("a", maxWorkingDirLen+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeWorkingDir(tc.in); !errors.Is(err, ErrValidation) {
				t.Errorf("normalizeWorkingDir(%q) err = %v, want ErrValidation", tc.in, err)
			}
		})
	}
}

// A working directory only reaches the build when the app reads a clone, so
// pasted and image sources keep an empty value.
func TestRepoBacked(t *testing.T) {
	cases := []struct {
		name string
		app  Application
		want bool
	}{
		{"public git", Application{SourceType: SourceGitPublic, CloneURL: "https://github.com/acme/demo.git"}, true},
		{"private git", Application{SourceType: SourceGitPrivate, CloneURL: "https://git.example.com/acme/demo.git"}, true},
		{"github app", Application{SourceType: SourceGitHubApp, Provider: "github", CloneURL: "https://github.com/acme/demo.git"}, true},
		{"compose from a repository", Application{SourceType: SourceCompose, CloneURL: "https://github.com/acme/demo.git", ComposeFile: "docker-compose.yml"}, true},
		{"pasted compose", Application{SourceType: SourceCompose, ComposeContent: "services: {}"}, false},
		{"dockerfile text", Application{SourceType: SourceDockerfile}, false},
		{"prebuilt image", Application{SourceType: SourceImage, ImageRef: "nginx:1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoBacked(tc.app); got != tc.want {
				t.Errorf("repoBacked(%+v) = %v, want %v", tc.app, got, tc.want)
			}
		})
	}
}
