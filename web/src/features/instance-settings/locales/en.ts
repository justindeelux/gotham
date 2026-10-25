const en = {
  page: {
    eyebrow: "Settings · Owner",
    title: "Instance",
    description:
      "Configure the Gotham instance itself: its public address, name and timezone, the host network and a few Linux system options. Visible to owner only.",
  },
  tabs: { general: "General", network: "Network", system: "System" },
  forbidden: "Only owner can view or change instance settings.",
  locked: "Locked by the environment variable {name}.",
  helper: {
    missing:
      "The host helper is not installed, so these values are read-only. Install it to manage host settings from Gotham.",
    restart:
      "Host changes are provisioned by the control-plane installer, so a ask the owner to re-run it (or upgrade) to enable them.",
    install: "Install host helper",
    installed: "Host helper installed. These settings are now editable.",
    installFailed: "The host helper could not be installed.",
    logTitleFailed: "Host helper install failed",
    logHint: "Output from the installer on this host.",
    close: "Close",
  },
  general: {
    title: "General",
    url: "Control-plane URL",
    urlHint: "Public base URL used for agent enrollment, OAuth redirects and webhooks.",
    name: "Instance name",
    timezone: "Timezone",
    timezoneHint:
      "Searchable list of IANA zones with the current UTC offset. Applied to schedules, log display and backups. Disabled when locked by the environment.",
    submit: "Save general settings",
    saved: "General settings saved.",
  },
  domainStatus: {
    title: "Control-plane domain status",
    empty: "Set a control-plane URL to see its router status.",
    invalid: "Enter a valid control-plane URL to check its status.",
    dnsUnknown: "DNS: not reported",
    certUnknown: "Certificate: not reported",
    certState: {
      issued: "Certificate: issued",
      pending: "Certificate: pending",
      failed: "Certificate: failed",
    },
    certExpiry: "Expires {date}",
    certNotApplicable: "Certificate: not applicable (HTTP)",
    observationHint:
      "The routers API does not report DNS resolution.",
    loading: "Router: checking…",
    synced: "Router: synced",
    pending: "Router: sync pending",
    unknown: "Router: unknown",
    missingLocalhost:
      "The localhost node is not registered. " +
      "Register it before syncing the control-plane router.",
    readFailed: "Could not read the localhost router status. Check the node and retry.",
    unsaved: "Save the URL before its router can be generated and synced.",
    refresh: "Refresh status",
  },
  network: {
    title: "Network",
    dnsPrimary: "Primary DNS server",
    dnsAlternate: "Alternate DNS server",
    dnsExtra: "DNS server {n}",
    dnsAdd: "Add DNS server",
    dnsRemove: "Remove",
    dnsHint: "Primary required, alternate optional. Up to 3 resolvers.",
    optional: "optional",
    mode: "Mode",
    dhcp: "DHCP",
    auto: "Automatic",
    static: "Static",
    address: "Address (CIDR)",
    gateway: "Gateway",
    ipv6Enabled: "Enable IPv6",
    submit: "Apply network changes",
    applied: "Network changes applied. Confirm them before the countdown ends.",
    pendingNote: "A network change is awaiting confirmation. Settle it before making another.",
    dnsOnlyNote: "Only the DNS servers changed: the interface configuration is left untouched.",
    riskyWarning:
      "This changes the address of the interface you are connected through. The host may become unreachable until the change is confirmed or reverted.",
    confirmLabel: "I understand this may interrupt the connection",
    confirmRequired: "Tick the confirmation box to apply this change.",
    stackUnsupported:
      "This host does not run systemd-networkd and systemd-resolved, so Gotham cannot manage its network. Values are shown read-only.",
  },
  confirm: {
    title: "Keep these network changes?",
    body: "The new configuration is live. If you cannot reach Gotham at the new address, do nothing: the host restores the previous configuration in",
    keep: "Keep changes",
    revert: "Revert now",
  },
  system: {
    title: "System",
    hostname: "Hostname",
    ntpEnabled: "Synchronize time (NTP)",
    ntpServers: "NTP servers",
    ntpHint: "Up to 4, comma separated. Leave empty for the system defaults.",
    submit: "Save system settings",
    saved: "System settings saved.",
    stackUnsupported:
      "This host lacks the tools Gotham uses for system settings. Values are shown read-only.",
  },
  validation: {
    url: "Enter an http(s) URL with a host, without credentials, query or fragment.",
    name: "The name must be 1-64 characters.",
    timezone: "Select an IANA timezone such as Europe/Berlin.",
    dns: "Enter up to 3 distinct IPv4 or IPv6 addresses.",
    dnsSingle: "Enter a valid IPv4 or IPv6 address.",
    ipv4Address: "Enter an IPv4 address in CIDR notation, e.g. 192.168.1.10/24.",
    ipv4Gateway: "Enter a valid IPv4 gateway.",
    ipv6Address: "Enter an IPv6 address in CIDR notation, e.g. 2001:db8::10/64.",
    ipv6Gateway: "Enter a valid IPv6 gateway.",
    hostname: "Use letters, digits and hyphens; dots separate labels.",
    ntp: "Enter up to 4 distinct host names or IP addresses.",
  },
};

export default en;
