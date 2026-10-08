package composeguard

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// service validates one compose service against the allowlist. Only
// known-safe keys pass; anything else — namespaces, capabilities, devices,
// host configuration, secrets, build inputs — fails closed naming the
// offending path.
func (v *validator) service(name string, service map[string]any, volumes, networks map[string]bool, all []string) error {
	path := "services." + name
	image, ok := service["image"].(string)
	if !ok || strings.TrimSpace(image) == "" {
		return at(path, "service declares no image")
	}
	for key, value := range service {
		var err error
		switch key {
		case "image":
		case "command", "entrypoint":
			err = checkStringList(path+"."+key, value)
		case "environment":
			err = checkEnvironment(path+"."+key, value)
		case "ports":
			err = checkPorts(path+"."+key, value)
		case "expose":
			err = checkStringList(path+"."+key, value)
		case "volumes":
			err = v.serviceVolumes(path+"."+key, value, volumes)
		case "networks":
			err = checkLocalRefs(path+"."+key, value, networks, "network")
		case "depends_on":
			err = checkDependsOn(path+"."+key, value, all)
		case "restart":
			err = checkRestart(path+"."+key, value)
		case "healthcheck":
			err = checkHealthcheck(path+"."+key, value)
		case "labels":
			err = checkLabels(path+"."+key, value)
		case "working_dir", "user":
			err = checkNonEmptyString(path+"."+key, value)
		case "deploy":
			err = checkDeployLimits(path+"."+key, value)
		case "init", "tty", "stdin_open", "read_only":
			err = checkBool(path+"."+key, value)
		case "stop_grace_period":
			err = checkNonEmptyString(path+"."+key, value)
		case "logging":
			err = checkLogging(path+"."+key, value)
		case "tmpfs":
			err = checkStringList(path+"."+key, value)
		case "mem_limit":
			err = checkNonEmptyString(path+"."+key, value)
		case "cpus":
			err = checkScalar(path+"."+key, value)
		default:
			return at(path, "service key %q is not allowed for confined applications", key)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkNonEmptyString accepts a non-empty string value.
func checkNonEmptyString(path string, value any) error {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return at(path, "value must be a non-empty string")
	}
	return nil
}

// checkScalar accepts a plain scalar (string, number or boolean): the shapes
// command arguments, resource quantities and similar fields take.
func checkScalar(path string, value any) error {
	switch value.(type) {
	case string, bool, int, int64, float64:
		return nil
	default:
		return at(path, "value must be a scalar")
	}
}

// checkStringList accepts a string or a list of scalars.
func checkStringList(path string, value any) error {
	switch entries := value.(type) {
	case string:
		return nil
	case []any:
		for _, entry := range entries {
			if err := checkScalar(path, entry); err != nil {
				return err
			}
		}
		return nil
	default:
		return at(path, "value must be a string or a list of strings")
	}
}

// checkBool accepts a boolean (or its quoted spelling, which documents
// commonly use).
func checkBool(path string, value any) error {
	switch flag := value.(type) {
	case bool:
		return nil
	case string:
		if truthy, err := strconv.ParseBool(strings.TrimSpace(flag)); err == nil && (truthy || !truthy) {
			return nil
		}
		return at(path, "value must be a boolean")
	default:
		return at(path, "value must be a boolean")
	}
}

// checkEnvironment accepts a mapping of scalars or a list of KEY=VALUE
// strings. A mapping null or a list entry without `=` would resolve from
// the agent's process environment on the node, so both are rejected: every
// value the container sees must be written in the document (or substituted
// from the application environment before it is sent).
func checkEnvironment(path string, value any) error {
	switch entries := value.(type) {
	case map[string]any:
		for key, entry := range entries {
			if entry == nil {
				return at(path, "variable %q has no value: write it explicitly, null inherits the node environment", key)
			}
			if err := checkScalar(path+"."+key, entry); err != nil {
				return err
			}
		}
		return nil
	case []any:
		for _, entry := range entries {
			text, ok := entry.(string)
			if !ok || !strings.Contains(text, "=") {
				return at(path, "entry %q must be a KEY=VALUE string: a bare name inherits the node environment", scalarString(entry))
			}
		}
		return nil
	default:
		return at(path, "environment must be a mapping or a list of KEY=VALUE strings")
	}
}

// dockerAPIPorts are unprivileged but never legitimate for an application
// to publish: they are the Docker API itself.
var dockerAPIPorts = []int{2375, 2376}

// checkPorts accepts the short string syntax and the long mapping syntax
// with its standard keys, and rejects a published host port inside the
// reserved set: the privileged band 1-1023 and the Docker API ports. The
// band matches the application's own HostPort rule, so a document cannot
// bypass it by writing the port directly. Ephemeral mappings (no published
// part) and high ports pass, like ordinary applications; unparseable
// numbers and live references pass here and fail closed later (the node
// refuses them, and the rendered re-check judges resolved values).
func checkPorts(path string, value any) error {
	entries, ok := value.([]any)
	if !ok {
		return at(path, "ports must be a list")
	}
	for _, entry := range entries {
		switch mapping := entry.(type) {
		case string:
			if strings.TrimSpace(mapping) == "" {
				return at(path, "port entry must not be empty")
			}
			if err := checkShortPublished(path, mapping); err != nil {
				return err
			}
		case map[string]any:
			for key := range mapping {
				switch key {
				case "target", "published", "host_ip", "protocol", "mode", "app_protocol":
				default:
					return at(path, "port key %q is not allowed", key)
				}
			}
			if strings.TrimSpace(scalarString(mapping["target"])) == "" {
				return at(path, "long port entry needs a target")
			}
			if published, ok := mapping["published"]; ok && published != nil {
				if err := checkPublishedBand(path, scalarString(published)); err != nil {
					return err
				}
			}
		default:
			return at(path, "port entries must be strings or mappings")
		}
	}
	return nil
}

// checkShortPublished rejects a short-syntax entry whose published host
// port falls in the reserved set. The container-side ranges and the
// protocol suffix never decide: only the host binding squats node ports.
func checkShortPublished(path, entry string) error {
	target := entry
	if index := strings.LastIndex(target, "/"); index >= 0 {
		target = target[:index]
	}
	rest := strings.TrimSpace(target)
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return at(path, "port entry %q is not valid", entry)
		}
		rest = strings.TrimSpace(strings.TrimPrefix(rest[end+1:], ":"))
		if rest == "" {
			return at(path, "port entry %q is not valid", entry)
		}
	}
	parts := strings.Split(rest, ":")
	switch len(parts) {
	case 1:
		return nil // Bare container port (or range): ephemeral host port.
	case 2:
		if net.ParseIP(strings.TrimSpace(parts[0])) != nil {
			return nil // host_ip with an ephemeral host port.
		}
		return checkPublishedBand(path, parts[0])
	case 3:
		// [ip:]host:container. The address never matters for the band:
		// even a loopback binding consumes the host port.
		return checkPublishedBand(path, parts[1])
	default:
		return at(path, "port entry %q has too many segments", entry)
	}
}

