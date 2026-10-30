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
  cleanup: {
    title: "Cleanup retention",
    history: "History retention (days)",
    historyHint:
      "Deployment logs, toolchain logs, request receipts and webhook events older than this become cleanup candidates. 7-3650 days.",
    backupAge: "Orphaned backup file age (days)",
    backupAgeHint:
      "A backup file no backups row references is offered as cleanup only after this many days. 1-365 days.",
    submit: "Save retention",
    saved: "Cleanup retention saved.",
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
  pendingConfirm: {
    title: "Pending control-plane URL change",
    body: "The control-plane URL is now {url}. Open Gotham at that address to keep the change.",
    hint:
      "Keep works only from the new address. If you cannot reach Gotham there, do nothing: the previous address is restored when the countdown ends.",
    countdownStart: "The control-plane URL change will revert in {time}.",
    keep: "Keep change",
    revert: "Revert now",
  },
  validation: {
    url: "Enter an http(s) URL with a host, without credentials, query or fragment.",
    name: "The name must be 1-64 characters.",
    cleanupHistory: "History retention must be a whole number of days between 7 and 3650.",
    cleanupBackupAge: "Backup file age must be a whole number of days between 1 and 365.",
  },
};

export default en;
