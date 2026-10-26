---
title: Notifications
---

# Notifications

Gotham records deploy and backup results in the in-app inbox, even when no
notification channel is configured. Channels are an optional way to send the
same events to Discord, Slack, Telegram or email outside the panel.

## In-app inbox

Every signed-in person has a bell in the topbar, with a badge for unread
notifications. Click it, or focus it and press Enter or Space, to open the inbox.
It loads the newest 30 notifications you can see, newest first. Each entry shows
the deploy or backup result, the application or database name, the host or error
when available, and a relative time.

The inbox records deploy success, deploy failure, backup success and backup
failure. **Send test** only tests a channel; it does not create an inbox entry.
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
to one application or one database, and only people who can reach that resource
see it in the list.

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

A channel watches exactly one application or one database:

- An application channel receives that application's deploy events.
- A database channel receives that database's backup events.

The subscribable events are `deploy_success`, `deploy_failure`,
`backup_success` and `backup_failure`. A new channel subscribes to all of them
unless you select a subset. A disabled channel receives nothing.

## Test send and delivery history

**Send test** delivers a synthetic test message to that one channel and reports
the result immediately. It is recorded in the history like a real attempt.

**Delivery history** shows the channel's newest 20 attempts, newest first: the
event (or **Test message**), delivered or failed, the time, and a bounded error
summary for failures. A channel keeps its last 50 attempts; older rows are
trimmed automatically. Delivery never blocks a deploy or backup — it runs on a
bounded background queue, and a failing channel does not stop the others.

Creating, editing, deleting, enabling and testing a channel needs write access
to the project the resource belongs to. A member outside that project does not
see the channel at all.
