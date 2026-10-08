// Package composeguard validates compose documents for confined execution.
//
// It is imported by the control plane (internal/deploy) AND the node agent
// (agent/): it must stay neutral and import nothing Gotham-internal, so the
// agent never gains an internal/ edge. Only the standard library and YAML
// are used.
//
// The validator is an allowlist: known-safe service keys and top-level
// sections pass, and anything else fails closed with a path-qualified
// error. Application containers are confined exactly like ordinary
// application containers (managed bind directory, no host namespaces,
// devices, capabilities or LSM changes), so the document a confined
// project runs can never escape the node or reach another tenant.
package composeguard

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Bounds of the structural walk. The input itself is size-capped by the
// caller (the control plane caps pasted text, the agent caps the RPC
// document); these bound what one document can make the decoder do.
const (
	// maxServices caps a document's service count, matching the Services and
	// agent validation bounds.
	maxServices = 256
	// maxAliasNodes caps YAML alias expansions: a billion-laughs payload
	// needs thousands, a hand-written document needs a handful.
	maxAliasNodes = 128
	// maxDepth caps mapping/sequence nesting: real documents stay under ten.
	maxDepth = 64
)

// Options scopes one validation run.
type Options struct {
	// AppID owns the managed binds: every host-path bind must be a direct
	// child of ManagedDir(ManagedRoot, AppID). It must be a canonical
	// lowercase UUID; any other value refuses every bind.
	AppID string
	// ManagedRoot is the absolute parent directory of the per-application
	// bind directories (for example /var/lib/gotham/volumes). The caller
	// passes its own configured root, so the control plane and the node
	// each enforce the root they trust.
	ManagedRoot string
}

// Validate checks a compose document for confined execution without running
// anything. It rejects what Postgres text columns reject (NUL, invalid
// UTF-8), multi-document files (extra documents must never be silently
// dropped), alias/depth bombs, and every structural element outside the
// allowlist, naming the offending path.
func Validate(content string, opts Options) error {
	if strings.ContainsRune(content, 0) {
		return fmt.Errorf("compose document must not contain NUL bytes")
	}
	if !utf8.ValidString(content) {
		return fmt.Errorf("compose document must be valid UTF-8")
	}
	root, appID, err := opts.valid()
	if err != nil {
		return err
	}
	doc, err := singleDocument(content)
	if err != nil {
		return err
	}
	var decoded map[string]any
	if err := doc.Decode(&decoded); err != nil {
		return fmt.Errorf("compose document is not valid YAML: %v", err)
	}
	v := &validator{root: root, appID: appID}
	return v.document(decoded)
}

// valid normalizes the options: a cleaned absolute root and a UUID app id.
// Anything else fails closed before any document is accepted.
func (o Options) valid() (root, appID string, err error) {
	root = filepath.Clean(strings.TrimSpace(o.ManagedRoot))
	if root == "" || !strings.HasPrefix(root, "/") {
		return "", "", fmt.Errorf("confined validation needs an absolute managed root")
	}
	appID = strings.TrimSpace(o.AppID)
	if !uuidPattern.MatchString(appID) {
		return "", "", fmt.Errorf("confined validation needs an application id")
	}
	return root, appID, nil
}

// uuidPattern is the canonical lowercase UUID alphabet both the control
// plane (uuid.String) and the agent project pattern produce.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// composeServiceNamePattern is the compose service identifier alphabet,
// mirroring the Services surface and the agent.
var composeServiceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// namedVolumePattern matches a named volume reference (no path separators,
// no leading dot), mirroring the Services surface.
var namedVolumePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// singleDocument decodes exactly one YAML document and walks its node tree
// for alias and depth bombs before any map decoding expands them. Only
// io.EOF ends the stream: any other decode error (an over-long key, a bad
// escape, a tab) fails closed, because compose is more lenient than this
// decoder and would run a tail document this loop never saw.
func singleDocument(content string) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var docs []*yaml.Node
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("compose document is not valid YAML: %v", err)
		}
		// A trailing empty document (a lone `---` marker, or one carrying
		// only comments) decodes to a document node with no content or a
		// single null child; it carries nothing and is ignored like the
		// CLI ignores it. Any other extra document fails closed below.
		if isEmptyDocument(&node) {
			continue
		}
		docs = append(docs, &node)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("compose document is required")
	}
	if len(docs) > 1 {
		return nil, fmt.Errorf("compose document must be a single document, found %d", len(docs))
	}
	w := &walker{}
	if err := w.walk(docs[0], 0); err != nil {
		return nil, err
	}
	return docs[0], nil
}

