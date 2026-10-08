package deploy

import (
	"strings"
	"testing"
)

func TestParseImageReference(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name      string
		ref       string
		wantName  string
		wantTag   string
		wantPin   string
		wantError string
	}{
		{name: "docker hub library", ref: "nginx:1.25.3", wantName: "nginx", wantTag: "1.25.3"},
		{name: "namespaced", ref: "library/nginx", wantName: "library/nginx"},
		{name: "registry with port", ref: "registry.example.com:5000/team/app:1.2", wantName: "registry.example.com:5000/team/app", wantTag: "1.2"},
		{name: "digest pinned", ref: "nginx@" + digest, wantName: "nginx", wantPin: digest},
		{name: "tag and digest", ref: "registry.example.com/team/app:1.2@" + digest, wantName: "registry.example.com/team/app", wantTag: "1.2", wantPin: digest},
		{name: "latest is valid (wizard warns, API accepts)", ref: "nginx:latest", wantName: "nginx", wantTag: "latest"},
		{name: "implicit latest", ref: "nginx", wantName: "nginx"},
		{name: "separators", ref: "my-registry.io/a.b_c-d/e:1.0-beta.2", wantName: "my-registry.io/a.b_c-d/e", wantTag: "1.0-beta.2"},
		{name: "localhost registry refused", ref: "localhost:5000/app:1", wantError: "loopback"},
		{name: "localhost bare refused", ref: "localhost/app:1", wantError: "loopback"},
		{name: "localhost trailing dot refused", ref: "localhost./x:1", wantError: "loopback"},
		{name: "localhost subdomain refused", ref: "evil.localhost/x:1", wantError: "loopback"},
		{name: "loopback refused", ref: "127.0.0.1:5000/app:1", wantError: "loopback"},
		{name: "loopback 127.0.0.2 refused", ref: "127.0.0.2/app:1", wantError: "loopback"},
		{name: "loopback shorthand refused", ref: "127.1/x:1", wantError: "loopback"},
		{name: "loopback shorthand 3-part refused", ref: "127.0.1/x:1", wantError: "loopback"},
		{name: "unspecified refused", ref: "0.0.0.0:5000/x:1", wantError: "unspecified"},
		{name: "unspecified v6 refused", ref: "[::]:5000/x:1", wantError: "unspecified"},
		{name: "loopback v6 refused", ref: "[::1]:5000/x:1", wantError: "loopback"},
		{name: "unbracketed v6 refused", ref: "::1/x:1", wantError: "invalid"},
		{name: "dash-leading host refused", ref: "-v.evil/x:1", wantError: "invalid"},
		{name: "double-dash host refused", ref: "--privileged.x/y:1", wantError: "invalid"},
		{name: "empty label refused", ref: "a..b/x:1", wantError: "invalid"},
		{name: "underscore host refused", ref: "reg_x.example.com/x:1", wantError: "invalid"},
		{name: "port zero refused", ref: "reg.example.com:0/x:1", wantError: "port"},
		{name: "port too big refused", ref: "reg.example.com:99999/x:1", wantError: "port"},
		{name: "port non-numeric refused", ref: "reg.example.com:http/x:1", wantError: "port"},
		{name: "empty port refused", ref: "reg.example.com:/x:1", wantError: "port"},
		{name: "multi-colon host refused", ref: "registry:abc:123/foo:1.0", wantError: "invalid"},
		{name: "private ipv4 allowed", ref: "192.168.1.10:5000/app:1", wantName: "192.168.1.10:5000/app", wantTag: "1"},
		{name: "single-label host with port allowed", ref: "reg:5000/app:1", wantName: "reg:5000/app", wantTag: "1"},
		{name: "empty", ref: "", wantError: "required"},
		{name: "whitespace only", ref: "   ", wantError: "required"},
		{name: "inner whitespace", ref: "ng inx:1", wantError: "whitespace"},
		{name: "newline smuggling", ref: "nginx:1\nEVIL=1", wantError: "whitespace"},
		{name: "empty tag", ref: "nginx:", wantError: "tag"},
		{name: "bad tag chars", ref: "nginx:a/b", wantError: "port"},
		{name: "uppercase path", ref: "Team/App:1", wantError: "lowercase"},
		{name: "empty component", ref: "team//app:1", wantError: "component"},
		{name: "bad digest", ref: "nginx@sha256:zzz", wantError: "digest"},
		{name: "short digest", ref: "nginx@sha256:abc", wantError: "digest"},
		{name: "non-sha256 digest", ref: "nginx@sha512:" + strings.Repeat("a", 128), wantError: "digest"},
		{name: "double digest", ref: "nginx@" + digest + "@" + digest, wantError: "more than one digest"},
		{name: "bad port", ref: "registry.example.com:http/app:1", wantError: "port"},
		{name: "too long", ref: strings.Repeat("a", 260) + ":1", wantError: "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseImageReference(tt.ref)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("ParseImageReference(%q) = nil; want error containing %q", tt.ref, tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v; want it to mention %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseImageReference(%q) = %v; want success", tt.ref, err)
			}
			if got.Name != tt.wantName || got.Tag != tt.wantTag || got.Digest != tt.wantPin {
				t.Errorf("got %+v; want name %q tag %q digest %q", got, tt.wantName, tt.wantTag, tt.wantPin)
			}
		})
	}
}

func TestPinnedImageReference(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got, err := PinnedImageReference("registry.example.com/team/app:1.2", digest)
	if err != nil {
		t.Fatalf("PinnedImageReference: %v", err)
	}
	if want := "registry.example.com/team/app@" + digest; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	// An empty digest keeps the tag: releases recorded before digest
	// resolution still roll back.
	got, err = PinnedImageReference("nginx:1.25", "")
	if err != nil || got != "nginx:1.25" {
		t.Errorf("empty digest = %q, %v; want the tag unchanged", got, err)
	}
	// A pinned reference re-pins to the new digest, dropping the old one.
	got, err = PinnedImageReference("nginx:1.25@"+digest, "sha256:"+strings.Repeat("f", 64))
	if err != nil {
		t.Fatalf("re-pin: %v", err)
	}
	if want := "nginx@sha256:" + strings.Repeat("f", 64); got != want {
		t.Errorf("re-pin = %q; want %q", got, want)
	}
	if _, err := PinnedImageReference("not a ref", digest); err == nil {
		t.Error("PinnedImageReference(bad ref) = nil; want error")
	}
	if _, err := PinnedImageReference("nginx:1", "nope"); err == nil {
		t.Error("PinnedImageReference(bad digest) = nil; want error")
	}
}
