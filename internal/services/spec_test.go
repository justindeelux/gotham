package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestInterpolateForms locks the compose reference forms this package
// supports and the escaping contract: every literal dollar survives the node's
// compose run as a literal dollar.
func TestInterpolateForms(t *testing.T) {
	document := `services:
  web:
    image: nginx
    environment:
      A: ${A}
      B: $B
      C: ${C:-fallback}
      D: ${D-default}
      E: ${E:-}
      F: ${F:+alternative}
      G: "$5 and ${HOME:-/root}"
      H: ${H}
      I: "$${A}"
`
	env := map[string]string{
		"A": "alpha",
		"B": "bravo",
		"C": "charlie",
		"D": "delta",
		"F": "foxtrot",
		"H": "with$dollar$",
	}
	rendered, err := Interpolate(document, env)
	if err != nil {
		t.Fatalf("Interpolate: %v", err)
	}
	spec, err := Parse(rendered)
	if err != nil {
		t.Fatalf("Parse(rendered): %v", err)
	}
	if len(spec.Services) != 1 {
		t.Fatalf("services = %v", spec.Services)
	}
	// Environment values are validated but not recorded; the observable
	// contract is the rendered text.
	for _, want := range []string{
		"A: alpha",
		"B: bravo",
		"C: charlie",
		"D: delta",
		"E: \"\"",
		"F: alternative",
		"G: $$5 and /root",
		"H: with$$dollar$$",
		"I: $${A}",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered document is missing %q:\n%s", want, rendered)
		}
	}
	if err := Validate(rendered, nil); err != nil {
		t.Errorf("Validate(rendered) = %v, want nil (no references may remain)", err)
	}
}

// TestInterpolateErrors proves an unresolvable reference is a validation
// error that names the variable (never a value).
func TestInterpolateErrors(t *testing.T) {
	longValue := strings.Repeat("s", 40)
	cases := map[string]struct {
		document string
		env      map[string]string
		want     string
	}{
		"missing variable": {
			document: "services:\n  web:\n    image: nginx\n    environment:\n      A: ${MISSING}\n",
			want:     "MISSING is required",
		},
		"missing short form": {
			document: "services:\n  web:\n    image: nginx\n    environment:\n      A: $MISSING\n",
			want:     "MISSING is required",
		},
		"error form": {
			document: "services:\n  web:\n    image: nginx\n    environment:\n      A: ${MISSING:?set the port}\n",
			want:     "set the port",
		},
		"empty required by error form": {
			document: "services:\n  web:\n    image: nginx\n    environment:\n      A: ${EMPTY:?}\n",
			env:      map[string]string{"EMPTY": ""},
			want:     "EMPTY is required",
		},
		"unterminated reference": {
			document: "services:\n  web:\n    image: nginx\n    environment:\n      A: ${MISSING\n",
			want:     "unterminated",
		},
		"invalid name": {
			document: "services:\n  web:\n    image: nginx\n    environment:\n      A: ${bad name}\n",
			want:     "invalid variable name",
		},
		"unsupported modifier": {
			document: "services:\n  web:\n    image: nginx\n    environment:\n      A: ${A:1}\n",
			env:      map[string]string{"A": "x"},
			want:     "unsupported reference modifier",
		},
		"document too large": {
			document: "services:\n  web:\n    image: " + longValue + "\n" + strings.Repeat("# padding\n", MaxComposeYAML/10),
			want:     "exceeds",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Interpolate(tc.document, tc.env)
			if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Interpolate = %v, want ErrValidation containing %q", err, tc.want)
			}
		})
	}
}

// TestInterpolateDoesNotEchoValues proves a failing render of a secret-bearing
// document does not return the secret.
func TestInterpolateDoesNotEchoValues(t *testing.T) {
	document := "services:\n  web:\n    image: nginx\n    environment:\n      PASSWORD: ${PASSWORD}\n      MISSING: ${NOPE:?}\n"
	_, err := Interpolate(document, map[string]string{"PASSWORD": "hunter2-secret"})
	if err == nil {
		t.Fatal("Interpolate = nil, want an error")
	}
	if strings.Contains(err.Error(), "hunter2-secret") {
		t.Fatalf("error leaked an environment value: %v", err)
	}
}

