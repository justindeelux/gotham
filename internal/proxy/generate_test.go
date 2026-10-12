package proxy

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Rules embed backticks (Host(`example.com`)), which Go raw strings cannot
// carry; goldens use the placeholder below and expand it before comparing.
const backtickPlaceholder = "§"

// expandGolden turns the readable golden placeholder into the real character.
func expandGolden(raw string) string {
	return strings.ReplaceAll(raw, backtickPlaceholder, "`")
}

// goldenAppID is a fixed application id so generated names are stable.
const goldenAppID = "11111111-2222-3333-4444-555555555555"

const goldenStaticYAML = `entryPoints:
  traefik:
    address: :8080
  web:
    address: :80
  websecure:
    address: :443
ping:
  entryPoint: traefik
providers:
  file:
    directory: /etc/traefik/dynamic
    watch: true
certificatesResolvers:
  letsencrypt:
    acme:
      storage: /acme/acme.json
      httpChallenge:
        entryPoint: web
`

const goldenDynamicYAML = `http:
  routers:
    app-11111111-2222-3333-4444-555555555555-web:
      rule: Host(§app.example.com§)
      service: app-11111111-2222-3333-4444-555555555555
      entryPoints:
        - web
  services:
    app-11111111-2222-3333-4444-555555555555:
      loadBalancer:
        servers:
          - url: http://172.17.0.1:3000
`

const goldenStaticTOML = `[entryPoints]
[entryPoints.traefik]
address = ':8080'

[entryPoints.web]
address = ':80'

[entryPoints.websecure]
address = ':443'

[ping]
entryPoint = 'traefik'

[providers]
[providers.file]
directory = '/etc/traefik/dynamic'
watch = true

[certificatesResolvers]
[certificatesResolvers.letsencrypt]
[certificatesResolvers.letsencrypt.acme]
storage = '/acme/acme.json'

[certificatesResolvers.letsencrypt.acme.httpChallenge]
entryPoint = 'web'
`

const goldenDynamicTOML = `[http]
[http.routers]
[http.routers.app-11111111-2222-3333-4444-555555555555-web]
rule = 'Host(§app.example.com§)'
service = 'app-11111111-2222-3333-4444-555555555555'
entryPoints = ['web']

[http.services]
[http.services.app-11111111-2222-3333-4444-555555555555]
[http.services.app-11111111-2222-3333-4444-555555555555.loadBalancer]
[[http.services.app-11111111-2222-3333-4444-555555555555.loadBalancer.servers]]
url = 'http://172.17.0.1:3000'
`

// sampleConfig is the generation input the goldens above were rendered from.
func sampleConfig() ProxyConfig {
	return BuildConfig([]Route{{
		AppID:  uuid.MustParse(goldenAppID),
		Domain: "app.example.com",
		Target: "http://172.17.0.1:3000",
	}}, nil, nil, "", "")
}

func TestGenerateGolden(t *testing.T) {
	cases := []struct {
		format  Format
		static  string
		dynamic string
	}{
		{format: FormatYAML, static: goldenStaticYAML, dynamic: goldenDynamicYAML},
		{format: FormatTOML, static: goldenStaticTOML, dynamic: goldenDynamicTOML},
	}
	for _, tc := range cases {
		files, err := Generate(sampleConfig(), tc.format)
		if err != nil {
			t.Fatalf("%s: Generate: %v", tc.format, err)
		}
		if len(files) != 2 {
			t.Fatalf("%s: got %d files, want 2", tc.format, len(files))
		}
		for _, file := range files {
			var want string
			switch file.Name {
			case StaticFileName(tc.format):
				want = expandGolden(tc.static)
			case DynamicFileName(tc.format):
				want = expandGolden(tc.dynamic)
			default:
				t.Fatalf("%s: unexpected file %q", tc.format, file.Name)
			}
			if string(file.Content) != want {
				t.Errorf("%s: %s mismatch\n--- got ---\n%s\n--- want ---\n%s",
					tc.format, file.Name, file.Content, want)
			}
		}
	}
}

// TestUploadReadTimeoutIsGated is the fix-round-1 regression: the long upload
// read timeout must not be applied to every tenant route by default. With the
// file feature off, the public entrypoints keep Traefik's 60 s default (no
// respondingTimeouts); applyUploadReadTimeout — which the sync applies only when
// the file feature is on — adds the generous timeout, and the loopback ping
// entrypoint is never touched.
func TestUploadReadTimeoutIsGated(t *testing.T) {
	cfg := BuildConfig(nil, nil, nil, "", "")
	for _, name := range []string{EntryPointWeb, EntryPointWebSecure} {
		entry, ok := cfg.EntryPoints[name]
		if !ok {
			t.Fatalf("entrypoint %q missing", name)
		}
		if entry.Transport != nil {
			t.Errorf("entrypoint %q must keep Traefik's default read timeout when files are off, got %+v", name, entry.Transport)
		}
	}

	applyUploadReadTimeout(&cfg)
	for _, name := range []string{EntryPointWeb, EntryPointWebSecure} {
		entry := cfg.EntryPoints[name]
		if entry.Transport == nil || entry.Transport.RespondingTimeouts == nil {
			t.Fatalf("entrypoint %q has no respondingTimeouts after applyUploadReadTimeout", name)
		}
		if got := entry.Transport.RespondingTimeouts.ReadTimeout; got != EntryPointReadTimeout {
			t.Errorf("entrypoint %q readTimeout = %q, want %q", name, got, EntryPointReadTimeout)
		}
	}
	if internal := cfg.EntryPoints[EntryPointInternal]; internal.Transport != nil {
		t.Errorf("internal entrypoint should not set transport, got %+v", internal.Transport)
	}
}

