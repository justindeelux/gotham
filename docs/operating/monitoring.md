---
title: Host pressure monitoring
---

# Host pressure monitoring

Gotham turns the observations a node agent already pushes into CPU, memory and
filesystem incidents. It does not poll node APIs and it does not keep sampled
history for alerting: each accepted observation is evaluated once.

Incidents appear in the notification inbox without configuring an external
channel. External delivery still requires a notification channel subscribed to
an incident event (`incident_opened`, `incident_severity_changed`,
`incident_recovered`).

## Defaults

| Rule | Warning | Critical | Clear | Sustained |
|---|---|---|---|---|
| CPU | ≥ 90% | — | ≤ 85% | warning 5 min, clear 1 min |
| Memory | ≥ 90% | — | ≤ 85% | warning 5 min, clear 1 min |
| Filesystem space | ≥ 90% | ≥ 95% | ≤ 85% | warning 1 min, critical 1 min, downgrade 1 min, clear 1 min |
| Filesystem inodes | ≥ 90% | ≥ 95% | ≤ 85% | warning 1 min, critical 1 min, downgrade 1 min, clear 1 min |

A spike shorter than the sustained window is silent. Critical filesystem
pressure downgrades to warning at ≤ 90% for one minute, so the incident stays
open while the disk is still above the clear value. Memory is used memory as a
percentage of total memory; Gotham never infers an OOM kill from that
percentage.

Filesystem warning and critical are measured from used bytes: `total -
available`. Inode pressure is measured from used inodes. A filesystem whose
space or inode counter cannot be read is **unknown**, not zero, and unknown
evidence never counts as recovery.

## Filesystems covered

The agent reports every candidate filesystem that is a distinct device:

- **root** — `/`, so a healthy control-plane partition is never assumed to
  cover the data.
- **Docker data** — the engine's `data-root`, commonly `/var/lib/docker`.
- **Managed storage** — the managed volume root when configured.
- **Backup** — the backup root when configured.

Paths on the same device are deduplicated, preferring the first candidate
(root, Docker, managed storage, backup), so a bind mount reached through two
paths produces one incident. One full Docker or backup mount therefore opens
one incident even when the root partition is healthy. A filesystem absent from
an accepted report is reported as unknown for its space and inode rules, so its
active incident stays visible and never resolves until fresh evidence arrives.

Not covered: SMART/temperature, directory-size crawls, disk forecasting and
automatic cleanup. Gotham never deletes data or restarts workloads to relieve
pressure.

## History

Filesystem usage is stored per node and stable filesystem identity in hourly
buckets with sample count, minimum, maximum and the fresh/partial/unknown
quality split. When the reported capacity changes (resize or replacement) the
segment advances, so later forecasting cannot cross the discontinuity. History
is kept for **30 days** and pruned by the monitoring sweep; active incident rows
belong to the incident lifecycle and are never pruned by that sweep.

## Configure rules in the UI

Open **Monitoring** in the sidebar (owner only). Choose **Instance defaults** to
change the rules inherited by every node, or pick a node to save an override.
Edit warning, critical, downgrade, clear percentages and their sustained
seconds, then **Save rules**. **Restore defaults** deletes the saved policy for
the selected scope: instance rules fall back to the built-in defaults, and a
node falls back to the instance rules.

Rules are validated in the form and again by the API: percentages must be finite
and ordered (`clear < downgrade ≤ warning < critical`), durations must be between
10 and 3600 seconds, and a node-scoped policy needs an existing node. Changes
apply to the next observation; history is not re-evaluated and no recovery is
invented.

## What the node detail shows

The node **Overview** card shows active pressure incidents, the last observation
and whether coverage is fresh, stale or unknown. It lists each filesystem's
mount, purpose, free/total space and free/total inodes, and links to the
**Metrics** tab for the underlying charts. Suggested action is to inspect
storage or logs, or reduce load.

