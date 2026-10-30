const en = {
  page: {
    eyebrow: "System · Owner",
    title: "Settings",
    description:
      "Configure the Gotham instance itself: its public control-plane address and name. Visible to the owner only.",
  },
  forbidden: "Only the owner can view or change these settings.",
  locked: "Locked by the environment variable {name}.",
  general: {
    title: "Instance",
    url: "Control-plane URL",
    urlHint: "Public base URL used for agent enrollment, OAuth redirects and webhooks.",
    name: "Instance name",
    submit: "Save settings",
    saved: "Settings saved.",
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
  validation: {
    url: "Enter an http(s) URL with a host, without credentials, query or fragment.",
    name: "The name must be 1-64 characters.",
  },
};

export default en;
