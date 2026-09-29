// Package notifications delivers deploy and backup terminal events to the
// channels a team configured: Discord and Slack webhooks, Telegram bots and
// email over SMTP.
//
// A channel belongs to one team and either covers the whole team or overrides
// the team configuration for a single resource (an application or a database).
// Transport secrets — webhook URLs, bot tokens, SMTP passwords — are sealed
// with providers.SealSecret before they are stored, are never logged and never
// returned by the API: reads carry a redacted view instead.
//
// The package also implements the terminal-event hooks of the deploy and
// backup domains (deploy.Notifier and databases.BackupNotifier). Delivery runs
// asynchronously on a bounded worker queue, so a slow or failing endpoint can
// never block a deployment or a backup job.
package notifications
