---
title: Notifications
---

# Notifications

Gotham records deploy and backup results, server connection changes, certificate
expiry warnings, available Gotham updates and applications that need a redeploy
in the in-app inbox, even when no notification channel is configured. Channels
are an optional way to send the same events to Discord, Slack, Telegram or email
outside the panel.

## In-app inbox

Every signed-in person has a bell in the topbar, with a badge for unread
notifications. Click it, or focus it and press Enter or Space, to open the inbox.
It loads the newest 30 notifications you can see, newest first. Each entry shows
the event, the resource, domain or version, the host or error when available, and
a relative time.

The inbox records deploy success, deploy failure, backup success and backup
failure, plus server offline, server back online, certificate expiry warnings,
Gotham update availability and a redeploy needed by an application. The redeploy
entry names the pending setting (domains or ports, environment variables, secrets
or storage) and opens the application. Server, certificate and update events are
owner-only. **Send test** only tests a channel; it does not create an inbox entry.
New notifications arrive without reloading while the page is open. If the live
connection is unavailable, the visible page refreshes the inbox about once a
minute.

Opening the inbox marks notifications through the newest loaded entry as seen,
including older entries beyond the displayed list. The unread count belongs to
your account and is saved across reloads and devices.

Click an entry to open its application or database. If the resource no longer
exists, the notification stays in the inbox, but its destination is unavailable.
Gotham keeps the newest 500 notifications across the whole instance and trims
older ones automatically.

The owner sees every notification. Members see only notifications for their
granted projects, created from the time access was granted onward. Revoking a
grant hides those notifications immediately; notifications without a project are
owner-only. See [Members and access](/using/members-access).

## Optional channels

Open **Settings → Notifications** (`/settings/notifications`). A channel belongs
to **All resources**, one application or one database. Only the owner can see or
manage an All resources channel; a scoped channel is visible to people who can
reach its resource.

| Kind | Configuration |
|---|---|
| Discord | Incoming webhook URL. |
| Slack | Incoming webhook URL. |
| Telegram | Bot token and the chat ID the bot posts to. |
| Email (SMTP) | SMTP host, port, optional username and password, from address and at least one recipient. |

Transport secrets — webhook URLs, bot tokens, SMTP passwords — are sealed
server-side before they are stored, are never logged and are never returned by
the API. Reads show a masked value with at most the last four characters, and
a saved connection shows whether a secret is configured. Editing a channel and
leaving the masked value in place keeps the stored secret; the channel kind
cannot be changed after creation.

## Scope and events

Choose the scope when creating or editing a channel:

- An **All resources** channel receives every selected event, including
  instance-wide server, certificate and update events.
- An application channel receives that application's deploy events and its
  redeploy-needed event.
- A database channel receives that database's backup events.

The subscribable events are `deploy_success`, `deploy_failure`,
`backup_success`, `backup_failure`, `server_offline`, `server_online`,
`certificate_expiring`, `update_available`, `redeploy_needed`,
`incident_opened`, `incident_severity_changed` and `incident_recovered`.
New channels start with the deploy/backup events and the three incident events
selected. Server, certificate
and update events are opt-in for both existing and new channels: edit an **All resources** channel
and select them. Application and database channels cannot receive these
instance-wide events. `redeploy_needed` is opt-in as well, and only an
**All resources** channel or a channel scoped to that application can receive it.
A disabled channel receives nothing.

A monitoring incident is a condition that is still actionable, not a single
delivered event. Gotham keeps it pending until the condition is sustained, then
firing, and only marks it recovered after stable healthy evidence; restarting
the control plane does not erase a firing incident. An occurrence opens once,
reports a severity change once and resolves once. Acknowledging or snoozing an
incident silences delivery without changing its severity or lifecycle, and the
panel shows why delivery is held. Resolved incidents stay in history for 30
days; active incidents are never pruned. Node connectivity still uses the
existing `server_offline`/`server_online` event: the incident view groups the
node's dependent problems beneath it with unknown coverage instead of sending a
second connectivity message or claiming a cause.

An application needs a redeploy when a setting that only reaches a new container
changes: domains or ports, environment variables, secrets, or storage. Exactly one
`redeploy_needed` event is recorded on the first change after a successful deploy;
further changes while it is pending are silent, and the next successful deploy
clears the flag so a later change notifies again.

A server is considered offline after at least 30 seconds without a heartbeat.
Gotham checks every 10 seconds, even when nobody has the servers page open, and
waits 30 seconds after starting before checking for offline servers. It records
one offline event per transition and one recovery event on the first successful
heartbeat after that. Registration alone and first enrollment do not send a
recovery event. Shorter interruptions do not trigger an alert; repeated outages
longer than the grace period can produce repeated offline/recovery pairs.

