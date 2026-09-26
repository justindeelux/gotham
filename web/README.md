# Gotham Web UI

Vue 3 + Vite + TypeScript single-page application for the Gotham control plane.
Vue Router handles routing, Pinia holds UI state, axios talks to the
`/api/v1` control-plane API, and Naive UI provides the layout shell.

## Requirements

- Node 20+ and npm 10+ (CI uses Node 20).

## Development

```sh
cd web
npm install
npm run dev          # Vite dev server on http://localhost:5173
```

The Go control plane can proxy the SPA to the Vite dev server for hot module
replacement. Run the Vite dev server first, then start the server with
`WEB_DEV=1`:

```sh
WEB_DEV=1 go run ./cmd/gotham serve
```

In dev mode every non-API request is reverse-proxied to
`http://localhost:5173`; override the target with `WEB_DEV_URL` if the dev
server runs elsewhere.

## Build

```sh
cd web
npm run build         # writes the compiled SPA to ../internal/server/webdist
npm run type-check    # vue-tsc --noEmit
npm run preview       # serve the production build locally
```

## Committed build output

Vite is configured with `build.outDir = ../internal/server/webdist`, and that
directory is **committed to git**. `internal/server/embed.go` embeds it with
`//go:embed all:webdist`, so `go build` produces a binary with the UI included
and does not require Node.

Because the output is committed, any change to the SPA must be accompanied by a
fresh build in the same commit. CI enforces this: the `web` job runs
`npm run build` and then fails if `git diff --exit-code -- internal/server/webdist`
is not clean.

`make build` refreshes the SPA automatically when `web/node_modules` exists,
and otherwise builds the Go binary against the committed dist.
