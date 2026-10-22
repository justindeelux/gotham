# Feature inventory (ground truth)

Source of truth for what the product actually does, extracted from code at commit
`d77c6aaba`. Use this — not `plans/` — when writing user-facing documentation.
`plans/*.md` are aspirational and stale in places (see Corrections).

Refresh: re-grep the route tables, CLI commands, and `web/src/app/router/index.ts`,
then diff against this file. Do not hand-edit rows without a code check.

Evidence convention: `file:line` in this repo. HTTP paths below the `/api/v1` mount.

## Corrections vs plans

- No top-level Applications / Databases / Services pages: `/applications`,
  `/databases`, `/services` redirect to `/projects`. Resources live at
  `/projects/{id}/environments/{id}/{applications|databases|services}/{id}`.
- Surfaces missing from the phase list: Terminal (host + container), App Store
  (`/toolchain`), Teams + invites, Git sources (`/settings/git-sources`),
  notification channels, PR previews, instance network settings.
- OAuth login is **GitHub only** (`cmd/gotham/main.go:294-307`); the route is
  `{provider}`-shaped but only one provider is wired.
- Build engines railpack / buildpacks are detection-only (`ErrEngineNotWired`);
  only Dockerfile and static builds work (`internal/builds/doc.go:6-8`).

## End-user features (web console)

| Area | What the user can do | Evidence |
|---|---|---|
| Auth | Register (first account on the instance becomes platform admin), login, GitHub OAuth, TOTP 2FA + recovery codes | `web/src/features/auth/pages/LoginPage.vue`, `web/src/features/auth/pages/RegisterPage.vue`, `internal/store/sqlc/users.sql.go:39-43` |
| Dashboard | KPI tiles (servers ready, running applications; the deploys/24h and SSL tiles are empty placeholders), server health grid, recent deploys | `web/src/features/dashboard/pages/DashboardPage.vue:172-227`, `web/src/features/dashboard/locales/en.ts:15-27` |
| Projects | Create project (auto production env), rename/delete, shared variables | `web/src/features/projects/pages/ProjectsPage.vue:109`, `internal/projects/service.go:230` |
| Environments | Add resource (app / service / database), shared variables, switch sibling env | `web/src/features/add-resource/components/ResourcePicker.vue:50`, `web/src/features/projects/composables/useEnvironmentPage.ts:413` |
| Applications | Deploy, redeploy, rollback, start/stop; tabs Overview, Deployments, Logs, Environment, Terminal, Storage, Domains, Previews, Settings | `web/src/features/applications/pages/ApplicationDetailPage.vue:123-288` |
| Databases | PostgreSQL, MySQL, MariaDB, MongoDB, Redis; start/stop/restart; credentials + connection string; backups, restore, cron schedules, S3 targets | `internal/databases/engine.go:12-16`, `internal/databases/routes.go:141-144`, `internal/databases/backup_routes.go:181-198`, `web/src/features/databases/composables/useDatabaseDetail.ts:141-147` |
| Services | One compose project per service; edit compose.yaml; deploy/stop/restart; logs; deploy history | `web/src/features/services/pages/ServiceDetailPage.vue:67-122` |
| Templates | One-click: WordPress, Nextcloud, n8n, Uptime Kuma; render to compose + env | `templates/embed.go:15-16` |
| Domains & SSL | Attach domains per app, set primary, certificates (http-01 / dns-01) | `internal/deploy/routes.go:461-465`, `internal/proxy/certificates.go:20` |
| Git sources | Connect GitHub App / GitLab / Gitea; list repos and branches | `web/src/features/applications/pages/GitSourcesPage.vue`, `internal/providers/provider.go:17-20` |
| Previews | PR preview deployments (provider webhook signature-verified) | `internal/webhooks/routes.go:109-118` |
| Container terminal | Shell into a running application container (team write access) | `internal/terminal/routes.go:186-189` |
| Teams | Create team, rename/delete, member roles, single-use invite links | `web/src/features/teams/pages/TeamsPage.vue:67-76` |
| Notifications | Discord / Slack / Telegram / email (SMTP) channels, test send, delivery log | `internal/notifications/routes.go:80-86`, `internal/notifications/model.go:44-57` |
| Profile | Display name, change password, 2FA, active sessions, revoke others | `web/src/features/profile/pages/ProfilePage.vue:49-54` |