// TestParseDomainMap proves the Gotham label convention maps compose services
// to validated hosts with the Phase 6 rules.
func TestParseDomainMap(t *testing.T) {
	document := `services:
  worker:
    image: busybox:1.36
  web:
    image: nginx:1.27
    labels:
      gotham.domain: "App.Example.COM"
      gotham.domain.port: "3000"
  api:
    image: ghcr.io/acme/api:1
    labels:
      - "gotham.domain=api.example.com"
volumes:
  data:
`
	spec, err := Parse(document)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []DomainRoute{
		{Service: "api", Domain: "api.example.com", Port: 80},
		{Service: "web", Domain: "app.example.com", Port: 3000},
	}
	if len(spec.Domains) != len(want) {
		t.Fatalf("domains = %+v, want %+v", spec.Domains, want)
	}
	for i, route := range spec.Domains {
		if route != want[i] {
			t.Errorf("domain[%d] = %+v, want %+v", i, route, want[i])
		}
	}
	if len(spec.Services) != 3 || spec.Services[0] != "api" {
		t.Errorf("services = %v, want sorted [api web worker]", spec.Services)
	}
}

// TestParseRejections locks the schema checks: constructs the control plane
// cannot ship to a node, unsafe domains and reserved volumes are rejected with
// a validation error.
func TestParseRejections(t *testing.T) {
	cases := map[string]struct {
		document string
		want     string
	}{
		"no services": {
			document: "services: {}\n",
			want:     "declares no services",
		},
		"build context": {
			document: "services:\n  web:\n    build: .\n",
			want:     "uses build",
		},
		"env_file": {
			document: "services:\n  web:\n    image: nginx\n    env_file: .env\n",
			want:     "uses env_file",
		},
		"extends": {
			document: "services:\n  web:\n    extends:\n      file: base.yml\n      service: base\n",
			want:     "uses extends",
		},
		"include": {
			document: "include:\n  - other.yml\nservices:\n  web:\n    image: nginx\n",
			want:     "include is not supported",
		},
		"missing image": {
			document: "services:\n  web:\n    labels:\n      gotham.domain: app.example.com\n",
			want:     "declares no image",
		},
		"invalid service name": {
			document: "services:\n  \"web bad\":\n    image: nginx\n",
			want:     "invalid compose service name",
		},
		"invalid domain": {
			document: "services:\n  web:\n    image: nginx\n    labels:\n      gotham.domain: \"bad host\"\n",
			want:     "invalid domain",
		},
		"invalid port": {
			document: "services:\n  web:\n    image: nginx\n    labels:\n      gotham.domain: app.example.com\n      gotham.domain.port: nope\n",
			want:     "must be a port",
		},
		"duplicate domain": {
			document: "services:\n  a:\n    image: nginx\n    labels:\n      gotham.domain: app.example.com\n  b:\n    image: nginx\n    labels:\n      gotham.domain: app.example.com\n",
			want:     "both declare",
		},
		"reserved volume": {
			document: "services:\n  web:\n    image: nginx\n    volumes:\n      - gotham-db-abc:/data\nvolumes:\n  gotham-db-abc:\n",
			want:     "reserved for managed databases",
		},
		"undeclared named volume": {
			document: "services:\n  web:\n    image: nginx\n    volumes:\n      - data:/data\n",
			want:     "without a top-level volumes declaration",
		},
		"bad environment shape": {
			document: "services:\n  web:\n    image: nginx\n    environment: nope\n",
			want:     "environment must be",
		},
		"bad labels shape": {
			document: "services:\n  web:\n    image: nginx\n    labels: nope\n",
			want:     "labels must be",
		},
		"bad volumes shape": {
			document: "services:\n  web:\n    image: nginx\n    volumes: nope\n",
			want:     "volumes must be a list",
		},
		"invalid yaml": {
			document: "services: [this is not\n",
			want:     "not valid YAML",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(tc.document)
			if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse = %v, want ErrValidation containing %q", err, tc.want)
			}
		})
	}
}