// isEmptyDocument reports whether a decoded document node carries no
// content: no children at all, or a single null scalar (a trailing `---`
// marker, or one with only comments).
func isEmptyDocument(node *yaml.Node) bool {
	if node.Kind != yaml.DocumentNode {
		return false
	}
	if len(node.Content) == 0 {
		return true
	}
	if len(node.Content) != 1 {
		return false
	}
	child := node.Content[0]
	return child.Kind == yaml.ScalarNode && child.Tag == "!!null"
}

// walker counts alias expansions and nesting depth of one node tree,
// and rejects explicitly tagged scalars. A custom tag (`!x "..."`) skips
// the control plane's substitution (which only normalizes core scalars)
// and reaches the node's compose with a live ${...} reference, so tagged
// values fail closed here with the tag named.
type walker struct {
	aliases int
	path    []string
}

// walk visits every node, failing closed on alias and depth bombs and on
// non-core tags. The path tracks mapping keys and sequence indices for
// error messages.
func (w *walker) walk(node *yaml.Node, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("compose document exceeds max nesting depth %d", maxDepth)
	}
	switch node.Kind {
	case yaml.AliasNode:
		w.aliases++
		if w.aliases > maxAliasNodes {
			return fmt.Errorf("compose document exceeds max alias expansions %d", maxAliasNodes)
		}
		// The alias target is walked where it is defined; counting the
		// reference is what bounds the expansion.
		return nil
	case yaml.ScalarNode:
		if !coreScalarTag(node.Tag) {
			return fmt.Errorf("%s: scalar with tag %q is not allowed", w.describe(), node.Tag)
		}
		// An escape (`"\0"`, `"\x00"`, `"\u0000"`) decodes to a real NUL
		// byte the raw-text check never sees. The daemon's behavior with
		// NUL in values is undefined and the error column rejects it, so
		// literals fail closed here with the path (never the value).
		if strings.ContainsRune(node.Value, 0) {
			return fmt.Errorf("%s: value must not contain NUL bytes", w.describe())
		}
	case yaml.MappingNode:
		if node.Tag != "" && node.Tag != "!!map" {
			return fmt.Errorf("%s: mapping with tag %q is not allowed", w.describe(), node.Tag)
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			w.push(scalarString(nodeValue(key)))
			if err := w.walk(key, depth+1); err != nil {
				return err
			}
			if err := w.walk(node.Content[i+1], depth+1); err != nil {
				return err
			}
			w.pop()
		}
		return nil
	case yaml.SequenceNode:
		if node.Tag != "" && node.Tag != "!!seq" {
			return fmt.Errorf("%s: sequence with tag %q is not allowed", w.describe(), node.Tag)
		}
		for i, child := range node.Content {
			w.pushf("%d", i)
			if err := w.walk(child, depth+1); err != nil {
				return err
			}
			w.pop()
		}
		return nil
	}
	for _, child := range node.Content {
		if err := w.walk(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// push records one path component, pushf a formatted one, pop removes the
// last one, and describe renders the current document path.
func (w *walker) push(component string) { w.path = append(w.path, component) }

// pushf records one formatted path component.
func (w *walker) pushf(format string, args ...any) { w.push(fmt.Sprintf(format, args...)) }

// pop removes the last recorded path component.
func (w *walker) pop() { w.path = w.path[:len(w.path)-1] }

// describe renders the current document path for error messages.
func (w *walker) describe() string {
	if len(w.path) == 0 {
		return "compose document"
	}
	return strings.Join(w.path, ".")
}

// coreScalarTag reports whether a scalar tag is a resolved core type the
// substitution understands, or the merge-key tag the decoder honors before
// validation (merged keys are validated like written ones). Anything else
// — custom tags, binary, timestamps — fails closed: an unrecognized tag
// must never decide what reaches the node uninterpolated.
func coreScalarTag(tag string) bool {
	switch tag {
	case "", "!!str", "!!int", "!!float", "!!bool", "!!null", "!!merge":
		return true
	default:
		return false
	}
}

// nodeValue renders one node for path tracking without failing.
func nodeValue(node *yaml.Node) any {
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return "?"
}

// validator carries the per-run confinement scope.
type validator struct {
	root  string
	appID string
}

// managedDir is the directory every bind of the application must live under.
func (v *validator) managedDir() string {
	return filepath.Join(v.root, v.appID)
}

// at prefixes an error with the document path of the offending value, so a
// rejection names where the document breaks the allowlist.
func at(path, format string, args ...any) error {
	return fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...))
}