Roles: `owner`, `admin`, `read_only` (`internal/teams/role.go:9-19`). `read_only` can read
everything, writes are rejected server-side; several console controls stay visible and
rely on API rejection (`web/src/features/projects/stores/projects.ts:76-79`).

## Operator-only features

| Area | What the operator can do | Evidence |
|---|---|---|
| Nodes | Add server (4-step wizard: connect, SSH validate, install, finish), edit, delete, reset host key | `internal/server/servers_routes.go:208-216`, `web/src/features/servers/components/AddServerWizard.vue:127-135` |
| Node metrics | CPU/memory/disk/network series, 30-day retention | `internal/server/servers_routes.go:220`, `internal/servers/metrics.go:34` |
| Containers | List, start/stop/restart, pull image, run image, stream logs | `internal/containers/routes.go:69-74`, `internal/server/server.go:557` |
| Host terminal | PTY shell on a node, session bound to the creating user | `internal/terminal/routes.go:150-153` |
| File manager | Browse/edit/upload (1 GiB/file)/chmod/compress/extract, trash, signed download | `internal/files/routes.go:107-135`, `internal/files/write.go:24` |
| App Store | Install/uninstall 14 host tools (docker, git, railpack, buildkit, …) with job queue | `toolcatalog/toolcatalog.go:34-47` |
| Domains & SSL | Traefik router list, DNS providers (Cloudflare/DigitalOcean), certificates, redirects | `internal/proxy/routes.go:64-95`, `internal/server/proxy_routers.go:24` |
| Instance settings | Control-plane URL, name, timezone; DNS/IPv4/IPv6 with confirm/revert countdown; hostname/NTP; host helper | `internal/instance/routes.go:66-74`, `internal/instance/model.go:67-68,117-119` |
| Updates | Check release, changelog, schedule, apply; rollback via CLI; stable/beta channel; fleet agent rollout | `internal/updates/routes.go:107-117`, `cmd/gotham/update.go:55`, `internal/updates/checker.go:43-45`, `internal/server/servers_routes.go:230-231` |
| CLI | `gotham serve`, `admin create|exists|reset-password`, `ca init`, `migrate up|down|status`, `update check|apply|rollback|reset` | `cmd/gotham/` |
| Agent CLI | `gotham-agent serve`, `update reset`, `version` | `cmd/gotham-agent/main.go:60-68` |
| API tokens | `gt1_` tokens with read/deploy/admin scopes, rotate, revoke (API only, no UI page yet) | `internal/auth/apitoken.go:27,34-36`, `internal/server/tokens.go:68-71` |
| Feature flags | `FEATURE_APPLICATIONS/DATABASES/SERVICES/TEAMS/FILES/NOTIFICATIONS/PROXY/METRICS/UPDATES/PREVIEWS`, all default on | `internal/server/server.go:289-311` |

## Proposed site structure

Two tracks, because the audiences and permissions differ.

```
Guide/                install · first login & node enrollment · updates & rollback · troubleshooting
Using Gotham/         dashboard · projects & environments · applications · databases & backups
                      services & compose · templates · domains & SSL · container terminal
                      git sources & previews · teams & invites · notifications · profile & 2FA
Operating Gotham/     nodes & containers · host terminal · file manager · app store
                      instance & network settings · proxy & DNS · security model · feature flags
Reference/            CLI · configuration keys · API tokens
```

Admin-only console surfaces (`/files`, `/terminal` host shells, `/domains`,
`/settings/{instance,updates}`) belong in Operating Gotham, not the end-user
track, even though they sit in the same UI.
