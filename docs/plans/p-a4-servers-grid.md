# P-A4 — Servers page: card grid instead of table (strict design port)

## Requirement
`/servers` renders as a GRID of node cards, matching `docs/design/servers.html`
exactly. Not a data table.

## Mockup structure (verified — docs/design/servers.html:49-120)
```html
<div class="grid cols-2" data-od-id="node-list">
  <article class="node-card" data-node-card data-tag="ready">
    .node-head   → avatar (initials) · name+meta (IP · OS · arch) · status tag
    dl.kv        → Docker version · Resources (vCPU/RAM/Disk) · Agent (version,
                   node_id/heartbeat) · SSH key + user
    .node-metrics → 3 tiles: CPU 5m / RAM / Disk — .val + .meter (kind: success|warn)
    .node-foot   → tags (containers, traefik, registry) + [Mở node] [Xác thực lại]
  </article> ×2 per row
</div>
```
CSS to port from `docs/design/assets/gotham-views.css` + `gotham-ui.css`:
`.grid.cols-2`, `.node-card`, `.node-head`, `.kv`, `.node-metrics`,
`.node-metric`, `.meter` (+ meter kind colors), responsive collapse to 1 col.

## Current code (verified)
- `web/src/pages/ServersPage.vue`: `NDataTable` (scroll-x 1500, pageSize 10,
  row-key), filter buttons + search above, `NCard` install-card below.
- Data source: servers Pinia store. Fields needed for the card: name, IP,
  OS/arch, status, docker version, resources, agent version + heartbeat,
  ssh key label + user, cpu/ram/disk percent (server metrics BE-8.4),
  container count. VERIFY the store exposes all of these; if a field is
  missing in the API response, extend the server response in the SAME PR
  (list the exact gaps in the PR description; no fabricated data in UI —
  render "—" when unknown).

## Changes
1. Rebuild the list region as a CSS grid of cards (raw HTML + scoped CSS,
   ported verbatim; keep Naive UI only for buttons/tooltips where the mockup
   uses .btn — port the mockup classes instead, tokens.css already carries
   the design tokens).
2. Keep existing behaviors: filter buttons (all/online/update), search,
   empty state, Add-server wizard entry, loading state (skeleton or spinner —
   follow the mockup's loading affordance if defined; otherwise a simple
   spinner).
3. Pagination: mockup has none — drop NDataTable pagination; the grid shows
   all filtered nodes (fleet sizes here are tens, not thousands; add
   `ponytail:` note if >50 nodes becomes real).
4. Update `web/e2e` specs that assert on table rows/cells (navigation,
   p8-misc-ui, services-regressions — grep for the old selectors first).

## Verification
- `npm run type-check`, `npm run build` (dist drift gate).
- Side-by-side screenshot vs `docs/design/servers.html`.
- UI e2e green (self-hosted → hosted runner).
- Metrics tiles show real values from heartbeats; offline node renders meters
  at last-known or "—" (decide: match current table behavior).

## PR
1 PR `fix/servers-grid` → Codex review → CI (UI E2E fires via web/**).