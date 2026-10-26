---
title: Domains and previews
---

# Domains and previews

Attach hostnames to an application, record the certificate it should receive,
connect a Git source, and let Gotham build a throwaway copy of the application
for every pull request. Certificate issuance and Git connections are handled by
the control plane; the node terminates traffic and performs ACME.

## Attach a domain

Open an application and use its **Domains** tab. **Add domain** asks for:

- **Host** — a DNS name such as `app.example.com`.
- **Port** — the container port the application listens on (1–65535), or empty
  to pick it at deploy time: the image's exposed port when the source declares
  one (Dockerfile `EXPOSE` or a compose service port), otherwise a generated port.
  Gotham publishes the chosen port and sets `PORT` to it. A typed port overrides
  that choice.
- **Protocol** — `http` or `https`.
- **Service** — only for compose applications, to route to one compose
  service instead of the release service. Picking a service pre-fills its
  declared port, which stays editable.

The first domain attached to an application becomes its **primary** domain.
Use **Make primary** on another row to change it; the primary is what display
surfaces and preview provisioning use. Removing the primary promotes the
oldest remaining enabled domain, so an application with domains always keeps
one.

A hostname is claimed platform-wide: a host already attached to another
application or service, or used as a redirect source, is rejected. Point the
host's A/AAAA record at the node hosting the application before relying on the
route; the first deploy of a new application starts with no route until a
domain is attached.

Renaming a host removes the automatic certificate record for the old hostname,
so record a certificate for the new host afterwards. Detaching a domain removes
only that host's routes.

## Certificates

Certificates are configured per domain — one configuration per domain — from
the same **Domains** tab. The control plane stores the intent; the node performs
the ACME order and keeps the certificate in its own storage. Deleting a
configuration falls back to plain HTTP for that host.

Each configuration has a challenge:

| Challenge | Use it when | Requirements |
|---|---|---|
| `http-01` | The default. | The host resolves directly to the node and port 80 is reachable from the internet. A proxied Cloudflare record must be **DNS only**. |
| `dns-01` | The host cannot serve port 80, or you need a wildcard. | A DNS provider whose configured zone covers the domain. Wildcard certificates are only available with `dns-01`. |

Attaching an `https` domain records a default `http-01` intent for that host
automatically. You can edit it, switch it to `dns-01`, or turn it off. A
`dns-01` configuration must name a provider, and the provider must hold a zone
that covers the domain (and the wildcard base, when a wildcard is requested).
The zone's nameservers must actually be delegated to that provider, or the
order fails.

**DNS provider credentials are owner-only.** They are added, rotated and
deleted on the owner's Domains & SSL page, and the API never returns a stored
credential. See [Proxy and DNS](/operating/proxy-dns).

The status shown next to a configuration is observed live from the node, never
stored: `present` means the node holds a certificate covering the domain and
its expiry is shown, `absent` means the storage was read and holds none, and
`unknown` means the node could not be read. `unknown` is not a failed order —
it means the control plane has no observation to report.

### Redirects

Redirect rules (one exact source host to one exact target host, with a
permanent or temporary code and optional path preservation) are managed on the
owner's Domains & SSL page; see [Proxy and DNS](/operating/proxy-dns).

## Git sources

Open **Settings → Git sources** (`/settings/git-sources`). Only the owner manages
Git connections and GitHub Apps. API-token scopes still apply to API calls.
Secrets — client secrets, admin tokens and webhook secrets — are write-only
and never displayed after they are stored.

- **GitHub App** — **Connect GitHub** posts an app manifest to GitHub under the
  name you give it (letters, digits and dashes) and redirects back here. Then
  install the app on the repositories it should see.
- **GitLab** — enter the instance URL (`gitlab.com` or a self-hosted instance).
  Automatic provisioning calls the instance once with a one-time admin token
  that is never stored; alternatively, use the manual fields with the
  application ID, secret and the exact redirect URI shown in the dialog. After
  saving, use **Reconnect** to finish the sign-in.
- **Gitea** — supported by the provider layer and configured with its instance
  URL and OAuth application. The console has no Gitea connect flow: a Gitea
  connection is created through the API and its row is listed read-only.
  Applications that already deploy through it keep working.

The list shows each connection's status, account or instance, installation and
repository counts, and how many applications on the instance name it. The
application wizard lists the repositories a connection can see and, once a
repository is chosen, its branches; the branch you pick is the branch pushes
deploy from.

Disconnecting a GitLab connection is refused while applications still deploy
through it, and the error names them. A GitHub App can always
be disconnected: Gotham reports how many applications named it, and those
applications must be pointed at another source before their next deploy.

## Previews

A preview is a sibling application built from a pull request's head branch. It
lets you review a change on its own URL without touching the base application's
deployment. Previews need three things: an application with an installed Git
webhook, a branch to watch, and a primary domain. Gotham installs the webhook
automatically when you create an application from a connected source — there is
no manual webhook setup in the console.

### Delivery and verification

The Git host posts `pull_request` events to Gotham's per-provider webhook
endpoint. Every delivery is verified before anything is built:

- GitHub signs the body with HMAC SHA-256 in `X-Hub-Signature-256`.
- GitLab sends the shared secret in `X-Gitlab-Token`.
- Gitea signs the body with HMAC SHA-256 in `X-Gitea-Signature`.

The comparison is constant-time against the secret generated when the hook was
installed; an unverifiable delivery is refused and never reaches the deploy
service. Deliveries are also rate-limited per client IP, and a replayed
delivery for the same pull request head is treated as a duplicate instead of
starting a second build.

### Lifecycle

A preview is created when a pull request is opened or reopened **against the
branch the application watches**, and refreshed when new commits are pushed to
it. Closing or merging the pull request tears the preview down; the binding row
stays as an audit record. Everything else is ignored: fork pull requests
(their code is not the repository the application trusts), other base branches,
and metadata-only actions such as labels or reviews.

Each preview is a sibling application named `<application>-pr-<number>` with
the host `http://pr-<number>-<slug>.<primary-domain>`. It is HTTP-only: the
sibling receives no certificate of its own. It carries the base application's
plain environment variables and its shared deploy key, but deliberately not its
sealed secrets or volumes.

One application can have at most five live previews. A sixth pull request is
acknowledged and ignored until one of the five closes. When the pull request has
a preview, Gotham posts a best-effort comment on it with the URL and the latest
deployment result.

### The Previews tab

The **Previews** tab on the application page lists the application's preview
bindings, newest first: pull request, head branch, preview URL, state, head
commit, and creation/deletion times. States are `active`, `deploying`,
`closing`, `failed` and `deleted`.

The tab is shown on the application page while previews are enabled on the
instance, including when the application has no previews yet. With
`FEATURE_PREVIEWS=false` on the control plane, no preview route is mounted: the
tab hides instead of showing an error, and `pull_request` deliveries are
ignored. Push-to-deploy is unaffected by that flag — pushes to the watched
branch still queue deployments.
