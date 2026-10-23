package composeguard

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func (v *validator) service(name string, service map[string]any, networks map[string]bool, all []string) error {
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
			err = v.serviceVolumes(path+"."+key, value)
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

func checkNonEmptyString(path string, value any) error {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return at(path, "value must be a non-empty string")
	}
	return nil
}

func checkScalar(path string, value any) error {
	switch value.(type) {
	case string, bool, int, int64, float64:
		return nil
	default:
		return at(path, "value must be a scalar")
	}
}

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

var dockerAPIPorts = []int{2375, 2376}

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
		return nil
	case 2:
		if net.ParseIP(strings.TrimSpace(parts[0])) != nil {
			return nil
		}
		return checkPublishedBand(path, parts[0])
	case 3:
		return checkPublishedBand(path, parts[1])
	default:
		return at(path, "port entry %q has too many segments", entry)
	}
}

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

func checkLabels(path string, value any) error {
	switch entries := value.(type) {
	case map[string]any:
		for key, entry := range entries {
			if strings.HasPrefix(key, "gotham.") {
				return at(path, "label %q is reserved for Gotham", key)
			}
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