// TestSyncServiceGatesUploadReadTimeout proves the sync applies the long read
// timeout only when the file feature is enabled, so a node with files off keeps
// the default read window.
func TestSyncServiceGatesUploadReadTimeout(t *testing.T) {
	for _, tc := range []struct {
		name         string
		filesUploads bool
		wantTimeout  bool
	}{
		{"files off keeps the default", false, false},
		{"files on widens the timeout", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &SyncService{filesUploads: tc.filesUploads}
			cfg := svc.buildConfig(nil, nil, nil)
			entry := cfg.EntryPoints[EntryPointWeb]
			if tc.wantTimeout {
				if entry.Transport == nil || entry.Transport.RespondingTimeouts == nil ||
					entry.Transport.RespondingTimeouts.ReadTimeout != EntryPointReadTimeout {
					t.Fatalf("web entrypoint = %+v, want readTimeout %q", entry.Transport, EntryPointReadTimeout)
				}
				return
			}
			if entry.Transport != nil {
				t.Fatalf("web entrypoint = %+v, want no transport", entry.Transport)
			}
		})
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	first := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	second := uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa")
	routes := []Route{
		{AppID: first, Domain: "one.example.com", Target: "http://172.17.0.1:3000"},
		{AppID: second, Domain: "two.example.com", Target: "http://172.17.0.1:3001"},
	}
	reversed := []Route{routes[1], routes[0]}

	for _, format := range []Format{FormatYAML, FormatTOML} {
		firstRun, err := Generate(BuildConfig(routes, nil, nil, "", ""), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		secondRun, err := Generate(BuildConfig(reversed, nil, nil, "", ""), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if len(firstRun) != len(secondRun) {
			t.Fatalf("%s: file count changed", format)
		}
		for i := range firstRun {
			if firstRun[i].Name != secondRun[i].Name {
				t.Fatalf("%s: file %d name changed: %q vs %q", format, i, firstRun[i].Name, secondRun[i].Name)
			}
			if string(firstRun[i].Content) != string(secondRun[i].Content) {
				t.Errorf("%s: %s differs between runs:\n%s\n---\n%s",
					format, firstRun[i].Name, firstRun[i].Content, secondRun[i].Content)
			}
		}
	}
}

// TestGenerateEmptyConfigIsValidDocument proves a node with no routes renders
// a document with no http section at all: Traefik rejects a standalone empty
// `http:` element ("http cannot be a standalone element") and would then keep
// serving the previous document, so a removed last route would never
// disappear. The live Phase 7 acceptance asserts the removal through Traefik.
func TestGenerateEmptyConfigIsValidDocument(t *testing.T) {
	files, err := Generate(BuildConfig(nil, nil, nil, "", ""), FormatYAML)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dynamic := string(files[1].Content)
	if strings.Contains(dynamic, "http") {
		t.Errorf("empty dynamic document = %q, want no http section", dynamic)
	}
	if strings.TrimSpace(dynamic) != "{}" {
		t.Errorf("empty dynamic document = %q, want an empty document", dynamic)
	}

	files, err = Generate(BuildConfig(nil, nil, nil, "", ""), FormatTOML)
	if err != nil {
		t.Fatalf("Generate(toml): %v", err)
	}
	if dynamic := string(files[1].Content); strings.Contains(dynamic, "http") {
		t.Errorf("empty TOML document = %q, want no http section", dynamic)
	}
}

func TestGenerateRejectsUnknownFormat(t *testing.T) {
	if _, err := Generate(ProxyConfig{}, Format("json")); err == nil {
		t.Fatal("Generate(format json) = nil error, want unsupported format")
	}
}

func TestStaticAndDynamicFileNames(t *testing.T) {
	if name := StaticFileName(FormatYAML); name != "traefik.yml" {
		t.Errorf("StaticFileName(yaml) = %q", name)
	}
	if name := StaticFileName(FormatTOML); name != "traefik.toml" {
		t.Errorf("StaticFileName(toml) = %q", name)
	}
	if name := DynamicFileName(FormatYAML); name != "dynamic/gotham.yml" {
		t.Errorf("DynamicFileName(yaml) = %q", name)
	}
	if name := DynamicFileName(FormatTOML); name != "dynamic/gotham.toml" {
		t.Errorf("DynamicFileName(toml) = %q", name)
	}
}
