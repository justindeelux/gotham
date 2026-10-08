package composeguard

import (
	"strings"
	"testing"
)

const guardAppID = "11111111-1111-1111-1111-111111111111"
const guardRoot = "/var/lib/gotham/volumes"

func guardOpts() Options {
	return Options{AppID: guardAppID, ManagedRoot: guardRoot}
}

func serviceDoc(body string) string {
	return "services:\n  web:\n    image: example.com/app:1.0\n" + body
}

// TestValidateBypassTable feeds every node-escape payload from the GS-8
// review reports and asserts rejection naming the offending path. Each case
// was verified against real `docker compose config` semantics by the
// reviewers; the validator must refuse them all before they reach a node.
func TestValidateBypassTable(t *testing.T) {
	managed := guardRoot + "/" + guardAppID
	cases := []struct {
		name    string
		content string
		want    string // required substring of the error (the offending path)
	}{
		// Host root through volume driver_opts.
		{"driver opts bind", "services:\n  web:\n    image: x\n    volumes:\n      - data:/mnt\nvolumes:\n  data:\n    driver_opts:\n      type: none\n      o: bind\n      device: /\n", "volumes.data"},
		{"driver non-local", "services:\n  web:\n    image: x\n    volumes:\n      - data:/mnt\nvolumes:\n  data:\n    driver: nfs\n", "volumes.data"},
		// Host files through secrets/configs file:.
		{"secrets file", "services:\n  web:\n    image: x\n    secrets:\n      - s\nsecrets:\n  s:\n    file: /etc/hostname\n", "secrets"},
		{"configs file", "services:\n  web:\n    image: x\n    configs:\n      - c\nconfigs:\n  c:\n    file: /etc/hostname\n", "configs"},
		{"secrets external", "services:\n  web:\n    image: x\n    secrets:\n      - s\nsecrets:\n  s:\n    external: true\n", "secrets"},
		// Relative bind escapes (compose treats leading ., /, ~ as paths).
		{"parent bind", serviceDoc("    volumes:\n      - ..:/mnt\n"), "services.web"},
		{"hidden bind", serviceDoc("    volumes:\n      - .hidden:/mnt\n"), "services.web"},
		{"home bind", serviceDoc("    volumes:\n      - ~:/mnt\n"), "services.web"},
		{"home user bind", serviceDoc("    volumes:\n      - ~root:/mnt\n"), "services.web"},
		{"dot bind", serviceDoc("    volumes:\n      - .:/mnt\n"), "services.web"},
		{"dot slash bind", serviceDoc("    volumes:\n      - ./data:/mnt\n"), "services.web"},
		// Capabilities (case and prefix variants).
		{"cap sys admin", serviceDoc("    cap_add:\n      - SYS_ADMIN\n"), "services.web"},
		{"cap prefix", serviceDoc("    cap_add:\n      - CAP_SYS_ADMIN\n"), "services.web"},
		{"cap lower", serviceDoc("    cap_add:\n      - cap_sys_admin\n"), "services.web"},
		{"cap all", serviceDoc("    cap_add:\n      - ALL\n"), "services.web"},
		{"cap net admin", serviceDoc("    cap_add:\n      - NET_ADMIN\n"), "services.web"},
		{"cap sys ptrace", serviceDoc("    cap_add:\n      - SYS_PTRACE\n"), "services.web"},
		{"cap dac override", serviceDoc("    cap_add:\n      - DAC_OVERRIDE\n"), "services.web"},
		{"cap drop rejected", serviceDoc("    cap_drop:\n      - ALL\n"), "services.web"},
		// LSM and misc host configuration.
		{"security opt", serviceDoc("    security_opt:\n      - seccomp=unconfined\n"), "services.web"},
		{"apparmor", serviceDoc("    security_opt:\n      - apparmor=unconfined\n"), "services.web"},
		{"sysctls", serviceDoc("    sysctls:\n      net.core.somaxconn: 1024\n"), "services.web"},
		{"devices", serviceDoc("    devices:\n      - /dev/fuse:/dev/fuse\n"), "services.web"},
		{"device cgroup", serviceDoc("    device_cgroup_rules:\n      - c 10:200 rwm\n"), "services.web"},
		{"gpus", serviceDoc("    gpus: all\n"), "services.web"},
		{"privileged", serviceDoc("    privileged: true\n"), "services.web"},
		{"privileged one", serviceDoc("    privileged: 1\n"), "services.web"},
		// Namespaces (host, container: and service: forms).
		{"network host", serviceDoc("    network_mode: host\n"), "services.web"},
		{"network container", serviceDoc("    network_mode: container:other\n"), "services.web"},
		{"network service", serviceDoc("    network_mode: service:other\n"), "services.web"},
		{"pid host", serviceDoc("    pid: host\n"), "services.web"},
		{"pid container", serviceDoc("    pid: container:other\n"), "services.web"},
		{"ipc host", serviceDoc("    ipc: host\n"), "services.web"},
		{"ipc service", serviceDoc("    ipc: service:other\n"), "services.web"},
		{"userns host", serviceDoc("    userns_mode: host\n"), "services.web"},
		{"uts host", serviceDoc("    uts: host\n"), "services.web"},
		{"cgroup host", serviceDoc("    cgroup: host\n"), "services.web"},
		{"cgroup parent", serviceDoc("    cgroup_parent: /\n"), "services.web"},
		// Cross-container mounts and networks.
		{"volumes from", serviceDoc("    volumes_from:\n      - container:other:rw\n"), "services.web"},
		{"external object", "services:\n  web:\n    image: x\n    volumes:\n      - shared:/data\nvolumes:\n  shared:\n    external:\n      name: gotham-app-other-data\n", "volumes.shared"},
		{"external bool", "services:\n  web:\n    image: x\n    volumes:\n      - shared:/data\nvolumes:\n  shared:\n    external: true\n", "volumes.shared"},
		{"volume name override", "services:\n  web:\n    image: x\n    volumes:\n      - shared:/data\nvolumes:\n  shared:\n    name: shared\n", "volumes.shared"},
		{"reserved db volume", "services:\n  web:\n    image: x\n    volumes:\n      - gotham-db-main:/data\nvolumes:\n  gotham-db-main: {}\n", "gotham-db-main"},
		{"undeclared volume", serviceDoc("    volumes:\n      - missing:/data\n"), "services.web"},
		{"networks external", "services:\n  web:\n    image: x\n    networks:\n      - n\nnetworks:\n  n:\n    external: true\n", "networks.n"},
		{"networks driver", "services:\n  web:\n    image: x\n    networks:\n      - n\nnetworks:\n  n:\n    driver: macvlan\n", "networks.n"},
		{"network undeclared", serviceDoc("    networks:\n      - ghost\n"), "services.web"},
		// Agent environment inheritance.
		{"env bare list", serviceDoc("    environment:\n      - INHERIT_ME\n"), "services.web"},
		{"env null mapping", serviceDoc("    environment:\n      INHERIT_ME:\n"), "services.web"},
		// Gotham label spoofing.
		{"label app id map", serviceDoc("    labels:\n      gotham.app_id: 00000000-0000-0000-0000-000000000000\n"), "services.web"},
		{"label app id list", serviceDoc("    labels:\n      - gotham.app_id=00000000-0000-0000-0000-000000000000\n"), "services.web"},
		{"label domain", serviceDoc("    labels:\n      gotham.domain: evil.example.com\n"), "services.web"},
		// Build and repo inputs the node never receives.
		{"build", serviceDoc("    build: .\n"), "services.web"},
		{"extends", serviceDoc("    extends:\n      service: base\n"), "services.web"},
		{"env file", serviceDoc("    env_file: .env\n"), "services.web"},
		{"container name", serviceDoc("    container_name: web\n"), "services.web"},
		{"extra hosts", serviceDoc("    extra_hosts:\n      - host.docker.internal:host-gateway\n"), "services.web"},
		{"hostname", serviceDoc("    hostname: web\n"), "services.web"},
		{"dns", serviceDoc("    dns:\n      - 8.8.8.8\n"), "services.web"},
		{"stop signal", serviceDoc("    stop_signal: SIGKILL\n"), "services.web"},
		{"profiles", serviceDoc("    profiles:\n      - debug\n"), "services.web"},
		{"platform", serviceDoc("    platform: linux/amd64\n"), "services.web"},
		{"pull policy", serviceDoc("    pull_policy: never\n"), "services.web"},
		{"replicas", serviceDoc("    deploy:\n      replicas: 3\n"), "services.web"},
		{"runtime", serviceDoc("    runtime: nvidia\n"), "services.web"},
		{"ulimits", serviceDoc("    ulimits:\n      nofile: 100\n"), "services.web"},
		// Binds outside the managed directory.
		{"bind etc", serviceDoc("    volumes:\n      - /etc/passwd:/data:ro\n"), "services.web"},
		{"docker sock", serviceDoc("    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n"), "services.web"},
		{"docker sock renamed", serviceDoc("    volumes:\n      - /tmp/docker.sock:/sock\n"), "services.web"},
		{"foreign app bind", serviceDoc("    volumes:\n      - /var/lib/gotham/volumes/22222222-2222-2222-2222-222222222222/data:/data\n"), "services.web"},
		{"nested bind", serviceDoc("    volumes:\n      - " + managed + "/a/b:/data\n"), "services.web"},
		{"long bind type mismatch", "services:\n  web:\n    image: x\n    volumes:\n      - type: volume\n        source: /etc\n        target: /data\n", "services.web"},
		{"subpath escape", "services:\n  web:\n    image: x\n    volumes:\n      - type: volume\n        source: data\n        target: /data\n        volume:\n          subpath: ../escape\nvolumes:\n  data: {}\n", "services.web"},
		// Structure.
		{"top name", "name: myproject\nservices:\n  web:\n    image: x\n", "top-level"},
		{"top include", "include:\n  - other.yml\nservices:\n  web:\n    image: x\n", "top-level"},
		{"top version", "version: \"3\"\nservices:\n  web:\n    image: x\n", "version"},
		{"no image", "services:\n  web:\n    command: sleep infinity\n", "services.web"},
		{"no services", "volumes:\n  data: {}\n", "no services"},
		{"depends undeclared", "services:\n  web:\n    image: x\n    depends_on:\n      - ghost\n", "services.web"},
		{"depends long form", "services:\n  web:\n    image: x\n    depends_on:\n      db:\n        condition: service_healthy\n  db:\n    image: y\n", "services.web"},
		{"logging syslog", serviceDoc("    logging:\n      driver: syslog\n"), "services.web"},
		{"multi document", "services:\n  web:\n    image: x\n---\nservices:\n  evil:\n    image: y\n    privileged: true\n", "single document"},
		{"nul byte", "services:\n  web:\n    image: a\x00b\n", "NUL"},
		{"invalid utf-8", "services:\n  web:\n    image: \xff\xfe\n", "UTF-8"},
		// Round-4 I1: NUL via escape decodes to a real NUL the raw-text
		// check never sees. Every spelling fails closed with the path.
		{"nul escape", "services:\n  web:\n    image: x\n    environment:\n      A: \"x\\0y\"\n", "services.web.environment"},
		{"nul hex escape", "services:\n  web:\n    image: x\n    environment:\n      A: \"x\\x00y\"\n", "services.web.environment"},
		{"nul unicode escape", "services:\n  web:\n    image: x\n    environment:\n      A: \"x\\u0000y\"\n", "services.web.environment"},
		{"nul escape key", "services:\n  web:\n    image: x\n    environment:\n      \"x\\u0000y\": v\n", "services.web.environment"},
		// Round-2 N1: explicitly tagged scalars bypass substitution and
		// interpolate on the node instead. Every non-core tag fails closed.
		{"tagged image", "services:\n  web:\n    image: !x \"example.com/${TAG:-x}:latest\"\n", "!x"},
		{"tagged env", "services:\n  web:\n    image: x\n    environment:\n      A: !x \"${AGENT_SECRET}\"\n", "!x"},
		{"tagged command", "services:\n  web:\n    image: x\n    command: !custom [\"echo\", \"${AGENT_SECRET}\"]\n", "!custom"},
		{"tagged bind", "services:\n  web:\n    image: x\n    volumes:\n      - !x \"/var/lib/gotham/volumes/" + guardAppID + "/${NOPE:-..}:/mnt\"\n", "!x"},
		{"tagged label", "services:\n  web:\n    image: x\n    labels:\n      note: !x \"${AGENT_SECRET}\"\n", "!x"},
		{"binary scalar", "services:\n  web:\n    image: x\n    environment:\n      A: !!binary \"aGk=\"\n", "!!binary"},
		{"timestamp scalar", "services:\n  web:\n    image: x\n    environment:\n      D: !!timestamp \"2024-01-01\"\n", "!!timestamp"},
		// Round-4 M1: published host ports in the document obey the same
		// reserved band as the application's own HostPort pin.
		{"published 22", serviceDoc("    ports:\n      - \"22:22\"\n"), "services.web.ports"},
		{"published 443", serviceDoc("    ports:\n      - \"443:443\"\n"), "services.web.ports"},
		{"published loopback docker", serviceDoc("    ports:\n      - \"127.0.0.1:2375:2375\"\n"), "services.web.ports"},
		{"published 2376", serviceDoc("    ports:\n      - \"2376:2376\"\n"), "services.web.ports"},
		{"published range overlap", serviceDoc("    ports:\n      - \"1-1024:8080\"\n"), "services.web.ports"},
		{"published range docker", serviceDoc("    ports:\n      - \"2370-2380:8080\"\n"), "services.web.ports"},
		{"published sexagesimal", serviceDoc("    ports:\n      - \"1:30\"\n"), "services.web.ports"},
		{"published octal", serviceDoc("    ports:\n      - \"0755:80\"\n"), "services.web.ports"},
		{"published udp", serviceDoc("    ports:\n      - \"22:22/udp\"\n"), "services.web.ports"},
		{"published long 443", "services:\n  web:\n    image: x\n    ports:\n      - target: 80\n        published: 443\n        host_ip: 0.0.0.0\n", "services.web.ports"},
		{"published long range", "services:\n  web:\n    image: x\n    ports:\n      - target: 80\n        published: 800-900\n", "services.web.ports"},
		{"published too many segments", serviceDoc("    ports:\n      - \"a:b:c:d\"\n"), "services.web.ports"},
		// Round-2 N5/N6: short-form parsing matches compose-go: drive
		// letters, propagation modes and relative targets fail closed.
		{"drive letter", "services:\n  web:\n    image: x\n    volumes:\n      - C:/../../..:/m\nvolumes:\n  C: {}\n", "\"C\""},
		{"drive letter long", "services:\n  web:\n    image: x\n    volumes:\n      - type: volume\n        source: C\n        target: /m\nvolumes:\n  C: {}\n", "\"C\""},
		{"propagation mode", serviceDoc("    volumes:\n      - data:/m:rshared\n") + "volumes:\n  data: {}\n", "services.web"},
		{"bad mode", serviceDoc("    volumes:\n      - data:/m:nonsense\n") + "volumes:\n  data: {}\n", "services.web"},
		{"relative target", serviceDoc("    volumes:\n      - data:mnt\n") + "volumes:\n  data: {}\n", "services.web"},
		{"long relative target", "services:\n  web:\n    image: x\n    volumes:\n      - type: volume\n        source: data\n        target: mnt\nvolumes:\n  data: {}\n", "services.web"},
		// Bare and null environment entries inherit the node environment.
		{"env bare list", serviceDoc("    environment:\n      - INHERIT_ME\n"), "services.web"},
		{"env null mapping", serviceDoc("    environment:\n      INHERIT_ME:\n"), "services.web"},
		// Structure: duplicates, leading markers, merge smuggling.
		{"duplicate keys", "services:\n  web:\n    image: x\n    image: y\n", "already defined"},
		{"leading marker multi", "---\nservices:\n  web:\n    image: x\n---\nservices:\n  evil:\n    image: y\n", "single document"},
		{"merge smuggles privileged", "services:\n  web:\n    image: x\n    <<:\n      privileged: true\n", "services.web"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.content, guardOpts())
			if err == nil {
				t.Fatalf("Validate accepted %q, want rejection", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

// TestValidatePublishedBandAccepts proves the band's allow side: ephemeral
// mappings, high published ports and references judged after render all
// pass the raw gate, like ordinary applications.
func TestValidatePublishedBandAccepts(t *testing.T) {
	accept := map[string]string{
		"ephemeral bare":     serviceDoc("    ports:\n      - \"3000\"\n"),
		"ephemeral range":    serviceDoc("    ports:\n      - \"3000-3005\"\n"),
		"high published":     serviceDoc("    ports:\n      - \"8080:80\"\n"),
		"high published ip":  serviceDoc("    ports:\n      - \"0.0.0.0:8080:80\"\n"),
		"high range":         serviceDoc("    ports:\n      - \"8000-8010:8000-8010\"\n"),
		"reference raw":      serviceDoc("    ports:\n      - \"${PUB}:80\"\n"),
		"long ephemeral":     "services:\n  web:\n    image: x\n    ports:\n      - target: 80\n",
		"long high":          "services:\n  web:\n    image: x\n    ports:\n      - target: 80\n        published: 8080\n",
		"long loopback high": "services:\n  web:\n    image: x\n    ports:\n      - target: 80\n        published: 8080\n        host_ip: 127.0.0.1\n",
	}
	for name, content := range accept {
		t.Run(name, func(t *testing.T) {
			if err := Validate(content, guardOpts()); err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
		})
	}
}

// TestValidateBillionLaughs pins the alias bomb guard: hundreds of alias
// references must fail before the decoder expands them without bound.
func TestValidateBillionLaughs(t *testing.T) {
	var bomb strings.Builder
	bomb.WriteString("services:\n  web:\n    image: x\n    expose:\n      - &p \"3000\"\n")
	for i := 0; i < 200; i++ {
		bomb.WriteString("      - *p\n")
	}
	err := Validate(bomb.String(), guardOpts())
	if err == nil {
		t.Fatal("Validate accepted an alias bomb, want rejection")
	}
	if !strings.Contains(err.Error(), "alias") {
		t.Errorf("error = %q, want the alias guard named", err)
	}
	// A handful of aliases stays usable.
	const few = "services:\n  web:\n    image: x\n    expose:\n      - &p \"3000\"\n      - *p\n      - *p\n"
	if err := Validate(few, guardOpts()); err != nil {
		t.Fatalf("Validate few aliases = %v, want nil", err)
	}
}

// TestValidateDeepNesting pins the depth guard.
func TestValidateDeepNesting(t *testing.T) {
	deep := "services:\n  web:\n    image: x\n    labels:\n"
	for i := 0; i < 200; i++ {
		deep += "      k" + string(rune('0'+i%10)) + ":\n"
	}
	deep += "        leaf: x\n"
	if err := Validate(deep, guardOpts()); err == nil {
		t.Fatal("Validate accepted 200-deep nesting, want rejection")
	}
}

// TestValidateRealisticApp pins the allowlist's usable surface: a web +
// database project with named volumes, healthchecks, dependencies, limits
// and project-local networking must pass untouched.
func TestValidateRealisticApp(t *testing.T) {
	const app = `services:
  web:
    image: example.com/app:1.0
    command: ["serve", "--port", "3000"]
    environment:
      DATABASE_URL: postgres://db:5432/app
      WORKERS: "4"
      DEBUG: false
    env_file: []
    ports:
      - "8080:3000"
      - target: 9090
        published: 9091
        protocol: tcp
    expose:
      - "3000"
    volumes:
      - app-data:/var/lib/app
      - type: tmpfs
        target: /tmp
    depends_on:
      - db
    restart: unless-stopped
    working_dir: /srv/app
    user: "1000:1000"
    init: true
    tty: false
    stdin_open: false
    stop_grace_period: 30s
    read_only: true
    tmpfs:
      - /run
    mem_limit: 512m
    cpus: 1.5
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:3000/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
    logging:
      driver: json-file
      options:
        max-size: 10m
        max-file: "3"
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
        reservations:
          memory: 128M
    labels:
      com.example.team: search
    networks:
      - front
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_PASSWORD_FILE: /run/secrets/dbpass
    volumes:
      - db-data:/var/lib/postgresql/data
    restart: always
    healthcheck:
      test: pg_isready -U postgres
      disable: false
volumes:
  app-data: {}
  db-data:
    driver: local
networks:
  front: {}
`
	// POSTGRES_PASSWORD_FILE is a path, not a secret reference: file-based
	// env values stay inside the container. The env_file entry is empty and
	// must still be rejected (the key is never allowed).
	if err := Validate(app, guardOpts()); err == nil {
		t.Fatal("Validate accepted env_file, want rejection")
	}
	fixed := strings.Replace(app, "    env_file: []\n", "", 1)
	if err := Validate(fixed, guardOpts()); err != nil {
		t.Fatalf("Validate realistic app = %v, want nil", err)
	}
}

// TestValidateOptions pins the scope inputs: binds need a UUID app and an
// absolute root, or every bind fails closed.
func TestValidateOptions(t *testing.T) {
	const doc = "services:\n  web:\n    image: x\n    volumes:\n      - /var/lib/gotham/volumes/11111111-1111-1111-1111-111111111111/data:/data\n"
	if err := Validate(doc, guardOpts()); err != nil {
		t.Fatalf("managed bind = %v, want nil", err)
	}
	if err := Validate(doc, Options{AppID: "not-a-uuid", ManagedRoot: guardRoot}); err == nil {
		t.Fatal("bad app id accepted binds, want rejection")
	}
	if err := Validate(doc, Options{AppID: guardAppID, ManagedRoot: "relative/root"}); err == nil {
		t.Fatal("relative root accepted, want rejection")
	}
	if err := Validate("services:\n  web:\n    image: x\n", Options{}); err == nil {
		t.Fatal("empty options accepted, want rejection")
	}
}

// TestCheckNoInterpolation pins the rendered-document gate: every dollar
// must be a $$ escape, wherever it hides (image, command, bind sources,
// environment, labels, keys). Raw documents with live references fail it
// by design; only rendered documents pass.
func TestCheckNoInterpolation(t *testing.T) {
	passing := []string{
		"services:\n  web:\n    image: example.com/app:1.0\n",
		"services:\n  web:\n    image: x\n    environment:\n      PRICE: $$5\n      PAIR: a$$b$$c\n",
		"services:\n  web:\n    image: x\n    command: echo $$HOME\n",
	}
	for _, doc := range passing {
		if err := CheckNoInterpolation(doc); err != nil {
			t.Errorf("CheckNoInterpolation = %v, want nil for %q", err, doc)
		}
	}
	failing := []struct {
		name string
		doc  string
		want string
	}{
		{"lone dollar", "services:\n  web:\n    image: x\n    environment:\n      PRICE: $5\n", "services.web.environment"},
		{"reference", "services:\n  web:\n    image: example.com/${TAG}\n", "services.web.image"},
		{"default escape", "services:\n  web:\n    image: x\n    volumes:\n      - /data/${NOPE:-..}:/mnt\n", "services.web.volumes"},
		{"odd run", "services:\n  web:\n    image: x\n    environment:\n      A: $$$VAR\n", "services.web.environment"},
		{"command", "services:\n  web:\n    image: x\n    command: echo $HOME\n", "services.web.command"},
		{"label", "services:\n  web:\n    image: x\n    labels:\n      note: $V\n", "services.web.labels"},
		{"key", "services:\n  web:\n    image: x\n    environment:\n      $KEY: v\n", "services.web.environment"},
	}
	for _, tc := range failing {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckNoInterpolation(tc.doc)
			if err == nil {
				t.Fatalf("CheckNoInterpolation accepted %q, want rejection", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

// TestValidateHiddenTail pins F1: a first valid document must not hide a
// second one the decoder chokes on. yaml.v3 refuses implicit keys past
// 1024 characters while compose accepts and merges them, so any decode
// error after the first document fails closed instead of ending the scan.
func TestValidateHiddenTail(t *testing.T) {
	longKey := "x-" + strings.Repeat("k", 1100)
	reviewer := "services: {web: {image: nginx}}\n---\nservices:\n  evil:\n    image: busybox\n    privileged: true\n    volumes: [\"/:/host\"]\n" + longKey + ": 1\n"
	cases := []struct {
		name    string
		content string
	}{
		{"reviewer payload", reviewer},
		{"long key tail", "services:\n  web:\n    image: x\n---\nservices:\n  evil:\n    image: y\n    privileged: true\n" + longKey + ": 1\n"},
		{"bad escape tail", "services:\n  web:\n    image: x\n---\nservices:\n  evil:\n    image: \"\\q\"\n"},
		{"second valid document", "services:\n  web:\n    image: x\n---\nservices:\n  evil:\n    image: y\n"},
		{"tab tail", "services:\n  web:\n    image: x\n---\nservices:\n\tevil:\n    image: y\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.content, guardOpts()); err == nil {
				t.Fatalf("Validate accepted a hidden tail (%s), want rejection", tc.name)
			}
		})
	}

	// Decode-error tails fail every scan closed: no loop may mistake them
	// for end-of-stream. (A clean second document passes the per-document
	// scans; Validate above is the single-document gate.)
	t.Run("decode error tails fail all scans", func(t *testing.T) {
		for _, tc := range cases {
			if tc.name == "second valid document" {
				continue
			}
			if err := CheckNoInterpolation(tc.content); err == nil {
				t.Errorf("CheckNoInterpolation accepted %s, want rejection", tc.name)
			}
			if err := CheckSweepLabels(tc.content); err == nil {
				t.Errorf("CheckSweepLabels accepted %s, want rejection", tc.name)
			}
		}
	})
}

// TestValidateEmptyTail pins L2: a trailing document marker with no content
// (or only a comment) is genuinely empty and accepted, while any non-empty
// extra document is rejected by the single-document gate above.
func TestValidateEmptyTail(t *testing.T) {
	accept := map[string]string{
		"trailing marker": "services:\n  web:\n    image: x\n---\n",
		"comment tail":    "services:\n  web:\n    image: x\n---\n# nothing here\n",
		"leading marker":  "---\nservices:\n  web:\n    image: x\n",
	}
	for name, content := range accept {
		t.Run(name, func(t *testing.T) {
			if err := Validate(content, guardOpts()); err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
		})
	}
}
