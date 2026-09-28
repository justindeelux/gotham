# Phase 6 — Proxy, Domains & SSL (W8–W9) — parallel with Phase 5

**Goal:** automatic HTTP + HTTPS routing via Traefik with domain management.

**Exit criteria (Milestone M6):**
- [ ] Attach any domain to an application/service → reachable via domain.
- [ ] Automatic Let's Encrypt HTTPS (HTTP-01).
- [ ] Wildcard certs via DNS-01 (Cloudflare + 1 other provider).
- [ ] Redirects work; cert status shown in the UI.

**Rollback:** Traefik config is generated from CP state — on error, just regenerate the config + restart Traefik (idempotent). Keep the previous config version 1 day in the DB for fast revert. Traefik itself runs as a `gotham-traefik` container on the node — upgrade by switching image tags.

**Phase gate:** continuous mode applies (see 00-roadmap.md) — after the exit criteria are met, continue to Phase 8 without stopping (Phase 5 runs in parallel on the same Phase 3 base).

---

## BE-6.1 — Traefik integration — `ws/p6-traefik`

- **Context brief:** Traefik 3.x runs on the node (1 instance/server, managed by the CP like a special container). The CP generates dynamic config from the apps/services/databases state (routers + services + middlewares). Pick ONE mechanism: **labels** (attached to containers at creation) or **file provider** (config volume written by the agent) — file provider recommended since it is easier to test and avoids touching the Phase 4 container-creation flow.
- **Deliverables:**
  - `internal/proxy/`: `ProxyConfig` model (routers, middlewares), YAML/TOML generator from state; sync when apps create/delete domains.
  - Bootstrap Traefik on the server (during server validation: create the Traefik container, mount the config dir + acme volume).
  - Agent RPC: write config file + reload Traefik (use the Traefik API ping).
  - Tests: generate config from sample state → snapshot comparison; reload without errors.
- **Verify:** attach a test domain (hosts file pointing local) to an Nginx app → curl through Traefik OK.
- **Depends on:** Phase 3. Parallel with BE-6.2 once the generator is done.

## BE-6.2 — SSL: Let's Encrypt HTTP-01 + DNS-01 — `ws/p6-ssl`

- **Context brief:** HTTP-01: Traefik handles it natively (just ensure ports 80/443 are open + acme config is correct). DNS-01: needs DNS provider credentials (Cloudflare, DigitalOcean...) stored in `secrets`, generating matching `certificatesResolvers`; wildcard `*.domain.com` issued once and reused for all subdomains.
- **Deliverables:**
  - Generator for the acme section (resolvers, storage, email), supporting Cloudflare + DigitalOcean (structured for easy additions). An optional `caServer` override (empty = Traefik's production default) makes the Let's Encrypt staging directory usable for iteration.
  - `dns_providers` table (secret refs), CRUD routes.
  - Tests: config-generation unit tests; e2e with a real domain + Cloudflare test zone (if available), otherwise verify with Let's Encrypt staging.
- **Verify:** a real domain gets a green cert (production or staging); a wildcard cert is issued successfully for 2 different subdomains.

### BE-6.2 acceptance — real DNS-01 issuance

The generator, the CRUD services and the bootstrap are covered by unit tests
and by the BE-6.1 e2e suite; the unmet acceptance step was a real certificate
obtained through a real DNS provider. It now runs as a gated test,
`TestP6DNS01Issuance` in `internal/e2e/p6_issuance_test.go`: it preflights the
Cloudflare token (create + delete a TXT record), creates the provider and the
DNS-01 certificate intent through the real service surface, bootstraps
Traefik with the DNS-01 resolver and asserts the certificate served for
`SNI=$GOTHAM_TEST_DOMAIN` on the local gateway chains to the selected CA. The
challenge TXT record is observed created and cleaned through the Cloudflare
API in the same run.

Prerequisites (owner-supplied, never committed):

- `$HOME/.config/gotham/cf-test.env` (mode 600) exporting `CF_DNS_API_TOKEN`
  (Cloudflare token with DNS edit on the test zone), `GOTHAM_TEST_DOMAIN`
  and `GOTHAM_TEST_ZONE`.
- Docker reachable on the default socket, dev Postgres up
  (`docker compose -f deploy/compose.dev.yml up -d`), ports 80/443/8080 free,
  and no pre-existing `gotham-traefik` container (the test refuses to remove
  one it cannot prove it owns). The test cleans up only rows, containers and
  DNS records it created.

Run:

```sh
set -a; . "$HOME/.config/gotham/cf-test.env"; set +a
export PATH=/usr/local/go/bin:$PATH
GOTHAM_E2E=1 GOTHAM_E2E_ACME_CA=staging go test ./internal/e2e/ \
  -run TestP6DNS01Issuance -count=1 -timeout 15m -v
# production burns one real, rate-limited issuance — explicit opt-in only:
GOTHAM_E2E=1 GOTHAM_E2E_ACME_CA=production go test ./internal/e2e/ \
  -run TestP6DNS01Issuance -count=1 -timeout 15m -v
```

`GOTHAM_E2E_ACME_CA` defaults to `staging`, so a plain `GOTHAM_E2E=1` run can
never consume production quota; a token without DNS edit permission stops the
test at the preflight (403, no blind retries).

Observed 2026-09-28 (local Docker Desktop, dev Postgres, ports 80/443/8080,
`gotham.deelux.dev` in `deelux.dev`):

- staging: pass in 56s; TXT `_acme-challenge.gotham.deelux.dev` observed
  created and cleaned through the Cloudflare API; Traefik served a chain
  `gotham.deelux.dev` → `(STAGING) Dastardly Durum YR1` →
  `(STAGING) Yonder Yam Root YR` for SNI=gotham.deelux.dev, and the
  certificate was present in `acme.json`.
- production: pass in 39s with `GOTHAM_E2E_ACME_CA=production` (one
  issuance); the served chain was `gotham.deelux.dev` → `YR2` →
  `Root YR` → `ISRG Root X1`.
- Scope: this run covers the exact-host DNS-01 path; the wildcard
  `tls.domains main/sans` variant is covered by generator goldens only and was
  not part of this live issuance.

- **Depends on:** BE-6.1.

## FE-6.1 — Domains UI — `ws/p6-domains-ui`

- **Context brief:** domain management inside the app detail: add domain, redirects (this domain → another domain), cert status (pending/issued/error + expiry date), DNS provider + wildcard config.
- **Deliverables:** `DomainEditor` component (embedded in app/service detail), DNS providers settings page.
- **Verify:** e2e: add a domain → wait for cert issued → browse via HTTPS.
- **Depends on:** BE-6.1 + BE-6.2.
