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
	}}, nil, "")
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

func TestGenerateIsDeterministic(t *testing.T) {
	first := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	second := uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa")
	routes := []Route{
		{AppID: first, Domain: "one.example.com", Target: "http://172.17.0.1:3000"},
		{AppID: second, Domain: "two.example.com", Target: "http://172.17.0.1:3001"},
	}
	reversed := []Route{routes[1], routes[0]}

	for _, format := range []Format{FormatYAML, FormatTOML} {
		firstRun, err := Generate(BuildConfig(routes, nil, ""), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		secondRun, err := Generate(BuildConfig(reversed, nil, ""), format)
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

func TestGenerateEmptyConfigIsValidDocument(t *testing.T) {
	files, err := Generate(BuildConfig(nil, nil, ""), FormatYAML)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dynamic := string(files[1].Content)
	if !strings.Contains(dynamic, "http: {}") {
		t.Errorf("empty dynamic document = %q, want empty http section", dynamic)
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
