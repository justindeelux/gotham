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

const (
	maxServices   = 256
	maxAliasNodes = 128
	maxDepth      = 64
)

type Options struct {
	AppID       string
	ManagedRoot string
}

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

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var composeServiceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

var namedVolumePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

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

type walker struct {
	aliases int
	path    []string
}

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
		return nil
	case yaml.ScalarNode:
		if !coreScalarTag(node.Tag) {
			return fmt.Errorf("%s: scalar with tag %q is not allowed", w.describe(), node.Tag)
		}
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

func (w *walker) push(component string) { w.path = append(w.path, component) }

func (w *walker) pushf(format string, args ...any) { w.push(fmt.Sprintf(format, args...)) }

func (w *walker) pop() { w.path = w.path[:len(w.path)-1] }

func (w *walker) describe() string {
	if len(w.path) == 0 {
		return "compose document"
	}
	return strings.Join(w.path, ".")
}

func coreScalarTag(tag string) bool {
	switch tag {
	case "", "!!str", "!!int", "!!float", "!!bool", "!!null", "!!merge":
		return true
	default:
		return false
	}
}

func nodeValue(node *yaml.Node) any {
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return "?"
}

type validator struct {
	root  string
	appID string
}

func (v *validator) managedDir() string {
	return filepath.Join(v.root, v.appID)
}

func at(path, format string, args ...any) error {
	return fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...))
}

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
	if err := v.topVolumes(doc["volumes"]); err != nil {
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
		if err := v.service(name, service, declaredNetworks, names); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) topVolumes(raw any) error {
	if raw == nil {
		return nil
	}
	volumes, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("top-level volumes is not a mapping")
	}
	for name, definition := range volumes {
		if !namedVolumePattern.MatchString(name) {
			return fmt.Errorf("invalid volume name %q", name)
		}
		if err := v.reservedVolume(name); err != nil {
			return err
		}
		if definition == nil {
			continue
		}
		mapping, ok := definition.(map[string]any)
		if !ok {
			return at("volumes."+name, "volume definition is not a mapping")
		}
		for key := range mapping {
			switch key {
			case "driver", "driver_opts":
			default:
				return at("volumes."+name, "volume key %q is not allowed", key)
			}
		}
		if driver, ok := mapping["driver"]; ok && scalarString(driver) != "local" {
			return at("volumes."+name, "volume driver %q is not allowed: only the default local driver", scalarString(driver))
		}
		if opts, ok := mapping["driver_opts"]; ok {
			if err := v.managedVolumeOpts("volumes."+name, name, opts); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *validator) managedVolumeOpts(path, name string, raw any) error {
	opts, ok := raw.(map[string]any)
	if !ok {
		return at(path, "driver_opts must be a mapping")
	}
	for key, value := range opts {
		switch key {
		case "type":
			if scalarString(value) != "none" {
				return at(path, "driver_opts.type %q is not allowed", scalarString(value))
			}
		case "o":
			if scalarString(value) != "bind" {
				return at(path, "driver_opts.o %q is not allowed", scalarString(value))
			}
		case "device":
			device := filepath.Clean(scalarString(value))
			want := filepath.Join(v.managedDir(), name)
			if device != want {
				return at(path, "driver_opts.device %q must be %s", scalarString(value), want)
			}
			if err := checkNoSymlinks(v.managedDir(), device); err != nil {
				return at(path, "driver_opts.device %q: %v", scalarString(value), err)
			}
		default:
			return at(path, "driver_opts key %q is not allowed", key)
		}
	}
	for _, key := range []string{"type", "o", "device"} {
		if _, ok := opts[key]; !ok {
			return at(path, "driver_opts.%s is required", key)
		}
	}
	return nil
}

func (v *validator) reservedVolume(name string) error {
	if strings.HasPrefix(name, "gotham-db-") {
		return fmt.Errorf("volume name %q is reserved for managed databases", name)
	}
	return nil
}

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

var sweepLabels = []string{"gotham.app_id", "gotham.deployment_id"}

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