// TestParseStorageMounts proves bind mounts and named volumes resolve, and
// that named volumes are reported (they are what survives a down).
func TestParseStorageMounts(t *testing.T) {
	document := `services:
  web:
    image: nginx
    volumes:
      - ./site:/usr/share/nginx/html:ro
      - data:/data
      - cache:/cache
      - /var/log/nginx:/logs
      - /anon
  db:
    image: postgres:16
    volumes:
      - type: volume
        source: data
        target: /var/lib/postgresql/data
volumes:
  data:
  cache:
  unused:
`
	spec, err := Parse(document)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := []string{"cache", "data", "unused"}; strings.Join(spec.NamedVolumes, ",") != strings.Join(want, ",") {
		t.Errorf("named volumes = %v, want %v", spec.NamedVolumes, want)
	}
	byTarget := map[string]StorageMount{}
	for _, mount := range spec.Mounts {
		byTarget[mount.Target] = mount
	}
	readonly := byTarget["/usr/share/nginx/html"]
	if readonly.Service != "web" || readonly.Source != "./site" || !readonly.ReadOnly || readonly.Named {
		t.Errorf("bind mount = %+v", readonly)
	}
	named := byTarget["/data"]
	if !named.Named || named.Source != "data" {
		t.Errorf("named volume = %+v", named)
	}
	absolute := byTarget["/logs"]
	if absolute.Named || absolute.Source != "/var/log/nginx" {
		t.Errorf("absolute bind = %+v", absolute)
	}
	long := byTarget["/var/lib/postgresql/data"]
	if long.Service != "db" || !long.Named || long.Source != "data" {
		t.Errorf("long-form mount = %+v", long)
	}
}

// TestRenderTemplatedDomain proves a domain label can itself be a ${VAR}
// reference and ends up validated after interpolation.
func TestRenderTemplatedDomain(t *testing.T) {
	document := `services:
  web:
    image: nginx
    labels:
      gotham.domain: ${DOMAIN}
    environment:
      DB_PASSWORD: ${DB_PASSWORD}
`
	rendered, err := Render(document, map[string]string{
		"DOMAIN":      "Shop.Example.com",
		"DB_PASSWORD": "s3cret-value",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(rendered.Spec.Domains) != 1 || rendered.Spec.Domains[0].Domain != "shop.example.com" {
		t.Fatalf("domains = %+v", rendered.Spec.Domains)
	}
	if strings.Contains(rendered.ComposeYAML, "${") {
		t.Errorf("rendered document still carries a reference:\n%s", rendered.ComposeYAML)
	}
	if _, err := Render(document, nil); !errors.Is(err, ErrValidation) {
		t.Errorf("Render without env = %v, want ErrValidation", err)
	}
}

// TestRenderRejectsRenderedSecretsInErrors proves a parse failure of the
// rendered document can be redacted at the service boundary.
func TestRenderRedaction(t *testing.T) {
	env := map[string]string{"DB_PASSWORD": "hunter2-secret"}
	message := "parse error at line 3: hunter2-secret is not a string"
	if redacted := Redact(message, env); strings.Contains(redacted, "hunter2-secret") {
		t.Fatalf("Redact = %q, want the value removed", redacted)
	}
	// A short value is left alone on purpose: replacing it would mangle
	// ordinary numbers.
	if redacted := Redact("port 80 is in use", map[string]string{"PORT": "80"}); redacted != "port 80 is in use" {
		t.Fatalf("Redact short value = %q", redacted)
	}
	// The error chain survives redaction so sentinel mapping keeps working.
	err := RedactError(errors.Join(ErrValidation, errors.New(message)), env)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("errors.Is(err, ErrValidation) = false for %v", err)
	}
	if strings.Contains(err.Error(), "hunter2-secret") {
		t.Fatalf("redacted error leaked the value: %v", err)
	}
}

// TestProjectName locks the derived project name the agent re-validates.
func TestProjectName(t *testing.T) {
	id := uuid.MustParse("3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182")
	if got := ProjectName(id); got != "gotham-3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182" {
		t.Fatalf("ProjectName = %q", got)
	}
}
