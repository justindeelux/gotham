# Phase 6 — Proxy, Domains & SSL (W8–W9) — parallel with Phase 5

**Goal:** automatic HTTP + HTTPS routing via Traefik with domain management.

**Exit criteria (Milestone M6):**
- Attach any domain to an application/service → reachable via domain.
- Automatic Let's Encrypt HTTPS (HTTP-01).
- Wildcard certs via DNS-01 (Cloudflare + 1 other provider).
- Redirects work; cert status shown in the UI.

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
obtained through a real DNS provider. It now runs as a doubly gated test,
`TestP6DNS01Issuance` in `internal/e2e/p6_issuance_test.go`: `GOTHAM_E2E=1`
(shared Docker-backed suite) plus `GOTHAM_E2E_DNS01=1` (explicit live-zone
opt-in). The shared CI E2E job sets only `GOTHAM_E2E=1`, so it skips this test
instead of failing; once the live opt-in is present, missing credentials fail
instead of skipping.

- The Cloudflare token is preflighted (create + delete a TXT record, fatal on
  403, delete retried and registered for cleanup), the provider and the
  DNS-01 certificate intent are created through the real service surface, and
  Traefik is bootstrapped with the DNS-01 resolver.
- Challenge-record ownership is proven, not inferred: every TXT record seen at
  the challenge name is only a candidate, and a record becomes owned only when
  its content equals the DNS-01 value recomputed from this run's own ACME
  account key and the challenge token of the authorization the CA reused
  after validation (RFC 8555 §7.1.4). Only owned records are awaited and
  deleted; a record that cannot be matched is left in place and the test
  reports failure.
- The certificate served for `SNI=$GOTHAM_TEST_DOMAIN` on the local gateway is
  verified with real signature validation (`x509.Verify`: signatures, validity
  and DNSName) against the system roots for production or the pinned Let's
  Encrypt staging roots for staging
  (`GOTHAM_E2E_STAGING_ROOT_PEM` can add a replacement staging anchor), and
  the leaf decoded from Traefik's `acme.json` must be byte-identical to the
  served leaf.
- Cleanup of everything owned fails the test when it fails; the proxy
  container is removed only when its per-run config-dir label proves
  ownership (a pre-existing `gotham-traefik` is always refused), and
  failure-path log dumps are redacted.

Offline regressions run under a plain `go test ./internal/e2e/`:
`TestP6IssuanceTXTValueOwnership` (no record is owned without a derived
value; a forged value never matches), `TestP6IssuanceChainVerificationRejectsForgery`
(a forged chain with the right issuer names is rejected by both the staging
anchors and the system roots) and `TestP6IssuanceACMENewOrder` (the signed
new-order request and the 200/201 order-reuse responses).

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
GOTHAM_E2E=1 GOTHAM_E2E_DNS01=1 GOTHAM_E2E_ACME_CA=staging go test ./internal/e2e/ \
  -run TestP6DNS01Issuance -count=1 -timeout 15m -v
# production burns one real, rate-limited issuance — explicit opt-in only:
GOTHAM_E2E=1 GOTHAM_E2E_DNS01=1 GOTHAM_E2E_ACME_CA=production go test ./internal/e2e/ \
  -run TestP6DNS01Issuance -count=1 -timeout 15m -v