// checkPublishedBand rejects one published host-port value inside the
// reserved set: 1-1023 (privileged, including 22/80/443) and the Docker
// API ports 2375/2376. Ranges are rejected on any reserved member. Values
// that are not plain numbers (live references, resolved after render) pass
// here; the rendered re-check judges them resolved.
func checkPublishedBand(path, published string) error {
	value := strings.TrimSpace(published)
	if value == "" {
		return nil
	}
	if strings.Contains(value, "$") {
		return nil
	}
	if strings.Contains(value, "-") {
		bounds := strings.SplitN(value, "-", 2)
		lo, loErr := strconv.Atoi(strings.TrimSpace(bounds[0]))
		hi, hiErr := strconv.Atoi(strings.TrimSpace(bounds[1]))
		if loErr != nil || hiErr != nil || len(bounds) != 2 {
			return nil
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		for _, reserved := range bandMembers(lo, hi) {
			return at(path, "published host port %q is reserved (%s)", published, reserved)
		}
		return nil
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	if reason := reservedReason(port); reason != "" {
		return at(path, "published host port %d is reserved (%s)", port, reason)
	}
	return nil
}

// bandMembers names the reserved members of a published range, if any.
func bandMembers(lo, hi int) []string {
	var members []string
	for port := lo; port <= hi; port++ {
		if reason := reservedReason(port); reason != "" {
			members = append(members, fmt.Sprintf("%d %s", port, reason))
		}
		if len(members) >= 3 {
			members = append(members, "...")
			break
		}
	}
	return members
}

// reservedReason names why a published host port is reserved, or "" when
// the application may publish it.
func reservedReason(port int) string {
	if port >= 1 && port <= 1023 {
		return "privileged band"
	}
	for _, reserved := range dockerAPIPorts {
		if port == reserved {
			return "Docker API"
		}
	}
	return ""
}

// checkLocalRefs accepts a list of names that must each be declared in the
// referenced top-level section (service networks). The mapping form (with
// aliases and conditions) is rejected: the default attachment is what a
// confined project needs.
func checkLocalRefs(path string, value any, declared map[string]bool, what string) error {
	entries, ok := value.([]any)
	if !ok {
		return at(path, "%s references must be a list of declared %ss", what, what)
	}
	for _, entry := range entries {
		name, ok := entry.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return at(path, "%s references must be a list of declared %ss", what, what)
		}
		if !declared[name] {
			return at(path, "%s %q is not a declared top-level %s", what, name, what)
		}
	}
	return nil
}

// checkDependsOn accepts a list of declared service names. The long form
// (conditions, restarts) is rejected: startup order beyond plain
// dependencies is not needed for a confined project.
func checkDependsOn(path string, value any, all []string) error {
	entries, ok := value.([]any)
	if !ok {
		return at(path, "depends_on must be a list of service names")
	}
	for _, entry := range entries {
		name, ok := entry.(string)
		if !ok || !composeServiceNamePattern.MatchString(strings.TrimSpace(name)) {
			return at(path, "depends_on entries must be service names")
		}
		known := false
		for _, candidate := range all {
			if candidate == strings.TrimSpace(name) {
				known = true
				break
			}
		}
		if !known {
			return at(path, "depends_on service %q is not declared", strings.TrimSpace(name))
		}
	}
	return nil
}

// checkRestart accepts the documented restart policies.
func checkRestart(path string, value any) error {
	policy, ok := value.(string)
	if !ok {
		return at(path, "restart must be a policy string")
	}
	switch trimmed := strings.TrimSpace(policy); {
	case trimmed == "no" || trimmed == "always" || trimmed == "unless-stopped":
		return nil
	case trimmed == "on-failure" || strings.HasPrefix(trimmed, "on-failure:"):
		if rest, found := strings.CutPrefix(trimmed, "on-failure:"); found {
			if _, err := strconv.Atoi(strings.TrimSpace(rest)); err != nil {
				return at(path, "restart policy %q is not allowed", policy)
			}
		}
		return nil
	default:
		return at(path, "restart policy %q is not allowed", policy)
	}
}

// checkHealthcheck accepts the standard probe fields.
func checkHealthcheck(path string, value any) error {
	probe, ok := value.(map[string]any)
	if !ok {
		return at(path, "healthcheck must be a mapping")
	}
	for key, entry := range probe {
		var err error
		switch key {
		case "test":
			err = checkStringList(path+"."+key, entry)
		case "interval", "timeout", "start_period", "start_interval":
			err = checkNonEmptyString(path+"."+key, entry)
		case "retries":
			err = checkScalar(path+"."+key, entry)
		case "disable":
			err = checkBool(path+"."+key, entry)
		default:
			return at(path, "healthcheck key %q is not allowed", key)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkLabels accepts a mapping of scalars or a list of KEY=VALUE strings.
// Any gotham.-prefixed key is rejected: those labels steer Gotham's own
// container sweeps and proxy, so a document must never set them.
func checkLabels(path string, value any) error {
	switch entries := value.(type) {
	case map[string]any:
		for key, entry := range entries {
			if strings.HasPrefix(key, "gotham.") {
				return at(path, "label %q is reserved for Gotham", key)
			}
			// A null label value means empty, which compose accepts.
			if entry == nil {
				continue
			}
			if err := checkScalar(path+"."+key, entry); err != nil {
				return err
			}
		}
		return nil
	case []any:
		for _, entry := range entries {
			text, ok := entry.(string)
			if !ok || !strings.Contains(text, "=") {
				return at(path, "label list entries must be KEY=VALUE strings")
			}
			if key, _, _ := strings.Cut(text, "="); strings.HasPrefix(strings.TrimSpace(key), "gotham.") {
				return at(path, "label %q is reserved for Gotham", strings.TrimSpace(key))
			}
		}
		return nil
	default:
		return at(path, "labels must be a mapping or a list of KEY=VALUE strings")
	}
}

// checkDeployLimits accepts resource limits and reservations only: the
// replica count, placement and restart policy of swarm mode would break the
// single-web-container release model, so the deploy section carries nothing
// else.
func checkDeployLimits(path string, value any) error {
	deploy, ok := value.(map[string]any)
	if !ok {
		return at(path, "deploy must be a mapping")
	}
	for key := range deploy {
		if key != "resources" {
			return at(path, "deploy key %q is not allowed: only resource limits", key)
		}
	}
	resources, ok := deploy["resources"].(map[string]any)
	if !ok {
		return at(path, "deploy.resources must be a mapping")
	}
	for key, entry := range resources {
		switch key {
		case "limits", "reservations":
		default:
			return at(path, "deploy.resources key %q is not allowed", key)
		}
		quotas, ok := entry.(map[string]any)
		if !ok {
			return at(path, "deploy.resources.%s must be a mapping", key)
		}
		for quota, amount := range quotas {
			switch quota {
			case "cpus", "memory":
			default:
				return at(path, "deploy.resources.%s key %q is not allowed", key, quota)
			}
			switch amount.(type) {
			case string, int, int64, float64:
			default:
				return at(path, "deploy.resources.%s.%s must be a quantity", key, quota)
			}
		}
	}
	return nil
}

// checkLogging accepts the default log drivers only: anything else would
// ship container logs off the node.
func checkLogging(path string, value any) error {
	logging, ok := value.(map[string]any)
	if !ok {
		return at(path, "logging must be a mapping")
	}
	for key, entry := range logging {
		switch key {
		case "driver":
			switch driver := strings.TrimSpace(scalarString(entry)); driver {
			case "json-file", "local":
			default:
				return at(path, "logging driver %q is not allowed", driver)
			}
		case "options":
			options, ok := entry.(map[string]any)
			if !ok {
				return at(path, "logging.options must be a mapping")
			}
			for option, optionValue := range options {
				if err := checkScalar(path+".options."+option, optionValue); err != nil {
					return err
				}
			}
		default:
			return at(path, "logging key %q is not allowed", key)
		}
	}
	return nil
}