Gotham checks enabled panel and application certificates when it starts and then
once an hour. It warns when a certificate has seven days or less left, after two
successful observations in that window. Already expired certificates also qualify.
Unknown, missing or disabled certificates and failed reads are skipped without
counting as an observation. A failed read does not reset a pending warning.
Each certificate and expiry date produces one warning; a renewed certificate
with a new expiry date can trigger a warning near that date.
Restarting Gotham clears this memory, so a certificate still near expiry may notify again.

An update-available notification is created when a successful manual or scheduled
Gotham update check finds a newer version for the selected update channel. Each
channel/version pair notifies once; a failed check or an up-to-date result does
not clear that memory. Notifications require the update service to be running:
`FEATURE_UPDATES=false` disables it. With auto-update off, scheduled checks are
off by default, so only manual checks in the panel notify unless a check schedule
has been enabled. These notifications do not enable checks or automatic updates.
Restarting Gotham clears this memory, so the same available update may notify again.

## Needs attention on the dashboard

The dashboard opens with **Needs attention**, the current state for every
resource you can see. It is separate from the notification inbox: unread counts
are event history, while this panel shows which conditions are still active.

Active incidents are sorted critical first and then by how long they have been
active, so the oldest critical condition comes first. Each row gives the plain
reason, the resource, when it started, the latest valid observation and the
coverage of the evidence behind it. **Severity** and **Resource** filters and
**Load more** keep large result sets bounded. Members only see incidents for
projects they can currently access; revoking a grant removes the rows and their
counts immediately.

When a server is unreachable, its dependent application and database incidents
are grouped beneath it with unknown coverage. Grouping shows what is affected,
not a proven cause: Gotham does not claim that the server outage caused those
symptoms.

**Acknowledge** records who is handling the condition and silences delivery;
**Snooze** silences it for 1 hour or 24 hours, and the row shows the exact
expiration. Neither action changes the measured severity or lifecycle, and
opening the inbox or an incident never acknowledges it. Acknowledged and snoozed
conditions stay visible with their status, and the row shows why delivery is
held. **Recently resolved** is a separate, bounded view of recoveries.

The panel follows the authenticated realtime channel: a new or recovered incident
refreshes the open dashboard, a reconnect refetches, and a visible tab falls back
to a 60-second refresh. Distinct messages distinguish an API outage, no server
having reported yet, monitoring not being available on the instance, and a
genuinely healthy system. Links open the application or database detail (and logs
for runtime failures), the server metrics, or notification settings. Unknown
detectors and resource types get a neutral label; raw evidence, errors, secrets
and host paths are never shown for them.

## Test send, retries and delivery history

**Send test** delivers a synthetic test message to that one channel and reports
the result immediately. It is recorded in the history like a real attempt and
updates the channel health, but it creates no inbox entry.

**Delivery history** shows the channel's newest 20 attempts, newest first: the
event (or **Test message**), the state, the time, the attempt count, the next
retry time and a bounded error summary. States are **Pending**, **Sending**,
**Retrying**, **Delivered**, **Failed** and **Cancelled**. Cancelled means the
work became obsolete (the channel was disabled, deleted or unsubscribed, or an
opening was never delivered); it is never shown as delivered. A channel keeps
its last 50 non-terminal attempts. Terminal failures and cancellations are kept
for 30 days so the evidence survives, and are then removed automatically.

Delivery work is written to the database before a worker is woken, so a
control-plane restart or a crash between persistence and send resumes eligible
work instead of losing it. A worker claims a bounded batch, and an abandoned
sending lease is recovered after a restart. Failures retry with exponential
backoff and jitter: connection failures, timeouts, HTTP 429 and 5xx. A bounded
`Retry-After` is respected. Each delivery stops after at most 8 attempts or 24
hours. Invalid configuration, authentication failures and provider rejections
are terminal until an operator acts. One failing channel does not block the
others or a deploy/backup.

Before sending, Gotham re-reads the channel's enabled state, subscription, scope
and encrypted configuration. Disabled, deleted or unsubscribed work is
cancelled. Per-incident/channel transitions keep their order, so an opening is
not delivered after its recovery; if an opening expired or was cancelled before
any successful send, the recovery for that channel is suppressed with the reason
kept in history.

Settings shows each channel's current health: **Delivering**, **Retrying**,
**Cannot deliver** or **No attempts yet**. When the owner has terminal delivery
failures, **Needs attention** lists them with the channel, event and bounded
error, and offers **Retry**. A successful explicit retest or a later valid
delivery clears the current health failure; the historical failure stays in the
history. If the only configured destination is broken, the in-panel health and
attention item are the fallback; Gotham never sends a failure through the same
broken channel. Retry reuses the existing local identity and the current
configuration, so it creates no duplicate inbox event.

Delivery blocks nothing and runs on the control plane. Retries and local
enqueue are guaranteed to be deduplicated locally, but not external
exactly-once delivery: a provider may accept a message whose response is lost,
and the retry can then deliver it a second time.

For a scoped channel, creating, editing, deleting, enabling and testing needs
write access to the project the resource belongs to. A member outside that project
does not see the channel. Channel health and terminal delivery errors are
owner-only.