// document validates the top-level sections: services, volumes and networks
// only. The obsolete `version` key gets its own message because legacy files
// carry it and "unknown section" alone would not say what to do.
func (v *validator) document(doc map[string]any) error {
	for key := range doc {
		switch key {
		case "services", "volumes", "networks":
		case "version":
			return fmt.Errorf("top-level version is obsolete and not supported: remove the version key")
		default:
			return fmt.Errorf("unknown top-level section %q", key)
		}
	}
	services, ok := doc["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return fmt.Errorf("compose document declares no services")
	}
	if len(services) > maxServices {
		return fmt.Errorf("compose document declares more than %d services", maxServices)
	}
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	declaredVolumes, err := v.topVolumes(doc["volumes"])
	if err != nil {
		return err
	}
	declaredNetworks, err := v.topNetworks(doc["networks"])
	if err != nil {
		return err
	}
	for _, name := range names {
		if !composeServiceNamePattern.MatchString(name) {
			return fmt.Errorf("invalid compose service name %q", name)
		}
		service, ok := services[name].(map[string]any)
		if !ok {
			return at("services."+name, "service is not a mapping")
		}
		if err := v.service(name, service, declaredVolumes, declaredNetworks, names); err != nil {
			return err
		}
	}
	return nil
}

// topVolumes validates the top-level volumes section: plain declarations
// only. A `driver` other than local, `driver_opts` (a local bind in
// disguise), `external` in any form and `name` overrides (which bypass the
// per-project prefix compose applies) are all rejected. It answers the
// declared names for the service reference check.
func (v *validator) topVolumes(raw any) (map[string]bool, error) {
	declared := map[string]bool{}
	if raw == nil {
		return declared, nil
	}
	volumes, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top-level volumes is not a mapping")
	}
	for name, definition := range volumes {
		if !namedVolumePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid volume name %q", name)
		}
		if err := v.reservedVolume(name); err != nil {
			return nil, err
		}
		if definition == nil {
			declared[name] = true
			continue
		}
		mapping, ok := definition.(map[string]any)
		if !ok {
			return nil, at("volumes."+name, "volume definition is not a mapping")
		}
		for key := range mapping {
			switch key {
			case "driver":
			default:
				return nil, at("volumes."+name, "volume key %q is not allowed", key)
			}
		}
		if driver, ok := mapping["driver"]; ok && scalarString(driver) != "local" {
			return nil, at("volumes."+name, "volume driver %q is not allowed: only the default local driver", scalarString(driver))
		}
		declared[name] = true
	}
	return declared, nil
}

// reservedVolume rejects the managed-database volume namespace: without it
// a project could mount (and a forced removal later delete) a managed
// database's volume.
func (v *validator) reservedVolume(name string) error {
	if strings.HasPrefix(name, "gotham-db-") {
		return fmt.Errorf("volume name %q is reserved for managed databases", name)
	}
	return nil
}

// topNetworks validates the top-level networks section: declarations only,
// without any settings. Custom network configuration (external networks,
// drivers, options) would join node networks the project must never reach;
// services that need isolation use the project's default network, which
// compose already scopes per project. It answers the declared names for
// the service reference check.
func (v *validator) topNetworks(raw any) (map[string]bool, error) {
	declared := map[string]bool{}
	if raw == nil {
		return declared, nil
	}
	networks, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top-level networks is not a mapping")
	}
	for name, definition := range networks {
		if !namedVolumePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid network name %q", name)
		}
		if definition == nil {
			declared[name] = true
			continue
		}
		mapping, ok := definition.(map[string]any)
		if !ok {
			return nil, at("networks."+name, "network definition is not a mapping")
		}
		if len(mapping) != 0 {
			return nil, at("networks."+name, "custom network settings are not supported")
		}
		declared[name] = true
	}
	return declared, nil
}

