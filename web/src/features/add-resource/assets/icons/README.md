# Resource icons

Vendored service and database icons for the resource gallery. They are bundled
with the app and loaded on demand; nothing here is fetched from a CDN at
runtime.

- Source: [Coolify](https://github.com/coollabsio/coolify) `public/svgs` at
  commit `7b3511c73ff9f62863675c1f0ffd0c0f9a388e23` (Apache-2.0)
- Export used: the `coolify-catalog-20261119` catalog dump
- `services/<slug>.<ext>`: one icon per catalog service, named after the
  template slug (the merged Coolify app name)
- `databases/<engine>.<ext>`: one icon per database engine value
- `default.webp`: shown when a template key has no icon here

Later additions taken from the project's own site or GitHub organization,
scaled to 128px WebP: `services/mitmproxy.webp` (mitmproxy.org favicon) and
`services/gost.webp` (go-gost GitHub organization avatar). Templates whose
project has no usable logo (3proxy, Squid, Dante) keep the default icon.

The brand marks belong to their respective owners and are used only to
identify the service in the product UI.

## Size policy

The raw export is 8.7 MiB. Every raster icon (PNG, JPEG, WebP, ICO) is scaled
to at most 128px on its longest edge and re-encoded as WebP; an SVG that wraps
an embedded raster is unpacked first, and an oversized vector-only SVG is
rendered to a 128px WebP. Vector-only icons under 40 KB stay SVG. The folder
is about 1.2 MiB in total.

Keep new icons on the same budget: 128px WebP for rasters, plain SVG only when
it stays under 40 KB.
