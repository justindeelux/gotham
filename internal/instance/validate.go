package instance

import (
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var hostLabel = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// normalizeGeneral validates and normalizes the general input.
func normalizeGeneral(in GeneralInput) (GeneralInput, FieldErrors) {
	errs := FieldErrors{}
	in.ControlPlaneURL = strings.TrimRight(strings.TrimSpace(in.ControlPlaneURL), "/")
	in.InstanceName = strings.TrimSpace(in.InstanceName)
	in.Timezone = strings.TrimSpace(in.Timezone)

	if in.ControlPlaneURL != "" {
		if msg := checkURL(in.ControlPlaneURL); msg != "" {
			errs["control_plane_url"] = msg
		}
	}
	if n := len([]rune(in.InstanceName)); n < 1 || n > 64 {
		errs["instance_name"] = "must be 1-64 characters"
	} else if strings.IndexFunc(in.InstanceName, unicode.IsControl) >= 0 {
		errs["instance_name"] = "must not contain control characters"
	}
	if msg := checkTimezone(in.Timezone); msg != "" {
		errs["timezone"] = msg
	}
	return in, nonEmpty(errs)
}

func checkURL(raw string) string {
	if len(raw) > 255 {
		return "must be at most 255 characters"
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "must be an http(s) URL with a host, e.g. https://gotham.example.com"
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "must not contain credentials, a query or a fragment"
	}
	return ""
}

func checkTimezone(tz string) string {
	if tz == "" || len(tz) > 64 || tz == "Local" {
		return "must be an IANA timezone such as Europe/Berlin"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "unknown timezone; use an IANA name such as Europe/Berlin"
	}
	return ""
}

// normalizeNetwork validates the network section.
func normalizeNetwork(n Network) (Network, FieldErrors) {
	errs := FieldErrors{}
	n.DNSServers = trimAll(n.DNSServers)
	if len(n.DNSServers) > 3 {
		errs["dns_servers"] = "at most 3 servers"
	}
	seen := map[netip.Addr]bool{}
	for _, raw := range n.DNSServers {
		addr, err := netip.ParseAddr(raw)
		switch {
		case err != nil || addr.Zone() != "":
			errs["dns_servers"] = "not a valid IP address: " + clip(raw)
		case addr.IsUnspecified() || addr.IsMulticast():
			errs["dns_servers"] = "not a usable resolver address: " + raw
		case seen[addr]:
			errs["dns_servers"] = "duplicate server: " + raw
		}
		seen[addr] = true
	}

	checkFamily(errs, "ipv4", n.IPv4.Mode, n.IPv4.Address, n.IPv4.Gateway, true, true)
	checkFamily(errs, "ipv6", n.IPv6.Mode, n.IPv6.Address, n.IPv6.Gateway, false, n.IPv6.Enabled)
	return n, nonEmpty(errs)
}

// checkFamily validates one address family; v4 selects the family.
func checkFamily(errs FieldErrors, key, mode, address, gateway string, v4, enabled bool) {
	if mode != ModeDHCP && mode != ModeStatic {
		errs[key+".mode"] = "must be dhcp or static"
		return
	}
	if mode == ModeDHCP || !enabled {
		if address != "" {
			errs[key+".address"] = "must be empty unless the mode is static"
		}
		if gateway != "" {
			errs[key+".gateway"] = "must be empty unless the mode is static"
		}
		if !enabled && mode == ModeStatic {
			errs[key+".mode"] = "must be dhcp while the family is disabled"
		}
		return
	}
	prefix, err := netip.ParsePrefix(address)
	if err != nil || prefix.Addr().Zone() != "" || prefix.Addr().Is4() != v4 || prefix.Addr().Is4In6() {
		errs[key+".address"] = "must be an address in CIDR notation, e.g. " + example(v4)
		return
	}
	if prefix.Bits() < 1 || prefix.Addr().IsUnspecified() || prefix.Addr().IsMulticast() || prefix.Addr().IsLoopback() {
		errs[key+".address"] = "not a usable host address"
		return
	}
	gw, err := netip.ParseAddr(gateway)
	switch {
	case err != nil || gw.Zone() != "" || gw.Is4() != v4 || gw.Is4In6():
		errs[key+".gateway"] = "must be a valid gateway address"
	case gw == prefix.Addr():
		errs[key+".gateway"] = "must differ from the host address"
	case !prefix.Contains(gw) && (gw.Is4() || !gw.IsLinkLocalUnicast()):
		errs[key+".gateway"] = "must be inside the configured subnet"
	}
}

func example(v4 bool) string {
	if v4 {
		return "192.168.1.10/24"
	}
	return "2001:db8::10/64"
}

// normalizeSystem validates the system section.
func normalizeSystem(s System) (System, FieldErrors) {
	errs := FieldErrors{}
	s.Hostname = strings.TrimSpace(s.Hostname)
	if s.Hostname != "" && !validHostname(s.Hostname) {
		errs["hostname"] = "must be a valid host name (letters, digits and hyphens, dot separated)"
	}
	s.NTPServers = trimAll(s.NTPServers)
	if len(s.NTPServers) > 4 {
		errs["ntp_servers"] = "at most 4 servers"
	}
	seen := map[string]bool{}
	for _, srv := range s.NTPServers {
		if _, err := netip.ParseAddr(srv); err != nil && !validHostname(srv) {
			errs["ntp_servers"] = "not a valid host name or IP address: " + clip(srv)
		}
		if seen[strings.ToLower(srv)] {
			errs["ntp_servers"] = "duplicate server: " + srv
		}
		seen[strings.ToLower(srv)] = true
	}
	return s, nonEmpty(errs)
}

func validHostname(name string) bool {
	if len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func clip(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

func nonEmpty(errs FieldErrors) FieldErrors {
	if len(errs) == 0 {
		return nil
	}
	return errs
}
