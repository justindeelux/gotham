const en = {
  page: {
    eyebrow: "Access · Notifications",
    title: "Notification channels",
    description:
      "Deploy and backup events are delivered to your channels: " +
      "Discord and Slack webhooks, a Telegram bot, or SMTP email. Channels " +
      "are resource-scoped, and secrets are write-only — reads only ever show " +
      "a masked value.",
    newChannel: "New channel",
    unavailableTitle: "Notifications unavailable",
    unavailableDesc:
      "Notification channels are turned off on this control plane. " +
      "Ask the owner to enable them.",
    empty: "No channels yet.",
    createFirst: "Create the first channel",
  },
  inbox: {
    title: "Notifications",
    bell: "Notifications",
    bellUnread: "Notifications, {count} unread",
    unreadCount: "{count} unread",
    empty: "No notifications yet.",
    unavailable: "This item is no longer available.",
    events: {
      deploy_success: "{name} deployed to {host}",
      deploy_failure: "{name} failed to deploy on {host}",
      backup_success: "Backup of {name} on {host} succeeded",
      backup_failure: "Backup of {name} on {host} failed",
      server_offline: "Server {name} is offline",
      server_online: "Server {name} is back online",
      certificate_expiring: "Certificate for {name} expires soon",
      update_available: "Gotham {name} is available",
      redeploy_needed: "{name} needs a redeploy: {changes}",
      runtime_failure: "Runtime failure: {name}",
      runtime_recovery: "Runtime recovered: {name}",
      incident_opened: "Incident opened: {name}",
      incident_severity_changed: "Incident severity changed: {name}",
      incident_recovered: "Incident recovered: {name}",
      unknown: "Update for {name} on {host}",
    },
    changes: {
      domains: "domains or ports",
      environment: "environment variables",
      secrets: "secrets",
      storage: "storage",
    },
  },
  card: {
    enabledLabel: "Enabled",
    enableAria: "Enable {name}",
    enabled: "enabled",
    disabled: "disabled",
    secretConfigured: "secret configured",
    sendTest: "Send test",
    testDelivered: "Test delivered: ",
    testFailed: "Test failed: ",
    noTest: "No test sent yet.",
    edit: "Edit",
    delete: "Delete",
    deleteConfirm:
      'Delete channel "{name}"? Deploy and backup notifications stop ' +
      "immediately.",
    updated: "Updated",
  },
  history: {
    show: "Delivery history",
    hide: "Hide delivery history",
    empty: "No delivery attempts yet.",
    testEvent: "Test message",
    delivered: "Delivered",
    failed: "Failed",
    pending: "Pending",
    retrying: "Retrying",
    sending: "Sending",
    cancelled: "Cancelled",
    retry: "Retry",
    retryQueued: "Retry queued",
    attempts: "{count} attempts",
    nextRetry: "Next retry {time}",
  },
  health: {
    ok: "Delivering",
    failing: "Cannot deliver",
    pending: "Retrying",
    unknown: "No attempts yet",
  },
  attention: {
    title: "Notification channels cannot deliver",
    hint: "The panel is the fallback while a channel is broken.",
    failed: "Delivery failed",
    retry: "Retry",
    retryQueued: "Retry queued",
  },
  kinds: {
    discord: "Discord webhook",
    slack: "Slack incoming webhook",
    telegram: "Telegram bot",
    email: "Email (SMTP)",
  },
  events: {
    deploy_success: "Deploy succeeded",
    deploy_failure: "Deploy failed",
    backup_success: "Backup succeeded",
    backup_failure: "Backup failed",
    server_offline: "Server offline",
    server_online: "Server back online",
    certificate_expiring: "Certificate expiring",
    update_available: "Gotham update available",
    redeploy_needed: "Redeploy needed",
    runtime_failure: "Runtime failure",
    runtime_recovery: "Runtime recovered",
    incident_opened: "Incident opened",
    incident_severity_changed: "Incident severity changed",
    incident_recovered: "Incident recovered",
  },
  scopeLabel: {
    allResources: "All resources",
    app: "App: {name}",
    database: "Database: {name}",
  },
  configSummary: {
    webhookMissing: "webhook not configured",
    webhook: "webhook {value}",
    chat: "chat {id}",
    chatMissing: "—",
  },
  dialog: {
    newTitle: "New notification channel",
    editTitle: "Edit notification channel",
    name: "Name",
    namePlaceholder: "e.g. Deploy alerts",
    nameAria: "Channel name",
    kind: "Kind",
    kindAria: "Channel kind",
    webhookUrl: "Webhook URL",
    webhookAria: "Webhook URL",
    webhookPlaceholder: "https://discord.com/api/webhooks/…",
    webhookKeepPlaceholder:
      "Leave the masked value to keep the stored URL",
    botToken: "Bot token",
    botTokenAria: "Bot token",
    botTokenPlaceholder: "123456:ABC-…",
    botTokenKeepPlaceholder:
      "Leave the masked value to keep the stored token",
    chatId: "Chat ID",
    chatIdAria: "Chat ID",
    chatIdPlaceholder: "-100…",
    smtpHost: "SMTP host",
    smtpHostAria: "SMTP host",
    smtpHostPlaceholder: "smtp.example.com",
    smtpPort: "SMTP port",
    smtpPortAria: "SMTP port",
    smtpPortPlaceholder: "587",
    smtpUsername: "SMTP username",
    smtpUsernameAria: "SMTP username",
    smtpUsernamePlaceholder: "ops{'@'}example.com",
    smtpPassword: "SMTP password",
    smtpPasswordAria: "SMTP password",
    smtpPasswordPlaceholder: "Leave blank for an open relay",
    smtpPasswordKeepPlaceholder:
      "Leave the masked value to keep the stored password",
    fromAddress: "From address",
    fromAddressAria: "From address",
    fromAddressPlaceholder: "Gotham <ops{'@'}example.com>",
    recipients: "Recipients",
    recipientsAria: "Recipients",
    recipientsPlaceholder: "ops{'@'}example.com, oncall{'@'}example.com",
    events: "Events",
    eventsAria: "Events",
    selectEvents: "Select at least one event",
    noEvents: "Select at least one event.",
    outOfScope:
      "This channel is subscribed to {names}, which its resource scope " +
      "cannot deliver. The stored subscription is kept unchanged unless " +
      "you edit the events or the scope.",
    eventsHintAll: "A global channel receives every selected event.",
    eventsHintApp:
      "Applications deliver deploy events; backup events cannot reach " +
      "this channel.",
    eventsHintDb:
      "Databases deliver backup events; deploy events cannot reach this " +
      "channel.",
    resourceScope: "Resource scope",
    resourceScopeAria: "Resource scope",
    scopeAll: "All resources",
    scopeApp: "Application",
    scopeAppUnavailable: "Application (unavailable)",
    scopeDb: "Database",
    scopeDbUnavailable: "Database (unavailable)",
    resourceAria: "Resource",
    selectApp: "Select an application",
    selectDb: "Select a database",
    missingResource: "{id} (missing)",
    unavailableBoth:
      "Applications and databases are unavailable on this control plane; " +
      "the channel can stay global.",
    unavailableApps:
      "Applications are turned off on this control plane; " +
      "a database scope is still available. Ask the owner to " +
      "enable them.",
    unavailableDbs:
      "Databases are turned off on this control plane; " +
      "an application scope is still available. The owner " +
      "must enable them.",
    enabled: "Enabled",
    enabledAria: "Channel enabled",
    secretsNote:
      "Secrets travel once, are sealed server-side and are never " +
      "displayed again — a read only returns the mask. A global channel " +
      "receives every selected event; a scoped channel only receives events " +
      "of the selected resource.",
    cancel: "Cancel",
    save: "Save",
    create: "Create channel",
  },
  toast: {
    updated: "Channel updated",
    created: "Channel created",
    deleted: "Deleted {name}",
  },
  errors: {
    sessionExpired: "Your session expired. Please sign in again.",
    forbiddenRole: "You do not have permission for this action.",
    featureDisabled:
      "Notification channels are turned off on this control plane. " +
      "Ask the owner to enable them.",
    invalidConfig: "Invalid channel configuration.",
    requestFailed: "Request failed",
    unexpected: "Something went wrong. Please try again.",
  },
};

export default en;