The dashboard's **Needs attention** panel aggregates these incidents across the
resources the caller can see, with acknowledge, snooze, filters and recovery
status. See
[Notifications](/using/notifications#needs-attention-on-the-dashboard) for the
panel's behaviour and access rules.

## Application and database runtime health

Host pressure is only half of the picture: a node can have spare CPU and disk
while an application or managed database is stopped, unhealthy or restarting.
The same accepted observations also feed runtime health for applications and
databases. That evaluation is independent of deploy status, keeps running with
no browser open, and does not add a Docker poll on the control plane.

A running container without a healthcheck is **unknown**, not healthy. Complete
inventories are required to prove a container absent, and partial reports, an
old agent or an offline node show unknown coverage instead of reporting every
container missing. Compose replicas are tracked by service and container number,
so a healthy sibling cannot hide a failed replica. User stops, active
deployments and the 30–600 second startup grace suppress expected transitions;
restart loops need three increases in five minutes and stay open until five
minutes after the newest restart. OOM is reported only from Docker's explicit
`OOMKilled` flag on a new failed occurrence.

The runtime card on application and database details shows the state, affected
replica, observation time, last restart/OOM evidence and links to logs and
deployments. Runtime notifications are the separate `runtime_failure` and
`runtime_recovery` events; existing channel subscriptions are not broadened.
See [Runtime health](/using/runtime-health) for the full semantics and limits.

## Website reachability checks

Application domains are also checked from the control plane. Every enabled
managed domain gets one HTTP(S) request every 60 seconds (with jitter) and a
5-second timeout, independent of any open browser. The check uses `GET /` by
default and accepts any status from 200 to 399. A result is healthy only when
the final response matches that expected range — a TCP connection alone is not
enough.

Checks are bound to managed application domains. The request always uses the
domain's configured protocol and hostname plus the configured path; Gotham does
not accept an external host, a custom port, credentials, custom headers or
body-regex rules. Redirects are followed at most three times and only to the
same host; cross-host redirects fail the probe, and destinations that resolve
to loopback, private, link-local or metadata addresses are blocked. For local
deployments a blocked destination is reported as **unsupported** coverage
instead of suggesting that the safeguards be bypassed.

### Confirmation and recovery

Three consecutive completed failing probes open one incident. Two consecutive
successful probes recover it. Probe executor or database failures, disabled
periods, configuration changes and control-plane downtime are **unknown**
gaps: they never count as target failure and never count as healthy time. New
domains, configuration changes and a release that just finished honour a
120-second grace before the next scheduled probe, so a deploy or edit cannot
page on startup.

Transport, DNS and TLS failures, the response status and the successful
response latency are stored as separate evidence. Certificate validation
failures observed on the real request are reachability evidence; certificate
expiry continues to use the existing certificate observation and is not
duplicated by this feature.

### History and coverage

Categorical outcomes and the valid observation interval they account for are
kept for **30 days**. Application domain details return 24-hour and 7-day
history, reachable latency and observed availability beside coverage. Unknown
time is excluded from the uptime denominator, failed probes are not drawn as
zero latency, and below 90% coverage the UI shows insufficient coverage rather
than a standalone uptime percentage. Read ranges and chart points are bounded.

### Configure a check

Open the application's **Domains** tab and use the **Website checks** card:

- **Enable/disable** turns scheduled probing off for that domain. Disabling
  also closes its active website incident.
- **Path** is a relative path such as `/` or `/health`; query strings and
  fragments are rejected.
- **Expected status** is an inclusive range, by default 200–399.
- **Timeout** is between 1 and 30 seconds.
- **Test check** performs one probe immediately and never writes scheduled
  history or opens an incident.

Sites behind authentication usually answer 401 or 403. Adjust the expected
status range for those sites instead of adding credentials.

The check runs from the control-plane vantage point, so it proves what a user
reaching that control plane would see. It is not an Internet-wide SLA and does
not cover regions, external probe fleets, arbitrary TCP/UDP monitors or public
status pages.
