# Phase 16 — Instance settings (general, network, Linux system)

**Goal:** an admin-only **Settings → Instance** area where the operator configures the Gotham instance itself (Linear JUS-92). Until now this was only possible through `gotham.yaml` / environment variables.

## Scope
| Section | Fields | Applied by |
|---|---|---|
| General | control-plane URL, instance name (default `gotham`), timezone (IANA) | stored only; read through `instance.Service` |
| Network | DNS servers, IPv4 (dhcp / static address+prefix+gateway), IPv6 (enabled, dhcp / static) | host helper, **confirm + auto-revert** |
| System | hostname, NTP enabled, NTP servers | host helper |

Out of scope: wiring every existing consumer (agent enrollment, OAuth redirects, webhooks) to the stored control-plane URL — they keep reading `gotham.yaml` until a follow-up swaps them to `Service.ControlPlaneURL(ctx)`; per-NIC/bonding/VLAN configuration; non-systemd hosts.

## Config precedence
`env > DB > file defaults`, per field of the General section.

- env: `GOTHAM_PUBLIC_URL`, `GOTHAM_INSTANCE_NAME`, `GOTHAM_TIMEZONE` (also `instance.public_url|name|timezone` in `gotham.yaml`, which is the "file default" layer).
- The API reports each field as `{value, source: env|db|default, locked}`. A field whose source is `env` is **locked**: a write to it is a field error (`locked by environment variable`), the UI renders it read-only with the variable name.
- Network and system values have no env/file layer: they describe the host, so the DB row is the desired state and the helper is the applier.

## Persistence (migration 00047, forward-only)
- `instance_settings` — singleton row (`id smallint PRIMARY KEY CHECK (id = 1)`): general columns (nullable = "not set, use default"), network and system columns, plus a `pending_network` JSONB + `pending_deadline` for an unconfirmed network change (survives a control-plane restart).
- `instance_settings_audit` — append-only: `actor_id`, `section`, `changes` JSONB (old → new, no secrets exist in this module), `created_at`. Every successful write also logs through `slog`.

## API (Chi, platform-operator only)
Mounted under `/api/v1/instance` behind `RequirePlatformAdmin` (admin-scope token, `admin` role or `PLATFORM_ADMINS`); everyone else receives **403**.

| Method | Path | Notes |
|---|---|---|
| GET | `/instance/settings` | effective values + sources, host capabilities, pending network change |
| PUT | `/instance/settings/general` | validation errors → 400 `{message, errors:{field:msg}}` |
| PUT | `/instance/settings/system` | needs helper (409 when unavailable) |
| PUT | `/instance/settings/network` | applies tentatively, returns the pending change + deadline |
| POST | `/instance/settings/network/confirm` | keeps the change, cancels the revert |
| POST | `/instance/settings/network/revert` | reverts now |

Validation lives in the Go package (`validate.go`) and is mirrored by zod schemas in `web/src/features/instance-settings/schemas/`: URL is `http(s)` with a host, no userinfo/query/fragment; timezone must load via `time.LoadLocation` (and is not `Local`); DNS entries are IP literals (max 3, deduplicated); IPv4 static needs an address/prefix and an in-subnet gateway; IPv6 likewise; hostname is an RFC 1123 label (≤ 63) or dotted name (≤ 253); NTP servers are hostnames or IPs (max 4).

## Host helper and privilege model
Same model as `deploy/gotham-update.sh`: a **fixed, root-owned** script `/usr/libexec/gotham/gotham-hostctl`, reached through a sudoers drop-in (`deploy/install-hostctl-sudoers.sh`, validated with `visudo`) that grants **only the exact verbs** `apply-network`, `confirm-network`, `revert-network`, `apply-system`, `status`. There is no shell, no arbitrary path and no environment override under sudo.

- Data travels on **stdin** as `key=value` lines. The helper accepts only a whitelist of keys and re-validates every value with strict anchored patterns before it is written into a file or passed as an argument — the control plane is treated as untrusted.
- Supported stack: `systemd-networkd` + `systemd-resolved` + `systemd-timesyncd`, written as Gotham-owned drop-ins (`10-gotham.network`, `resolved.conf.d/gotham.conf`, `timesyncd.conf.d/gotham.conf`). `status` reports `{network: supported|unsupported, system: supported|unsupported}`; on an unsupported host the UI shows the sections read-only and writes return 409. A missing helper behaves the same (dev machines, containers).
- `hostnamectl set-hostname` applies the hostname; `timedatectl set-ntp` toggles sync.

### Rollback / lock-out protection (network)
1. `apply-network` snapshots the Gotham-owned files (or records their absence) under `/var/lib/gotham-hostctl/backup`, writes them, reloads networkd/resolved, and arms a **revert timer** (`systemd-run --on-active=<N>s <helper> revert-network`, N = 60–300, default 120) plus a root-owned pending marker.
2. The control plane stores the change as *pending* with the deadline and returns it. The UI opens a **confirm dialog with a countdown**; the operator reaches the control plane at the new address and presses *Keep changes* → `confirm-network` stops the timer and removes the marker.
3. If the operator cannot reach the control plane (wrong address/gateway), nobody confirms; the timer fires, the helper restores the snapshot and reloads — **auto-revert needs no cooperation from the control plane or the network**.
4. The control plane reconciles lazily (on `GET` and at startup): a pending change past its deadline is marked reverted and the previous desired values are restored in the DB, so the UI never shows a state the host does not have.
5. A second network write while one is pending is refused (409). *Revert now* is available while pending.

## Frontend (`web/src/features/instance-settings`)
Pages `settings/instance` (tabs General / Network / System) in Naive UI, mockup `docs/design/instance-settings.html` ported first. API client + `parseWith` envelopes, zod schemas in `schemas/` driving `ruleFrom` rules and server field errors mapped back onto fields, en + vi locale catalogs, confirm dialog with countdown (`useNetworkConfirm`). The nav entry/route/titles are additive one-line edits to shared files (admin-only visibility follows the profile "platform admin" flag).

## Work packages
| Id | Title | Migration |
|---|---|---|
| IS-1 | `internal/instance` service, store, routes, audit | 00047 |
| IS-2 | `deploy/gotham-hostctl.sh` + sudoers installer + `deploy/README.md` section | — |
| IS-3 | Mockup + Vue pages + schemas + locales + webdist | — |

## Verify
`go build ./...`, `go test ./...` (fake `HostApplier`, no root needed), `golangci-lint run`, `go vet`, `npm run build`, `npm run type-check`, `webdist` committed, `sqlc-check` clean. The helper script gets a `sh -n` + `shellcheck` (when available) pass and a dry-run test that executes it against a temp root.