```

`GOTHAM_E2E_ACME_CA` defaults to `staging`, so a live run without the CA
selection can never consume production quota; a token without DNS edit
permission stops the test at the preflight (403, no blind retries). The CI
E2E job (`.github/workflows/e2e.yml`, `GOTHAM_E2E=1` only) skips this test:
live issuance requires the owner zone and token, so the second flag is the
explicit request for it.

Observed 2026-09-28 (local Docker Desktop, dev Postgres, ports 80/443/8080,
`gotham.deelux.dev` in `deelux.dev`; the staging values below are the latest
run retained in the evidence directory):

- staging: pass in 67s; one candidate TXT record was observed at
  `_acme-challenge.gotham.deelux.dev` through the Cloudflare API and matched
  the value derived from this run's own ACME account (owned record), the
  served chain `gotham.deelux.dev` → `(STAGING) Dastardly Durum YR1` →
  `(STAGING) Yonder Yam Root YR` verified against the pinned staging roots,
  the `acme.json` leaf was byte-identical to the served leaf, and the ACME
  client removed the record (owned cleanup observed; no test deletion was
  needed). Earlier hardened runs passed the same way (70s, and a 59s run that
  drew the rotated `(STAGING) Ersatz Emmer YR2` intermediate); the staging
  intermediate is chosen by Let's Encrypt per order, and the pinned-root
  verification covers them.
- production: pass in 57s with `GOTHAM_E2E_ACME_CA=production` (the second
  and final production issuance for this package); the served chain
  `gotham.deelux.dev` → `YR1` → `Root YR` → `ISRG Root X1` verified with the
  system roots, the owned challenge record was observed created and cleaned,
  and the `acme.json` leaf matched the served leaf byte for byte.
- Scope: this run covers the exact-host DNS-01 path; the wildcard
  `tls.domains main/sans` variant is covered by generator goldens only and was
  not part of this live issuance.

- **Depends on:** BE-6.1.

## FE-6.1 — Domains UI — `ws/p6-domains-ui`

- **Context brief:** domain management inside the app detail: add domain, redirects (this domain → another domain), cert status (pending/issued/error + expiry date), DNS provider + wildcard config.
- **Deliverables:** `DomainEditor` component (embedded in app/service detail), DNS providers settings page.
- **Verify:** e2e: add a domain → wait for cert issued → browse via HTTPS.
- **Depends on:** BE-6.1 + BE-6.2.
- **Note:** the mockup's domain→domain redirects and certificate status/expiry have no backend in BE-6.1/BE-6.2; FE-6.1 ships them as explicit, non-fabricated "backend pending" stubs, and BE-6.3 below closes the gap (FE follow-up replaces the stubs).

## BE-6.3 — Domain redirects + certificate status/expiry — `ws/p6-redirects-status`

- **Context brief:** the two `domains.html` features the API cannot back today.
  (1) Domain→domain redirects: a rule that sends one host to another, generated
  as a Traefik `redirectRegex` middleware next to the existing routers and
  middlewares; the HTTP→HTTPS redirect stays a separate concern.
  (2) Certificate status/expiry: the CP has no issuance telemetry, so the status
  must come from the node's Traefik ACME storage (`acme.json`) read through the
  agent, reported per `domain_certificates` row as absent/present plus the
  certificate `notAfter`; when the node cannot be read the status is reported as
  unknown, never fabricated.
- **Deliverables:** forward-only migration for the redirect rule(s); store +
  service + `/v1/proxy` routes for redirect CRUD; generator support emitting the
  redirect middleware/router; an agent read path for the node's ACME storage and
  a CP service mapping it to per-domain status/expiry; tests (unit + a gated e2e
  on the existing lifecycle).
- **Design gate:** the redirect scope (per-application vs per-domain), the
  redirect code (301/302), the conflict/ownership rules for the redirect target,
  and the ACME-storage read boundary (agent RPC shape, what is redacted) MUST be
  proposed to the coordinator (`ask`) before inventing any state/schema. The
  proposed design was approved with four conditions — a key-material leak proof,
  the backend-less noop service assertion, no redirects for domain-disabled
  applications, and additive/omitempty status fields that never error the
  certificate list — all of which are covered below.
- **Verify:** attach a domain to an Nginx app with a redirect rule → Traefik
  answers the source host with the configured redirect to the target host; the
  certificate status/expiry reflect the node's ACME storage (absent stays absent,
  a real cert reports the served `notAfter`); restart/repair preserves the
  behavior; an unreachable node reports unknown rather than a fabricated value.
- **Depends on:** BE-6.1 + BE-6.2.

### Delivered design

**Redirects are per-application rules** (migration `00016_domain_redirects.sql`):
`domain_redirects(id, application_id FK ON DELETE CASCADE, source_domain,
target_domain, code, preserve_path, enabled, created_at, updated_at)`. Source and
target are exact hostnames, normalized and validated with the existing
`NormalizeDomain` + `ValidateDomain`; wildcard sources are out of scope for
BE-6.3 (a `HostRegexp` source is a follow-up, and the FE keeps that input
disabled). The source host is globally unique — disabling a rule pauses it
without releasing the host — and must not shadow **any** application's
`base_domain` (any node, including the owning application and migration-disabled
rows). The target must not equal another enabled rule's source **and** the
source must not equal another enabled rule's target: the no-chain guard is
two-directional, so neither insertion order (create, update or enable) can
complete a chain, and it reads every enabled rule in the database rather than
only the rule's node, so a chain across nodes is rejected too. Source and
target must differ. Write-time guards answer 409/400. The guard is a
check-then-write, not a database constraint: concurrent racing writes are
caught by the generator, which holds the later-created rule of a chain back
(global candidate set, not node-local) and reports it as a per-application
diagnostic. The generator also holds back duplicate sources, sources shadowing
a routed host, domain-disabled owners and invalid codes instead of freezing the
node. Deleting an application cascades its rules; a domain-disabled application
does not emit its redirects and cannot own a rule, because its route is already
excluded by the uniqueness conflict.

**Generation** emits one `redirectRegex` middleware and one web-entrypoint
router per enabled rule: rule ``Host(`source`)``, middleware
`(?i)^http://source\.?(?::[^/]*)?/(.*) → https://target/${1}` when the path is
preserved (path and query kept) or `.../.* → https://target/` when it is not.
The host part of the regex deliberately accepts every form Traefik's
`Host(source)` router matches while the raw URL retains it — any case, one
fully-qualified trailing dot, and any port form (the router strips ports with
`net.SplitHostPort`, which accepts non-numeric and empty ports) — so the
middleware always terminates for the requests the router sends it and can never
fall through to the backend-less service. Every redirect router references one
shared `gotham-redirect-noop` service with `servers: []`: Traefik v3 rejects a
router without a service, and the `redirectRegex` middleware terminates the
request before a backend is consulted, so the service is never a proxy target
(asserted by golden/unit tests and exercised per host form in the gated e2e).
The router never carries the shared `gotham-https-redirect` middleware; the
HTTP→HTTPS redirect stays a separate concern, and the target scheme is always
`https`.

