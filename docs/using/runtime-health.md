---
title: Runtime health
---

# Runtime health

Gotham watches applications and managed databases after a deploy without
opening the panel. A successful deploy remains a pipeline result; whether the
resource is healthy **now** is a separate runtime evaluation. The node agent
pushes container evidence about every 15 seconds, and the control plane
evaluates the newest accepted inventory. No browser has to be open and the
control plane does not poll Docker on a schedule.

Open an application or database to see the runtime card, or read the runtime
badge beside its status in → the environment resource list. The card shows the
state, when it was measured, coverage, the affected service/replica, the
incident reason, and the last restart or OOM evidence. Application cards link
to the existing logs and deployments tabs.

## States and coverage

| State | Meaning |
| --- | --- |
| healthy | Every expected replica is running and every replica that declares a native healthcheck reports healthy. |
| starting | A container is being created/started, or the resource is inside its startup grace after a start or deploy. |
| degraded | A replica is running but failing its healthcheck, stuck restarting, or recently OOM killed. |
| unavailable | An expected replica is absent or stopped unexpectedly, or a container is paused. |
| intentionally stopped | The owner stopped the resource, or the database status is stopped. This is not a failure. |
| unknown | There is no current proof: no observation yet, stale evidence, an incomplete inventory, an old agent, or a running container without a healthcheck. |
| disabled | Continuous monitoring is turned off for this resource. |

A running container **without a healthcheck is unknown, not healthy**. Gotham
only claims healthy when a declared healthcheck says so. Unknown coverage is
shown explicitly instead of claiming that every container disappeared or
recovered.

## Complete inventories prove absence

An incident for a missing container only opens from a **complete** inventory.
When the agent cannot inspect every managed container, the inventory is
partial, and Gotham marks existing evidence unknown and keeps timing paused. An
older agent that does not push inventories at all is also unknown coverage.
When a node is offline, its runtime evidence becomes unknown instead of
reporting every container as missing or recovered. A notification that comes
back while the resource is still down re-evaluates the surviving evidence.

## Expected stops and startup grace

These transitions do not raise a runtime failure:

- A user **stop** of an application or database. The resource is shown as
  intentionally stopped until it is started or deployed again.
- A **deploy, restart or replacement** while the existing deployment is still
  in progress, and for the startup grace after it finishes.
- A database that is **creating, deleting or stopped**.
- A supported **one-shot job**: a container that exits with code 0 under the
  Docker `restart: "no"` policy. Use `restart: "no"` for jobs; long-running
  services should use `always` or `unless-stopped`. Because `no` is also the
  Compose default, a long-running service that is stopped cleanly outside
  Gotham can look like a completed one-shot; the restart policy above avoids
  that false negative.

The startup grace defaults to **120 seconds** and can be tuned from **30 to 600
seconds** in the runtime card by the owner or a project writer. An operation
that stops moving does not suppress failures forever: an in-flight deployment
is only treated as expected for its bounded operation window.

## Compose replicas

For a Compose application, replicas are grouped by the application and Compose
service, with the Compose container number as the stable replica identity and
the container and deployment IDs as evidence. If one replica fails, the card names the
affected service and replica; a healthy sibling cannot hide it. A non-Compose
application is evaluated as one expected container. Databases are evaluated as
one managed container each.

## Restart loops and OOM kills

Gotham baselines the restart counter on the first observation. A restart loop
incident opens when the counter increases by at least **3 within a rolling
five-minute window**. Counter resets, a recreated container, a redeploy and a
stale evidence gap restart the baseline, so they never produce a negative or
enormous delta. The incident closes once five minutes pass without a new
restart and fresh running, acceptable health evidence is available.

OOM detection is explicit: it needs Docker's `OOMKilled` flag on a new
terminated container occurrence. Exit code 137 alone is never treated as OOM,
and the same old occurrence is not reported on every sample. A restart or a new
container closes the OOM incident once the service is running again.

## Permissions and notifications

Only people who can already read the application or database see its runtime
card and list badge; logs keep their existing authorization and masking.
Owners and project writers can enable monitoring and tune the startup grace.

Runtime notifications reuse existing notification channels. Two new explicit
events, **Runtime failure** and **Runtime recovered**, are available for
application and database channels. They are not added to existing channel
subscriptions automatically, so turning monitoring on never broadens an
external channel you did not choose.

## Limits

Gotham does not restart or repair a failed resource automatically, does not
chart per-container numeric metrics, does not analyse database queries, and
does not add a scheduled Docker inventory poll on the control plane. Detection
depends on evidence pushed by a current agent.
