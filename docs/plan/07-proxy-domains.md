# Phase 6 — Proxy, Domains & SSL (W8–W9) — parallel with Phase 5

**Goal:** automatic HTTP + HTTPS routing via Traefik, Coolify-style domain management.

**Exit criteria (Milestone M6):**
- [ ] Attach any domain to an application/service → reachable via domain.
- [ ] Automatic Let's Encrypt HTTPS (HTTP-01).
- [ ] Wildcard certs via DNS-01 (Cloudflare + 1 other provider).
- [ ] Redirects work; cert status shown in the UI.

**Rollback:** Traefik config is generated from CP state — on error, just regenerate the config + restart Traefik (idempotent). Keep the previous config version 1 day in the DB for fast revert. Traefik itself runs as a `gotham-traefik` container on the node — upgrade by switching image tags.

**Phase gate:** after the exit criteria are met, STOP and ask the project owner before continuing to the next phase (see Model policy & Phase gate in `00-roadmap.md`).

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
  - Generator for the acme section (resolvers, storage, email), supporting Cloudflare + DigitalOcean (structured for easy additions).
  - `dns_providers` table (secret refs), CRUD routes.
  - Tests: config-generation unit tests; e2e with a real domain + Cloudflare test zone (if available), otherwise verify with Let's Encrypt staging.
- **Verify:** a real domain gets a green cert (production or staging); a wildcard cert is issued successfully for 2 different subdomains.
- **Depends on:** BE-6.1.

## FE-6.1 — Domains UI — `ws/p6-domains-ui`

- **Context brief:** domain management inside the app detail: add domain, redirects (this domain → another domain), cert status (pending/issued/error + expiry date), DNS provider + wildcard config.
- **Deliverables:** `DomainEditor` component (embedded in app/service detail), DNS providers settings page.
- **Verify:** e2e: add a domain → wait for cert issued → browse via HTTPS.
- **Depends on:** BE-6.1 + BE-6.2.