**Codes:** Traefik's `redirectRegex` only distinguishes permanent from
temporary, and it special-cases only `GET`: the `301` intent answers `301` to
GET while HEAD and every other method answer `308`; the `302` intent answers
`302` to GET while HEAD and every other method answer `307` (verified against
`traefik:v3.7`, including a request-level e2e assertion). The API accepts the
two intents `301` and `302` and maps them to `permanent`. The mockup's four
options are these two intents plus their method-dependent on-the-wire codes.

**Certificate status is computed on read, never stored.** The additive agent RPC
`ProxyService.ReadACMEStorage` returns one entry per stored certificate:
`{resolver, main, sans, not_after}`. The agent reads `<acmeDir>/acme.json`
through the same no-follow traversal as the write path, caps the file size and
entry count, treats a missing or empty file as `present=false`, and parses it
with decode structs that structurally omit the per-certificate `key` and the
resolver `Account` (ACME account key): key material never enters a Go value,
the response, a log line or an error string. Before Traefik can ever start, the
agent also prepares the storage file itself (create-only, mode `0600`, never
truncating) while it writes the generated configuration: Traefik writes the
file with `os.WriteFile` and keeps an existing file's mode and ownership, so
the agent-owned placeholder stays readable after Traefik stores certificates in
it. Without it, a root-run Traefik would create a root-only `0600` file that the
unprivileged agent cannot read — a genuine read failure, reported as `unknown`
(never fabricated); a legacy file the agent cannot read stays `unknown` until
an operator rotates it (the placeholder only fixes nodes whose ACME directory
the agent owns). The CP status service joins each certificate intent to its
application's node, reads each node once (10s bound), and maps the intent's
recorded domain to `present` (latest covering `notAfter`; the one-label
wildcard rule applies to the stored primary name and SANs, with apex and
multi-label names rejected), `absent` (the node answered and nothing covers the
domain), or `unknown` (unreachable node, RPC/read/parse failure, unassigned
application). It never returns an error: node state degrades to `unknown` and
the certificate list still answers 200. `GET /v1/proxy/certificates` and
`.../{id}` gain additive, `omitempty` `status` and `not_after` fields;
`not_after` appears only for `present`.