// scalarString renders a YAML scalar the way compose interpolates it.
func scalarString(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

// sweepLabels are the Gotham container labels a compose document must never
// set: they steer the deploy container sweeps (delete, move, orphan
// reconciliation), so a user-supplied value could aim a teardown at another
// application's containers. The full validator rejects every gotham.-
// prefixed label; this lighter check covers unconfined (Services) documents,
// which legitimately use labels like gotham.domain for routing.
var sweepLabels = []string{"gotham.app_id", "gotham.deployment_id"}

// CheckSweepLabels rejects a compose document that sets a sweep label on
// any service, in mapping or list form. It runs on every node document
// regardless of confinement; confined documents additionally fail the full
// allowlist on any gotham.-prefixed label.
func CheckSweepLabels(content string) error {
	if strings.ContainsRune(content, 0) {
		return fmt.Errorf("compose document must not contain NUL bytes")
	}
	if !utf8.ValidString(content) {
		return fmt.Errorf("compose document must be valid UTF-8")
	}
	decoder := yaml.NewDecoder(strings.NewReader(content))
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("compose document is not valid YAML: %v", err)
		}
		services, ok := doc["services"].(map[string]any)
		if !ok {
			continue
		}
		for name, raw := range services {
			service, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			labels, ok := service["labels"]
			if !ok {
				continue
			}
			if err := checkSweepLabelValues(name, labels); err != nil {
				return err
			}
		}
	}
}

// checkSweepLabelValues rejects one service's sweep labels.
func checkSweepLabelValues(service string, labels any) error {
	denied := func(key string) error {
		for _, reserved := range sweepLabels {
			if key == reserved {
				return fmt.Errorf("services.%s: label %q is reserved for Gotham", service, key)
			}
		}
		return nil
	}
	switch entries := labels.(type) {
	case map[string]any:
		for key := range entries {
			if err := denied(key); err != nil {
				return err
			}
		}
	case []any:
		for _, entry := range entries {
			text, ok := entry.(string)
			if !ok {
				continue
			}
			key, _, _ := strings.Cut(text, "=")
			if err := denied(strings.TrimSpace(key)); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckNoInterpolation verifies that a rendered document carries no live
// variable reference: every `$` in every key and value must be part of a
// `$$` escape, which is what the control plane's Render produces for every
// literal dollar. A lone `$` means the text would interpolate on the node
// — from the agent's process environment, after validation — so it fails
// closed naming the path. Raw (pre-render) documents legitimately contain
// `${VAR}` references and must never pass this check; it runs on rendered
// documents only: the control plane after Render, and the node on whatever
// it is about to hand to `docker compose`.
func CheckNoInterpolation(content string) error {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	documents := 0
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("compose document is not valid YAML: %v", err)
		}
		documents++
		if err := checkScalars(doc, nil); err != nil {
			return err
		}
	}
	if documents == 0 {
		return fmt.Errorf("compose document is required")
	}
	return nil
}

// checkScalars rejects every lone `$` in the keys and string values below
// node, tracking the document path for the error.
func checkScalars(node any, path []string) error {
	describe := func() string {
		if len(path) == 0 {
			return "compose document"
		}
		return strings.Join(path, ".")
	}
	switch value := node.(type) {
	case map[string]any:
		for key, entry := range value {
			if strings.ContainsRune(key, 0) {
				return fmt.Errorf("%s: key must not contain NUL bytes", describe())
			}
			if at := loneDollar(key); at >= 0 {
				return fmt.Errorf("%s: unescaped $ in key %q", describe(), key)
			}
			if err := checkScalars(entry, append(path, key)); err != nil {
				return err
			}
		}
	case []any:
		for i, entry := range value {
			if err := checkScalars(entry, append(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
	case string:
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("%s: value must not contain NUL bytes", describe())
		}
		if at := loneDollar(value); at >= 0 {
			return fmt.Errorf("%s: unescaped $ would interpolate on the node", describe())
		}
	}
	return nil
}

// loneDollar reports the index of the first `$` in s that is not part of a
// `$$` escape, or -1 when every dollar is escaped.
func loneDollar(s string) int {
	for i := 0; i < len(s); {
		if s[i] != '$' {
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			i += 2
			continue
		}
		return i
	}
	return -1
}
