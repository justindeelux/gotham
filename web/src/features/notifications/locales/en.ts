/**
 * English catalog for the notifications feature: page shell, team scope
 * card, channel cards, and the channel create/edit dialog.
 * Wire values (channel kinds, event keys, URLs, ports, email addresses)
 * stay raw; only framing copy lives here.
 */
const en = {
  page: {
    eyebrow: "Team · Notifications",
    title: "Notification channels",
    description:
      "Deploy and backup events are delivered to the team's channels: " +
      "Discord and Slack webhooks, a Telegram bot, or SMTP email. Channels " +
      "are team-scoped, and secrets are write-only — reads only ever show a " +
      "masked value.",
    newChannel: "New channel",
    unavailableTitle: "Notifications unavailable",
    unavailableDesc:
      "Notification channels are not enabled on this control plane " +
      "(FEATURE_NOTIFICATIONS=false).",
    empty: "No channels for this team yet.",
    createFirst: "Create the first channel",
  },
  scope: {
    title: "Team scope",
    teamAria: "Notification team",
    yourRole: "your role: {role}",
    readOnlyNote:
      "Channels below belong to this team. A read-only role can look but " +
      "not change anything.",
  },
  card: {
    enabledLabel: "Enabled",
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
  },
  scopeLabel: {
    teamWide: "Team-wide",
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
    eventsHintTeam: "A team-wide channel receives every selected event.",
    eventsHintApp:
      "Applications deliver deploy events; backup events cannot reach " +
      "this channel.",
    eventsHintDb:
      "Databases deliver backup events; deploy events cannot reach this " +
      "channel.",
    resourceScope: "Resource scope",
    resourceScopeAria: "Resource scope",
    scopeTeamWide: "Team-wide (all resources)",
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
      "the channel can stay team-wide.",
    unavailableApps:
      "Applications are unavailable on this control plane " +
      "(FEATURE_APPLICATIONS=false); a database scope is still available.",
    unavailableDbs:
      "Databases are unavailable on this control plane " +
      "(FEATURE_DATABASES=false); an application scope is still available.",
    enabled: "Enabled",
    enabledAria: "Channel enabled",
    secretsNote:
      "Secrets travel once, are sealed server-side and are never " +
      "displayed again — a read only returns the mask. A team-wide channel " +
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
    forbiddenRole: "Your team role does not allow this action.",
    featureDisabled:
      "Notification channels are not enabled on this control plane " +
      "(FEATURE_NOTIFICATIONS=false).",
    invalidConfig: "Invalid channel configuration.",
    requestFailed: "Request failed",
    unexpected: "Something went wrong. Please try again.",
  },
};

export default en;