**Routes:** `POST/GET/GET{id}/PATCH/DELETE /v1/proxy/redirects`, admin-scoped
and mounted with the SSL surface; `GET` accepts an optional `application_id`
filter.

**Verification evidence**

- Unit: generator goldens (YAML + TOML), the no-backends /
  terminating-middleware assertions and the regex host-form matrix (port,
  empty/non-numeric port, trailing dot, case; with non-matching neighbours)
  (`internal/proxy/redirect_generate_test.go`); CRUD validation, both no-chain
  directions across create/update/enable and the later-rule hold-back filter
  (`redirects_test.go`); status mapping including wildcard main and SANs,
  apex/multi-label rejection and unreachable nodes
  (`certificate_status_test.go`); HTTP surface, strict decoding and the additive
  certificate fields (`redirect_routes_test.go`); metadata-only ACME read
  against a fixture file that really contains private and account keys — no key
  material in the RPC response or the logs — plus the agent-owned storage
  placeholder and the permission-denied-is-not-absent classification
  (`agent/proxy_acme_test.go`).
- Persistence: migration + store round trip, unique-source/code/self-redirect
  constraints, application join, ownership-guard queries and the delete cascade
  (`internal/store/redirects_integration_test.go`, runs against dev Postgres).
- Gated e2e (`GOTHAM_E2E=1`, Docker + dev Postgres, ports 80/443 free):
  `TestP6RedirectsAndCertificateStatus` in `internal/e2e/p6_redirects_test.go` —
  a real store-backed rule answers `HTTP 301` for its source host through the
  bootstrapped Traefik with path+query preserved **for the bare, ported,
  dot-suffixed and uppercase host forms**, `HEAD`/`POST` answer `308` for the
  permanent intent and `307` for the temporary one, pausing removes the route
  and re-enabling restores it, a proxy restart keeps serving it, and certificate
  status walks empty → absent, unreadable → unknown, missing → absent,
  fixture → present (with the stored `notAfter`), malformed → unknown, with an
  unreachable node reporting unknown in the same call. The `acme.json` is a
  fixture, so no ACME issuance and no production/staging quota is consumed; the
  fixture's key material is asserted absent from the RPC response.

Run:

```sh
export PATH=/usr/local/go/bin:$PATH
GOTHAM_E2E=1 go test ./internal/e2e/ -run TestP6RedirectsAndCertificateStatus -count=1 -timeout 15m -v
```

**FE follow-up (replaces the FE-6.1 "backend pending" stubs).** The redirect
list/add form binds to `/v1/proxy/redirects` (per application, `enabled` toggle,
source/target, code 301/302 and path preservation); the certificate rows show
the `status`/`not_after` fields now returned by the certificate API, rendering
unknown explicitly and never a fabricated date. The mockup's 307/308 options
collapse onto the 302/301 intents (the on-the-wire code depends on the method),
and wildcard source hosts stay disabled until `HostRegexp` support lands. No
`web/` change was made in this work package.
