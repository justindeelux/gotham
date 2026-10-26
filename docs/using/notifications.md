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
`certificate_expiring`, `update_available` and `redeploy_needed`.
New channels start with the four deploy/backup events selected. Server, certificate
and update events are opt-in for both existing and new channels: edit an **All resources** channel
and select them. Application and database channels cannot receive these
instance-wide events. `redeploy_needed` is opt-in as well, and only an
**All resources** channel or a channel scoped to that application can receive it.
A disabled channel receives nothing.

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

## Test send and delivery history

**Send test** delivers a synthetic test message to that one channel and reports
the result immediately. It is recorded in the history like a real attempt.

**Delivery history** shows the channel's newest 20 attempts, newest first: the
event (or **Test message**), delivered or failed, the time, and a bounded error
summary for failures. A channel keeps its last 50 attempts; older rows are
trimmed automatically. Delivery never blocks a deploy or backup — it runs on a
bounded background queue, and a failing channel does not stop the others.

For a scoped channel, creating, editing, deleting, enabling and testing needs
write access to the project the resource belongs to. A member outside that project
does not see the channel at all.
