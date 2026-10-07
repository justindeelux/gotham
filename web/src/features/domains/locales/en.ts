/**
 * English catalog for the domains feature: page shell, KPI tiles, DNS
 * provider controls, certificate controls, router stub and redirect rules.
 * Wire values (provider types, challenge modes, redirect codes, domains,
 * URLs, Traefik middleware names) are interpolated as parameters or kept in
 * code; only the framing copy lives here.
 */
const en = {
  page: {
    eyebrow: "Operations · Reverse proxy",
    title: "Domains & SSL",
    descriptionPre: "Traefik 3.1 runs on every node as the container",
    descriptionPost:
      "The control plane generates the dynamic configuration from application " +
      "state through the file provider — testable, idempotent, and keeping one " +
      "previous version for a quick rollback. Certificates below are intent " +
      "records; the node performs ACME issuance.",
    addProvider: "Add DNS provider",
    addCertificate: "Add certificate",
  },
  tabs: {
    routers: "Routers",
    certificates: "Certificates",
    dns: "DNS providers",
    redirects: "Redirects",
  },
  stats: {
    certConfigs: "Certificate configs",
    certConfigsSub: "{count} enabled · one per application",
    wildcardConfigs: "Wildcard configs",
    wildcardSub: "dns-01 challenge required",
    dnsProviders: "DNS providers",
    dnsProvidersSub: "{count} enabled · credential sealed",
    appsWithDomain: "Applications with a domain",
    appsWithDomainSub: "edited on each application page",
  },
  providers: {
    title: "DNS providers",
    add: "Add provider",
    sealedNote:
      "Credentials are sealed server-side and never returned by the API. " +
      "The UI can set or rotate a credential, but cannot display or copy it.",
    zones: "Zones",
    credential: "Credential",
    credentialSet: "set",
    credentialUnset: "not set",
    credentialNeverReturned: "· the API never returns the token",
    updated: "Updated",
    enableLabel: "Enable {name}",
    editRotate: "Edit & rotate credential",
    delete: "Delete",
    deleteConfirm:
      "Delete the {provider} provider {name}? Providers referenced by a " +
      "certificate configuration cannot be deleted.",
    empty: "No DNS providers configured.",
    emptyHint:
      "DNS-01 challenges need a provider credential. Wildcard certificates " +
      "require the DNS-01 challenge.",
    dns01Title: "DNS-01 requires the zone to be delegated to the provider",
    dns01BodyPre: "Let's Encrypt validates through a TXT record",
    dns01BodyPost:
      "created by the provider. If the zone's nameservers do not point at " +
      "Cloudflare or DigitalOcean, the order fails with NXDOMAIN. The control " +
      "plane stores the configured zones; it does not check delegation for you.",
    enabled: "enabled",
    disabled: "disabled",
    unknownProvider: "unknown provider",
    saved: "DNS provider saved.",
    created: "DNS provider created.",
    enabledToast: "Provider enabled.",
    disabledToast: "Provider disabled.",
    deleted: "DNS provider deleted.",
  },
  providerDialog: {
    addTitle: "Add DNS provider",
    editTitle: "Edit DNS provider",
    provider: "Provider",
    providerAria: "Provider type",
    name: "Name",
    nameAria: "Provider name",
    namePlaceholder: "Optional label, e.g. Production Cloudflare",
    zones: "Zones",
    zonesAria: "DNS zones",
    zonesPlaceholder: "Type a zone and press Enter",
    credential: "Credential",
    rotateCredential: "Rotate credential (optional)",
    credentialAria: "Provider credential",
    credentialPlaceholder: "API token",
    keepCredentialPlaceholder: "Leave blank to keep the stored credential",
    enabled: "Enabled",
    enabledAria: "Provider enabled",
    sealedNote:
      "The credential is sent once over the API and sealed server-side; it " +
      "is never displayed, logged or stored in the browser again.",
    cancel: "Cancel",
    save: "Save",
  },
  certificates: {
    title: "Certificate configurations",
    onePerApp: "one per application",
    add: "Add certificate",
    domain: "Domain",
    challenge: "Challenge",
    dnsProvider: "DNS provider",
    wildcard: "Wildcard",
    wildcardTag: "wildcard",
    no: "no",
    enabled: "Enabled",
    enabledTag: "enabled",
    disabledTag: "disabled",
    status: "Status",
    expires: "Expires",
    updated: "Updated",
    actions: "Actions",
    edit: "Edit",
    delete: "Delete",
    deleteConfirm:
      "Delete the certificate configuration for {domain}? The route falls " +
      "back to plain HTTP.",
    empty: "No certificate configurations yet.",
    statusPresent: "present",
    statusAbsent: "no certificate",
    statusUnknown: "unknown",
    statusNotReported: "not reported",
    expiryNotReported: "not reported",
    baseDomainChanged:
      "{app} · base domain changed to {domain} — re-save to re-record",
    statusTitle: "Status is observed live from the owning node",
    statusBody:
      "present means the node's ACME storage holds a certificate for the " +
      "recorded domain and its expiry is shown; absent means the storage was " +
      "read and holds none; unknown means the node could not be read. The " +
      "status is computed on read — the control plane stores the desired " +
      "configuration only.",
    footer:
      "The recorded domain comes from the application's base domain at save " +
      "time. Changing the base domain later requires saving the configuration " +
      "again to re-record it.",
    saved: "Certificate configuration saved.",
    created: "Certificate configuration created.",
    deleted: "Certificate configuration deleted.",
  },
  certificateDialog: {
    addTitle: "Add certificate",
    editTitle: "Edit certificate configuration",
    note: "The recorded domain always comes from the selected application's base domain — it is not editable here.",
    cancel: "Cancel",
    save: "Save",
  },
  certificateForm: {
    application: "Application",
    applicationAria: "Application",
    selectApplication: "Select an application",
    domainFromApp: "Domain (from the application)",
    noBaseDomain: "No base domain yet — set one on the application first.",
    appWithoutDomain: "{name} · no base domain",
    challenge: "Challenge",
    httpHint: "· shared HTTP resolver",
    dnsHint: "· TXT record via a DNS provider",
    dnsProvider: "DNS provider",
    dnsProviderAria: "DNS provider",
    selectProvider: "Select a DNS provider",
    providerDisabled: "{label} · {provider} (disabled)",
    httpNoProvider: "The shared HTTP-01 resolver needs no provider.",
    wildcard: "Wildcard",
    wildcardAria: "Wildcard certificate",
    wildcardRequested: "requested",
    wildcardRequires: "Wildcards require the DNS-01 challenge.",
    enabled: "Enabled",
    enabledAria: "Certificate configuration enabled",
  },
  routers: {
    title: "Router list — backend pending",
    empty: "The generated Traefik routers are not exposed by the API yet.",
    hint: "The control plane generates the file-provider configuration from application state (BE-6.1), but there is no read endpoint for the resulting routers. A live router table arrives in a later backend package; nothing is shown here rather than invented.",
    manageDomains: "Manage application domains",
  },
  redirects: {
    rulesTitle: "Redirect rules",
    middlewareTag: "middleware redirectregex",
    addTitle: "Add redirect",
    appliesAfter: "applies after the next config sync",
    application: "Application",
    applicationAria: "Redirect application",
    selectApplication: "Select an application",
    appWithoutDomain: "{name} · no base domain",
    sourceDomain: "Source domain",
    sourceAria: "Redirect source domain",
    targetDomain: "Target domain",
    targetAria: "Redirect target domain",
    redirectCode: "Redirect code",
    codeAria: "Redirect code",
    codePermanent: "permanent",
    codeTemporary: "temporary",
    codeOption: "{code} · {kind}",
    preservePath: "Preserve path",
    preserveAria: "Redirect preserve path",
    enabled: "Enabled",
    enabledNowAria: "Redirect enabled now",
    addRedirect: "Add redirect",
    ruleHint:
      "Source and target must differ. The code applies to GET; HEAD and " +
      "every other method answer 308/307, keeping the method.",
    editTitle: "Edit redirect rule",
    owningApp: "Application:",
    cannotMove: "— the owning application cannot be moved after creation.",
    editSourceAria: "Edit redirect source domain",
    editTargetAria: "Edit redirect target domain",
    editCodeAria: "Edit redirect code",
    editPreserveAria: "Edit redirect preserve path",
    editEnabledAria: "Edit redirect enabled",
    enableRedirectAria: "Enable redirect {source}",
    cancel: "Cancel",
    save: "Save",
    paused: "paused",
    keepsPath: "· keeps the path",
    edit: "Edit",
    delete: "Delete",
    deleteConfirm:
      "Delete the redirect {source} → {target}? Requests to the source " +
      "stop redirecting.",
    empty: "No redirect rules yet.",
    emptyHint:
      "A rule sends one exact source host to one exact target host. The " +
      "target must serve its own certificate.",
    /**
     * Rule count with library pluralization (one | other): the numeric total
     * selects the segment; `{total}`/`{enabled}` interpolate raw counts.
     */
    rulesSummary:
      "{total} rule · {enabled} enabled. GET answers the stored code; " +
      "other methods answer 308/307 so they keep their method. | " +
      "{total} rules · {enabled} enabled. GET answers the stored code; " +
      "other methods answer 308/307 so they keep their method.",
    footer:
      "Rules are applied by Traefik's redirectRegex middleware on the next " +
      "dynamic configuration sync — no redeploy is needed.",
    created: "Redirect rule created.",
    saved: "Redirect rule saved.",
    enabledToast: "Redirect rule enabled.",
    pausedToast: "Redirect rule paused.",
    deleted: "Redirect rule deleted.",
  },
  errors: {
    sessionExpired: "Your session expired. Please sign in again.",
    adminScope:
      "You need the admin scope to manage domains and SSL. Sign in with " +
      "an admin account or use an admin API token.",
    invalidRequest: "Invalid request. Check the highlighted fields and retry.",
    notFound: "Not found. It may have been deleted already.",
    conflict: "The change conflicts with existing state.",
    nodeUnreachable:
      "The node agent is unreachable. Check the node status and retry.",
    secretNotConfigured:
      "The deployment secret is not configured, so credentials cannot be " +
      "stored (set GOTHAM_SECRET_KEY).",
    requestFailed: "Request failed",
    unexpected: "Something went wrong. Please try again.",
  },
};

export default en;
