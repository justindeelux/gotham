// JUS-9/JUS-10 UI-truth checks for the pure helpers behind the copy and
// data-state fixes.
//
// The repo has no web unit-test runner (no vitest/jsdom), so this script
// bundles the pure helpers with the project's own esbuild and asserts their
// behaviour directly, in the same style as mock-ws-check.mjs.
//
// Run from web/:  node scripts/ui-truth-check.mjs

import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { build } from "esbuild";

// Web source root for the "@" import alias (mirrors vite.config.ts).
const srcDir = new URL("../src", import.meta.url).pathname;

// Vue SFCs are re-exported through feature indexes, so a bundled helper can
// pull one into the module graph. The Node harnesses never execute them;
// stub the default export instead of teaching esbuild to compile SFCs.
const vueStubPlugin = {
  name: "stub-vue-sfc",
  setup(vueBuild) {
    vueBuild.onResolve({ filter: /\.vue$/ }, (args) => ({
      path: args.path,
      namespace: "vue-stub",
    }));
    vueBuild.onLoad({ filter: /.*/, namespace: "vue-stub" }, () => ({
      contents: "export default {};",
      loader: "js",
    }));
  },
};

// ── tiny assertion harness ───────────────────────────────────────────────
const results = [];

async function check(name, fn) {
  try {
    await fn();
    results.push({ name, ok: true });
    console.log(`  ✓ ${name}`);
  } catch (error) {
    results.push({ name, ok: false, error });
    console.error(`  ✗ ${name}\n      ${error.message}`);
  }
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message ?? "assertion failed");
  }
}

// ── load a TypeScript module through esbuild ─────────────────────────────
// The API modules import the axios-backed ./http layer, which the pure
// helpers under test never touch; stub it so the bundle stays DOM-free.
async function loadModule(relativePath) {
  const directory = await mkdtemp(join(tmpdir(), "gotham-ui-truth-"));
  const outfile = join(directory, "module.mjs");
  await build({
    entryPoints: [new URL(relativePath, import.meta.url).pathname],
    outfile,
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node20",
    logLevel: "silent",
    alias: { "@": srcDir },
    plugins: [
      vueStubPlugin,
      {
        name: "stub-http-layer",
        setup(httpBuild) {
          httpBuild.onResolve({ filter: /(^|\/)(\.\/)?http$/ }, () => ({
            path: "stub-http-layer",
            namespace: "stub-http",
          }));
          httpBuild.onLoad({ filter: /.*/, namespace: "stub-http" }, () => ({
            contents: [
              "export const http = { get: async () => ({}), post: async () => ({}),",
              "  patch: async () => ({}), put: async () => ({}), delete: async () => ({}) };",
              "export function teamHeaders() { return {}; }",
            ].join("\n"),
            loader: "js",
          }));
        },
      },
    ],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

// ── load an API module with catalogs registered ──────────────────────────
// Node bundles never run initI18n (no import.meta.glob under esbuild), so a
// describer with localized summaries would otherwise resolve against empty
// catalogs. The entry registers the same synchronous catalogs the app merges
// at startup, keeping assertions on real display text.
async function loadApiWithCatalogs(apiImport, namespace, catalogImport) {
  const { writeFile } = await import("node:fs/promises");
  const directory = await mkdtemp(join(tmpdir(), "gotham-ui-truth-"));
  const entry = join(directory, "entry.ts");
  await writeFile(
    entry,
    [
      `import { i18n } from "@/shared/i18n";`,
      `import common from "@/shared/i18n/locales/en";`,
      `import feature from "${catalogImport}";`,
      `i18n.global.mergeLocaleMessage("en", common);`,
      `i18n.global.mergeLocaleMessage("en", { ${namespace}: feature });`,
      `export * from "${apiImport}";`,
    ].join("\n"),
  );
  const outfile = join(directory, "module.mjs");
  await build({
    entryPoints: [entry],
    outfile,
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node20",
    logLevel: "silent",
    alias: { "@": srcDir },
    plugins: [
      vueStubPlugin,
      {
        name: "stub-http-layer",
        setup(httpBuild) {
          httpBuild.onResolve({ filter: /(^|\/)(\.\/)?http$/ }, () => ({
            path: "stub-http-layer",
            namespace: "stub-http",
          }));
          httpBuild.onLoad({ filter: /.*/, namespace: "stub-http" }, () => ({
            contents: [
              "export const http = { get: async () => ({}), post: async () => ({}),",
              "  patch: async () => ({}), put: async () => ({}), delete: async () => ({}) };",
              "export function teamHeaders() { return {}; }",
            ].join("\n"),
            loader: "js",
          }));
        },
      },
    ],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

/** sleep pauses the harness without pulling in a test runner. */
function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// ── load applications.ts with a controllable http layer ───────────────────
async function loadApplicationsWithHttp() {
  const directory = await mkdtemp(join(tmpdir(), "gotham-ui-truth-"));
  const outfile = join(directory, "module.mjs");
  await build({
    entryPoints: [new URL("../src/features/applications/api/applications.ts", import.meta.url).pathname],
    outfile,
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node20",
    logLevel: "silent",
    alias: { "@": srcDir },
    plugins: [
      vueStubPlugin,
      {
        name: "controllable-http-layer",
        setup(httpBuild) {
          httpBuild.onResolve({ filter: /(^|\/)(\.\/)?http$/ }, () => ({
            path: "controllable-http-layer",
            namespace: "ctrl-http",
          }));
          httpBuild.onLoad({ filter: /.*/, namespace: "ctrl-http" }, () => ({
            contents: [
              "export const http = {",
              "  get: (...a) => globalThis.__gothamHttp.get(...a),",
              "  post: (...a) => globalThis.__gothamHttp.post(...a),",
              "  patch: (...a) => globalThis.__gothamHttp.patch(...a),",
              "  put: (...a) => globalThis.__gothamHttp.put(...a),",
              "  delete: (...a) => globalThis.__gothamHttp.delete(...a),",
              "};",
              "export function teamHeaders() { return {}; }",
            ].join("\n"),
            loader: "js",
          }));
        },
      },
    ],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

// ── load a real Pinia store with stubbed API imports ───────────────────────
// apiStubs maps an import specifier (as written in the store source, e.g.
// "@/features/teams/api/teams") to the stub module contents. Pinia and Vue
// are bundled for real, so setActivePinia reaches the instance under test.
async function loadStoreHarness(storeDir, storeFile, storeExport, apiStubs) {
  const directory = await mkdtemp(join(tmpdir(), "gotham-store-check-"));
  const outfile = join(directory, "store.mjs");
  await build({
    stdin: {
      contents: [
        'import { createPinia, setActivePinia } from "pinia";',
        `import { ${storeExport} } from "./${storeFile}";`,
        `export { createPinia, setActivePinia, ${storeExport} };`,
      ].join("\n"),
      resolveDir: new URL(storeDir, import.meta.url).pathname,
      loader: "ts",
    },
    outfile,
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node20",
    logLevel: "silent",
    alias: { "@": srcDir },
    plugins: [
      vueStubPlugin,
      {
        name: "stub-store-apis",
        setup(stubBuild) {
          stubBuild.onResolve({ filter: /.*/ }, (args) => {
            if (Object.hasOwn(apiStubs, args.path)) {
              return { path: args.path, namespace: "store-stub" };
            }
            return null;
          });
          stubBuild.onLoad({ filter: /.*/, namespace: "store-stub" }, (args) => ({
            contents: apiStubs[args.path],
            loader: "js",
          }));
        },
      },
    ],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

// ── load the real auth store with stubbed HTTP, token and user stores ─────
async function loadAuthHarness() {
  const directory = await mkdtemp(join(tmpdir(), "gotham-auth-check-"));
  const outfile = join(directory, "store.mjs");
  const apiStubs = {
    "@/shared/api/http": [
      "export const http = {",
      "  get: async () => { throw new Error('unused'); },",
      "  post: (...a) => globalThis.__authHttp.post(...a),",
      "  patch: async () => { throw new Error('unused'); },",
      "  put: async () => { throw new Error('unused'); },",
      "  delete: async () => { throw new Error('unused'); },",
      "};",
      "export function teamHeaders() { return {}; }",
    ].join("\n"),
    "@/shared/api/token": [
      "export const getSession = () => ({",
      "  user: null, accessToken: null, refreshToken: null,",
      "});",
      "export const setSession = () => {};",
      "export const clearSession = () => {};",
      "export const subscribeSession = () => {};",
    ].join("\n"),
    "@/features/servers": [
      "export function stripErrorPrefix(m) { return m; }",
      "export function isApiError() { return false; }",
      "export const useServersStore = () => globalThis.__userStores.servers;",
    ].join("\n"),
    "@/features/teams": [
      "export const useTeamsStore = () => globalThis.__userStores.teams;",
    ].join("\n"),
    "@/features/applications": [
      "export const useApplicationsStore = () => globalThis.__userStores.applications;",
      "export const useProvidersStore = () => globalThis.__userStores.providers;",
      "export const useGitHubAppStore = () => globalThis.__userStores['github-app'];",
    ].join("\n"),
    "@/features/databases": [
      "export const useDatabasesStore = () => globalThis.__userStores.databases;",
      "export const useBackupsStore = () => globalThis.__userStores.backups;",
    ].join("\n"),
    "@/features/notifications": [
      "export const useNotificationsStore = () => globalThis.__userStores.notifications;",
    ].join("\n"),
    "@/features/projects": [
      "export const useProjectsStore = () => globalThis.__userStores.projects;",
    ].join("\n"),
    "@/features/services": [
      "export const useServicesStore = () => globalThis.__userStores.services;",
    ].join("\n"),
    "@/features/domains": [
      "export const useProxyStore = () => globalThis.__userStores.proxy;",
    ].join("\n"),
    "@/features/templates": [
      "export const useTemplatesStore = () => globalThis.__userStores.templates;",
    ].join("\n"),
  };
  await build({
    stdin: {
      contents: [
        'import { createPinia, setActivePinia } from "pinia";',
        'import { useAuthStore } from "./auth";',
        "export { createPinia, setActivePinia, useAuthStore };",
      ].join("\n"),
      resolveDir: new URL("../src/features/auth/stores", import.meta.url).pathname,
      loader: "ts",
    },
    outfile,
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node20",
    logLevel: "silent",
    alias: { "@": srcDir },
    plugins: [
      vueStubPlugin,
      {
        name: "stub-auth-deps",
        setup(stubBuild) {
          stubBuild.onResolve({ filter: /.*/ }, (args) => {
            if (Object.hasOwn(apiStubs, args.path)) {
              return { path: args.path, namespace: "auth-stub" };
            }
            return null;
          });
          stubBuild.onLoad({ filter: /.*/, namespace: "auth-stub" }, (args) => ({
            contents: apiStubs[args.path],
            loader: "js",
          }));
        },
      },
    ],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

// ── load the real http layer (axios + token bundled for real) ─────────────
// loadModule's http stub would also rewrite axios's own adapters/http.js, so
// the forced-logout test bundles without plugins instead.
async function loadHttpReal() {
  const webDir = new URL("..", import.meta.url).pathname;
  const directory = await mkdtemp(join(webDir, ".tmp-http-check-"));
  const outfile = join(directory, "module.mjs");
  await build({
    entryPoints: [new URL("../src/shared/api/http.ts", import.meta.url).pathname],
    outfile,
    bundle: true,
    packages: "external",
    format: "esm",
    platform: "node",
    target: "node20",
    logLevel: "silent",
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

async function main() {
  const format = await loadModule("../src/shared/utils/format.ts");
  const servers = await loadModule("../src/features/servers/api/servers.ts");
  const teams = await loadModule("../src/features/teams/api/teams.ts");
  const databases = await loadModule("../src/features/databases/api/databases.ts");
  const applications = await loadModule("../src/features/applications/api/applications.ts");
  const dashboard = await loadModule("../src/features/dashboard/utils/dashboard.ts");

  console.log("shared usage thresholds (JUS-10)");
  await check("warn at 60 and danger at 80", () => {
    const { USAGE_WARN_PERCENT, USAGE_DANGER_PERCENT, usageLevel } =
      format.module;
    assert(USAGE_WARN_PERCENT === 60, "warn threshold is 60");
    assert(USAGE_DANGER_PERCENT === 80, "danger threshold is 80");
    assert(usageLevel(0) === "ok", "0 is ok");
    assert(usageLevel(49) === "ok", "49 is ok (the reported disk case)");
    assert(usageLevel(59) === "ok", "59 is ok");
    assert(usageLevel(60) === "warn", "60 is warn");
    assert(usageLevel(79) === "warn", "79 is warn");
    assert(usageLevel(80) === "danger", "80 is danger");
    assert(usageLevel(100) === "danger", "100 is danger");
  });

  console.log("shared usage view boundaries (JUS-10 fix round 1)");
  await check("NaN renders as a dash, out-of-range readings clamp", () => {
    const { usageView, usageBarColor } = format.module;
    assert(
      usageBarColor("ok", "var(--accent)") === "var(--accent)",
      "healthy keeps the metric base hue",
    );
    assert(
      usageBarColor("warn", "var(--accent)") === "var(--warn)",
      "warn is amber everywhere",
    );
    assert(
      usageBarColor("danger", "var(--accent)") === "var(--danger)",
      "danger is red everywhere",
    );
    const missing = usageView(NaN, "var(--accent)");
    assert(missing.label === "—", "NaN labels as a dash, not 0%");
    assert(missing.percentage === 0, "NaN bars at zero");
    const nullish = usageView(null, "var(--success)");
    assert(nullish.label === "—", "null labels as a dash");
    const negative = usageView(-3, "var(--success)");
    assert(negative.percentage === 0, "negative clamps to 0");
    assert(negative.label === "0%", "negative labels 0%");
    const over = usageView(140, "var(--success)");
    assert(over.percentage === 100, ">100 clamps to 100");
    assert(over.color === "var(--danger)", ">100 renders danger red");
    const healthy = usageView(0.49, "var(--accent)");
    assert(healthy.label === "49%", "fraction scales to percent");
    assert(healthy.color === "var(--accent)", "healthy keeps the base hue");
    const warn = usageView(72, "var(--success)");
    assert(warn.color === "var(--warn)", "72 renders amber");
  });

  console.log("server error prefix stripping (JUS-9)");
  await check("internal package prefixes are stripped, the rest kept", () => {
    const { describeServerError, stripErrorPrefix } = servers.module;
    assert(
      stripErrorPrefix("servers: validation failed: ssh dial timeout") ===
        "validation failed: ssh dial timeout",
      "servers prefix stripped",
    );
    assert(
      stripErrorPrefix("databases: connection refused") === "connection refused",
      "databases prefix stripped",
    );
    assert(
      stripErrorPrefix("host:port unreachable") === "host:port unreachable",
      "unknown prefixes are not package names",
    );
    assert(
      stripErrorPrefix("validation failed: bad input") ===
        "validation failed: bad input",
      "prefix-free messages pass through",
    );
    assert(
      describeServerError({
        message: "servers: validation failed: ssh dial timeout",
        status: 422,
      }) === "validation failed: ssh dial timeout",
      "ApiError path strips the prefix",
    );
    assert(
      describeServerError(new Error("proxy: bad gateway")) === "bad gateway",
      "Error path strips the prefix",
    );
  });

  console.log("sidebar role label (JUS-10)");
  await check("role wording matches the Teams page, neutral while loading", () => {
    const { meRoleLabel } = teams.module;
    assert(meRoleLabel(null) === "Team member", "loading fallback is neutral");
    assert(meRoleLabel("owner") === "owner", "owner label");
    assert(meRoleLabel("admin") === "admin", "admin label");
    assert(meRoleLabel("read_only") === "read-only", "read-only label");
  });

  console.log("databases empty states (JUS-9)");
  await check("none-yet versus filtered-empty copy", () => {
    const { databaseEmptyDescription, databaseEmptyHint } = databases.module;
    assert(
      databaseEmptyDescription(0) === "No databases yet",
      "zero databases says none yet",
    );
    assert(
      databaseEmptyDescription(3) === "No databases match this filter",
      "existing databases say filtered",
    );
    assert(
      databaseEmptyHint(0).includes("first"),
      "none-yet hint points at creation",
    );
    assert(
      databaseEmptyHint(3).includes("filter"),
      "filtered hint points at the filter",
    );
  });

  console.log("dashboard running count (JUS-10)");
  await check("only running latest deployments count", () => {
    const { countRunning } = applications.module;
    assert(countRunning([]) === 0, "no applications");
    assert(
      countRunning(["running", "failed", null, undefined]) === 1,
      "one running of four",
    );
    assert(
      countRunning(["running", "running"]) === 2,
      "all running",
    );
    assert(
      countRunning(["queued", "building"]) === 0,
      "in-flight deploys are not running",
    );
  });

  console.log("dashboard tile states (JUS-10 fix round 3)");
  await check("loading / error / empty / ready and boundaries", () => {
    const { applicationTileView, incompleteTileHint } = dashboard.module;
    const base = {
      loading: false,
      error: null,
      total: 2,
      running: 1,
      failedReads: 0,
    };
    assert(
      applicationTileView({ ...base, loading: true }).state === "loading",
      "loading shows the skeleton",
    );
    assert(
      applicationTileView({ ...base, loading: true, error: "x", failedReads: 1 })
        .state === "loading",
      "loading beats a set error",
    );
    const error = applicationTileView({ ...base, error: "Could not load" });
    assert(error.state === "error", "error branch renders, never false-empty");
    assert(error.error === "Could not load", "error text passes through");
    assert(
      applicationTileView({ ...base, total: 0 }).state === "empty",
      "zero applications is genuinely empty",
    );
    assert(
      applicationTileView({ ...base, total: 0, error: "x" }).state === "error",
      "error beats empty (no false none-yet)",
    );
    const ready = applicationTileView(base);
    assert(
      ready.state === "ready" &&
        ready.countText === "1/2" &&
        ready.hint === "",
      "complete tile reads 1/2 with no hint",
    );
    assert(
      applicationTileView({ ...base, failedReads: 1 }).countText === "≥1/2",
      "one failed read marks the figure at least",
    );
    const incomplete = applicationTileView({ ...base, failedReads: 1 });
    assert(
      incomplete.state === "ready" && incomplete.hint === incompleteTileHint,
      "incomplete tile carries the caveat hint",
    );
    const allFailed = applicationTileView({
      ...base,
      total: 3,
      running: 0,
      failedReads: 3,
    });
    assert(
      allFailed.countText === "≥0/3" && allFailed.hint === incompleteTileHint,
      "zero running with failed reads is at least 0/3, not 0/3",
    );
    assert(
      incompleteTileHint === "Some states could not be read",
      "hint wording is fixed",
    );
  });

  await check("tile input mapping passes the live page values through", async () => {
    const { buildApplicationTileInput } = dashboard.module;
    const input = buildApplicationTileInput({
      loading: false,
      error: "Could not load",
      total: 2,
      running: 1,
      failedReads: 1,
    });
    assert(input.error === "Could not load", "error text is not nulled");
    assert(input.failedReads === 1, "failed reads are not zeroed");
    assert(
      input.loading === false && input.total === 2 && input.running === 1,
      "loading/total/running pass through",
    );
    const idle = buildApplicationTileInput({
      loading: true,
      error: null,
      total: 0,
      running: 0,
      failedReads: 0,
    });
    assert(
      idle.loading === true && idle.error === null,
      "loading/null-error states stay distinct",
    );
    const { readFile } = await import("node:fs/promises");
    const page = await readFile(
      new URL("../src/features/dashboard/pages/DashboardPage.vue", import.meta.url),
      "utf8",
    );
    const callAt = page.indexOf("buildApplicationTileInput({");
    assert(callAt !== -1, "page maps through buildApplicationTileInput");
    for (const live of [
      "applicationsLoading.value",
      "applicationsError.value",
      "applicationTotal.value",
      "applicationRunning.value",
      "applicationFailedReads.value",
    ]) {
      assert(page.includes(live), `page passes live ${live}`);
    }
    assert(
      !page.includes("error: null") && !page.includes("failedReads: 0"),
      "page hardcodes no null error / zero reads",
    );
  });

  await check("tile template renders the tested output only", async () => {
    const { readFile } = await import("node:fs/promises");
    const source = await readFile(
      new URL("../src/features/dashboard/pages/DashboardPage.vue", import.meta.url),
      "utf8",
    );
    const start = source.indexOf("dashboard.kpi.applications");
    assert(start !== -1, "tile card exists");
    const end = source.indexOf("dashboard.kpi.deploys", start);
    assert(end !== -1, "tile card block ends");
    const block = source.slice(start, end);
    // Every tile branch directive is exactly a tile.state comparison: an
    // `&& false` (or any extra condition) fails here.
    const directives = [...block.matchAll(/\bv-(?:if|else-if)="([^"]*)"/g)].map(
      (match) => match[1],
    );
    assert(
      directives.length === 4,
      `three tile branches plus the hint (saw ${directives.length})`,
    );
    assert(
      directives[0] === "tile.state === 'loading'" &&
        directives[1] === "tile.state === 'error'" &&
        directives[2] === "tile.state === 'ready'" &&
        directives[3] === "tile.hint",
      `branch directives are exactly the tile state plus hint (saw ${directives})`,
    );
    assert(block.includes("{{ tile.countText }}"), "figure binds countText");
    assert(block.includes("{{ tile.hint }}"), "caveat binds hint");
    assert(block.includes("{{ tile.error }}"), "error branch binds tile.error");
    // The figure and marker come only from the tested function: no
    // duplicated formatting, no page-level refs, no marker literal.
    for (const stale of [
      "tile.running",
      "tile.total",
      "tile.prefix",
      "tile.incomplete",
      "applicationsLoading",
      "applicationsError",
      "applicationTotal",
      "applicationRunning",
      "applicationFailedReads",
      "applicationsIncomplete",
      "≥",
    ]) {
      assert(!block.includes(stale), `tile block references no ${stale}`);
    }
  });

  await check("sidebar role retry is bounded (JUS-10 fix round 3)", async () => {
    const { shouldRetryRoleRead, roleReadMaxRetries, roleReadRetryMs } =
      teams.module;
    assert(roleReadMaxRetries === 2, "two retries after the initial read");
    assert(roleReadRetryMs === 5_000, "retries are 5s apart");
    assert(
      shouldRetryRoleRead({ loaded: false, loading: false, retries: 0 }) === true,
      "first failure schedules a retry",
    );
    assert(
      shouldRetryRoleRead({ loaded: false, loading: false, retries: 1 }) === true,
      "second failure schedules the last retry",
    );
    assert(
      shouldRetryRoleRead({ loaded: false, loading: false, retries: 2 }) === false,
      "exhausted attempts stop",
    );
    assert(
      shouldRetryRoleRead({ loaded: true, loading: false, retries: 0 }) === false,
      "loaded stops (even an empty list: disabled feature stays neutral)",
    );
    assert(
      shouldRetryRoleRead({ loaded: false, loading: true, retries: 0 }) === false,
      "an in-flight fetch stops (the label follows the store)",
    );
    const { readFile } = await import("node:fs/promises");
    const card = await readFile(
      new URL("../src/app/layouts/AccountMenu.vue", import.meta.url),
      "utf8",
    );
    assert(card.includes("shouldRetryRoleRead("), "AccountMenu decides via the helper");
    assert(
      card.includes("roleReadRetryMs") && !card.includes("5_000"),
      "AccountMenu takes the delay from the policy, not a local literal",
    );
    assert(
      !card.includes("maxRoleRetries") && !card.includes("roleRetryMs"),
      "AccountMenu keeps no local retry constants",
    );
    assert(
      card.includes("onUnmounted(cancelRoleRetry)"),
      "unmount drops a pending retry",
    );
  });

  console.log("tightened error prefix allowlist (fix round 1)");
  await check("only real Go prefixes strip; ordinary words pass through", () => {
    const { stripErrorPrefix } = servers.module;
    assert(
      stripErrorPrefix("config: key X missing") === "config: key X missing",
      "config is not a backend prefix and stays",
    );
    assert(
      stripErrorPrefix("clientip: 1.2.3.4") === "clientip: 1.2.3.4",
      "clientip is not a backend prefix and stays",
    );
    assert(
      stripErrorPrefix("deploy: list env config: boom") === "list env config: boom",
      "deploy strips exactly one level",
    );
    assert(
      stripErrorPrefix("ws: hub closed") === "hub closed",
      "ws strips",
    );
    assert(
      stripErrorPrefix("cleanup: retention failed") === "cleanup: retention failed",
      "cleanup is test-only and stays (round 2)",
    );
    assert(
      stripErrorPrefix("Update: available") === "Update: available",
      "sentence-case words are not prefixes",
    );
    assert(
      stripErrorPrefix("  Servers: validation failed  ") === "validation failed",
      "match is case-insensitive and trims",
    );
  });

  await check("allowlist regenerated from the Go sources live", async () => {
    const { execFileSync } = await import("node:child_process");
    const { readFile } = await import("node:fs/promises");
    // The documented regeneration grep over non-test Go sources: a new Go
    // prefix changes this set and fails the comparison below.
    const repoRoot = new URL("../../", import.meta.url).pathname;
    const grepped = execFileSync(
      "/bin/sh",
      [
        "-c",
        "grep -rhoE '(errors\\.New|Errorf)\\(\"[a-z][0-9a-z_-]*:' internal" +
          " --include='*.go' --exclude='*_test.go'" +
          ' | grep -oE \'"[a-z][0-9a-z_-]*\' | tr -d \'"\' | sort -u',
      ],
      { cwd: repoRoot, encoding: "utf8" },
    )
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => line.length > 0);
    const source = await readFile(
      new URL("../src/features/servers/api/servers.ts", import.meta.url),
      "utf8",
    );
    const setStart = source.indexOf("new Set([");
    assert(setStart !== -1, "allowlist set literal exists");
    const setBody = source.slice(setStart, source.indexOf("])", setStart));
    const allowlist = [...setBody.matchAll(/"([^"]+)"/g)].map(
      (match) => match[1],
    );
    assert(
      allowlist.length === grepped.length &&
        allowlist.every((prefix) => grepped.includes(prefix)) &&
        grepped.every((prefix) => allowlist.includes(prefix)),
      `allowlist [${allowlist}] equals the Go sources [${grepped}]`,
    );
    const { stripErrorPrefix } = servers.module;
    for (const prefix of grepped) {
      assert(
        stripErrorPrefix(`${prefix}: boom`) === "boom",
        `${prefix} strips`,
      );
    }
    // Test-only strings and agent-internal prefixes never cross the API.
    for (const prefix of [
      "cleanup", "cloudflare", "rpc", "dial", "hijack", "fake",
      "disconnected", "agent", "stats", "sudo",
    ]) {
      assert(
        stripErrorPrefix(`${prefix}: boom`) === `${prefix}: boom`,
        `${prefix} passes through`,
      );
    }
  });

  console.log("every describe path strips backend prefixes (fix round 1)");
  await check("database, team, proxy, service and generic paths strip", async () => {
    const backups = await loadModule("../src/features/databases/api/backups.ts");
    // Localized database summaries need the same catalogs the app merges.
    const databasesI18n = await loadApiWithCatalogs(
      "@/features/databases/api/databases",
      "databases",
      "@/features/databases/locales/en",
    );
    const backupsI18n = await loadApiWithCatalogs(
      "@/features/databases/api/backups",
      "databases",
      "@/features/databases/locales/en",
    );
    const containers = await loadModule("../src/features/servers/api/containers.ts");
    const metrics = await loadModule("../src/features/servers/api/metrics.ts");
    const notifications = await loadModule("../src/features/notifications/api/notifications.ts");
    const previews = await loadModule("../src/features/applications/api/previews.ts");
    const providers = await loadModule("../src/features/applications/api/providers.ts");
    const proxy = await loadModule("../src/features/domains/api/proxy.ts");
    const services = await loadModule("../src/features/services/api/services.ts");
    const templates = await loadModule("../src/features/templates/api/templates.ts");
    try {
      const prefixed = (prefix) => ({ message: `${prefix}: boom`, status: 500 });
      assert(
        databasesI18n.module.describeDatabaseError(prefixed("databases")) ===
          "Request failed (boom)",
        "database generic strips then summarizes",
      );
      assert(
        databasesI18n.module.describeDatabaseError(new Error("databases: gone")) ===
          "Something went wrong. Please try again. (gone)",
        "database Error path strips then summarizes",
      );
      assert(
        teams.module.describeTeamError({ message: "teams: gone", status: 400 }) ===
          "gone",
        "team 400 strips through the shared helper",
      );
      assert(
        teams.module.describeTeamError(prefixed("teams")) ===
          "Request failed: boom",
        "team generic pairs the summary with the raw diagnostic",
      );
      assert(
        proxy.module.describeProxyError({ message: "proxy: bad", status: 400 }) ===
          "bad",
        "proxy 400 strips",
      );
      assert(
        services.module.describeServiceError({ message: "services: down", status: 502 }) ===
          "Node agent error: down",
        "service 502 strips the embedded detail",
      );
      assert(
        services.module.describeServiceError({ message: "services: down", status: 500 }) ===
          "Request failed: down",
        "service unknown status pairs the summary with the same raw",
      );
      assert(
        services.module.describeServiceError(new Error("services: plain boom")) ===
          "Request failed: plain boom",
        "service plain error pairs the summary with the same raw",
      );
      assert(
        services.module.describeServiceError({ message: "", status: 500 }) ===
          "Request failed",
        "service empty unknown falls back exactly once",
      );
      assert(
        backupsI18n.module.describeBackupError(prefixed("databases")) ===
          "Request failed (boom)",
        "backup generic strips then summarizes",
      );
      assert(
        containers.module.describeContainerError(prefixed("containers")) === "boom",
        "container generic strips",
      );
      assert(
        metrics.module.describeMetricsError(prefixed("ws")) === "boom",
        "metrics generic strips",
      );
      assert(
        notifications.module.describeChannelError(prefixed("notifications")) ===
          "Request failed: boom",
        "channel generic pairs the summary with the raw diagnostic",
      );
      assert(
        previews.module.describePreviewError(prefixed("deploy")) ===
          "Request failed boom",
        "preview generic keeps the summary with the stripped raw",
      );
      assert(
        providers.module.describeProviderError(prefixed("providers")) ===
          "Request failed boom",
        "provider generic keeps the summary with the stripped raw",
      );
      assert(
        templates.module.describeTemplateError(prefixed("templates")) === "boom",
        "template generic strips",
      );
      assert(
        applications.module.describeApplicationError(prefixed("deploy")) ===
          "Request failed boom",
        "application generic keeps the summary with the stripped raw",
      );
      assert(
        applications.module.describeApplicationError(
          { message: "config: key X missing", status: 500 },
        ) === "Request failed config: key X missing",
        "ordinary words still pass through under the summary",
      );
    } finally {
      for (const loaded of [
        backups, containers, metrics, notifications, previews,
        providers, proxy, services, templates,
      ]) {
        await loaded.cleanup();
      }
    }
  });

  await check("bounded latest-state reads carry ?limit=1", async () => {
    const bundled = await loadApplicationsWithHttp();
    try {
      const { latestDeploymentStates } = bundled.module;
      const state = {
        calls: [],
        inflight: 0,
        maxInflight: 0,
        failApps: new Set(["app-3"]),
      };
      globalThis.__gothamHttp = {
        get: async (url, config) => {
          state.calls.push({ url, config });
          state.inflight += 1;
          state.maxInflight = Math.max(state.maxInflight, state.inflight);
          await sleep(5);
          state.inflight -= 1;
          const id = url.split("/")[2];
          if (state.failApps.has(id)) {
            throw new Error("boom");
          }
          if (id === "app-empty") {
            return { data: { deployments: [] } };
          }
          return {
            data: {
              deployments: [{ state: id === "app-2" ? "failed" : "running" }],
            },
          };
        },
      };
      const ids = ["app-1", "app-2", "app-empty", "app-3", "app-4", "app-5"];
      const { states, failed } = await latestDeploymentStates(ids, 2);
      assert(state.maxInflight <= 2, `concurrency capped (saw ${state.maxInflight})`);
      assert(state.calls.length === 6, "one read per application");
      assert(
        state.calls.every((call) => call.config?.params?.limit === 1),
        "every read is bounded to the latest row",
      );
      assert(failed === 1, "one failed read counted");
      assert(states.length === 6, "states keep input order");
      assert(states[1] === "failed", "failed state preserved");
      assert(states[2] === null && states[3] === null, "empty/failed read as null");
      assert(
        states[0] === "running" && states[4] === "running" && states[5] === "running",
        "running states preserved",
      );
    } finally {
      delete globalThis.__gothamHttp;
      await bundled.cleanup();
    }
  });

  await check("sign-out resets the teams cache (A owner, B read-only)", async () => {
    const harness = await loadStoreHarness("../src/features/teams/stores", "teams", "useTeamsStore", {
      "@/features/teams/api/teams": [
        "export const listTeams = () => globalThis.__teamsApi.listTeams();",
        "export const createTeam = async () => { throw new Error('unused'); };",
        "export const deleteTeam = async () => {};",
        "export const renameTeam = async () => { throw new Error('unused'); };",
        "export const describeTeamError = (e) => String((e && e.message) || e);",
        "export const isFeatureDisabled = () => false;",
      ].join("\n"),
    });
    try {
      const { useTeamsStore, createPinia, setActivePinia } = harness.module;
      setActivePinia(createPinia());
      const store = useTeamsStore();
      globalThis.__teamsApi = {
        listTeams: async () => [
          { id: "t1", name: "A", is_personal: true, role: "owner" },
        ],
      };
      await store.fetchTeams();
      assert(store.activeTeam?.role === "owner", "A signs in as owner");
      store.reset();
      assert(store.teams.length === 0, "reset drops the list");
      assert(store.loaded === false, "reset clears the loaded flag");
      assert(store.activeTeamId === "", "reset clears the selection");
      globalThis.__teamsApi = {
        listTeams: async () => [
          { id: "t2", name: "B", is_personal: true, role: "read_only" },
        ],
      };
      await store.fetchTeams();
      assert(store.activeTeam?.role === "read_only", "B signs in as read-only");
    } finally {
      delete globalThis.__teamsApi;
      await harness.cleanup();
    }
  });

  await check("user-scoped stores reset on sign-out", async () => {
    const serversHarness = await loadStoreHarness("../src/features/servers/stores", "servers", "useServersStore", {
      "@/features/servers/api/servers": [
        "const api = () => globalThis.__serversApi;",
        "export const listServers = (...a) => api().listServers(...a);",
        "export const createServer = (...a) => api().createServer(...a);",
        "export const deleteServer = (...a) => api().deleteServer(...a);",
        "export const validateServer = (...a) => api().validateServer(...a);",
        "export const updateServer = (...a) => api().updateServer(...a);",
        "export const describeServerError = (e) => String((e && e.message) || e);",
        "export const isApiError = (e) => typeof e === 'object' && e !== null && 'message' in e && 'status' in e;",
        "export const stripErrorPrefix = (m) => String(m ?? '').trim();",
        "export const failureText = (e) => String((e && e.message) || e);",
      ].join("\n"),
    });
    const appsHarness = await loadStoreHarness("../src/features/applications/stores", "applications", "useApplicationsStore", {
      "@/features/applications/api/applications": [
        "export const describeApplicationError = (e) => String((e && e.message) || e);",
        "export const getApplication = async () => { throw new Error('unused'); };",
        "export const getDeploymentBuildLog = async () => '';",
        "export const getEnv = async () => [];",
        "export const getStorages = async () => [];",
        "export const isActiveDeployment = () => false;",
        "export const listDeployments = async () => [];",
        "export const replaceEnv = async () => [];",
        "export const replaceStorages = async () => [];",
        "export const rollbackDeployment = async () => { throw new Error('unused'); };",
        "export const startApplication = async () => { throw new Error('unused'); };",
        "export const stopApplication = async () => { throw new Error('unused'); };",
        "export const triggerDeploy = async () => { throw new Error('unused'); };",
        "export const updateApplication = async () => { throw new Error('unused'); };",
        "export const deleteApplication = async () => { throw new Error('unused'); };",
      ].join("\n"),
    });
    const databasesHarness = await loadStoreHarness("../src/features/databases/stores", "databases", "useDatabasesStore", {
      "@/features/databases/api/databases": [
        "export const createDatabase = async () => { throw new Error('unused'); };",
        "export const deleteDatabase = async () => {};",
        "export const describeDatabaseError = (e) => String((e && e.message) || e);",
        "export const getDatabase = async () => { throw new Error('unused'); };",
        "export const getDatabaseCredentials = async () => { throw new Error('unused'); };",
        "export const listDatabases = async () => [];",
        "export const renameDatabase = async () => { throw new Error('unused'); };",
        "export const restartDatabase = async () => { throw new Error('unused'); };",
        "export const startDatabase = async () => { throw new Error('unused'); };",
        "export const stopDatabase = async () => { throw new Error('unused'); };",
        "export const updateDatabase = async () => { throw new Error('unused'); };",
      ].join("\n"),
    });
    const notificationsHarness = await loadStoreHarness(
      "../src/features/notifications/stores",
      "notifications",
      "useNotificationsStore",
      {
        "@/features/notifications/api/notifications": [
          "export const createChannel = async () => { throw new Error('unused'); };",
          "export const deleteChannel = async () => {};",
          "export const describeChannelError = (e) => String((e && e.message) || e);",
          "export const isFeatureDisabled = () => false;",
          "export const listChannels = async () => [];",
          "export const testChannel = async () => { throw new Error('unused'); };",
          "export const updateChannel = async () => { throw new Error('unused'); };",
        ].join("\n"),
        "@/features/teams": [
          "export const useTeamsStore = () => ({ activeTeamId: '' });",
        ].join("\n"),
      },
    );
    try {
      serversHarness.module.setActivePinia(serversHarness.module.createPinia());
      const serversStore = serversHarness.module.useServersStore();
      serversStore.servers = [{ id: "s1" }];
      serversStore.error = "stale";
      serversStore.reset();
      assert(serversStore.servers.length === 0, "servers reset drops the list");
      assert(serversStore.error === null, "servers reset clears the error");

      appsHarness.module.setActivePinia(appsHarness.module.createPinia());
      const appsStore = appsHarness.module.useApplicationsStore();
      appsStore.applicationsById = { a: { id: "a" } };
      appsStore.deploymentsByApp = { a: [] };
      appsStore.reset();
      assert(
        Object.keys(appsStore.applicationsById).length === 0,
        "applications reset drops the cache",
      );
      assert(
        Object.keys(appsStore.deploymentsByApp).length === 0,
        "applications reset drops deployments",
      );

      databasesHarness.module.setActivePinia(
        databasesHarness.module.createPinia(),
      );
      const databasesStore = databasesHarness.module.useDatabasesStore();
      databasesStore.databases = [{ id: "d1" }];
      databasesStore.credentialsById = { d1: {} };
      databasesStore.reset();
      assert(databasesStore.databases.length === 0, "databases reset drops rows");
      assert(
        Object.keys(databasesStore.credentialsById).length === 0,
        "databases reset drops credentials",
      );

      notificationsHarness.module.setActivePinia(
        notificationsHarness.module.createPinia(),
      );
      const notificationsStore = notificationsHarness.module.useNotificationsStore();
      notificationsStore.channels = [{ id: "c1" }];
      notificationsStore.reset();
      assert(
        notificationsStore.channels.length === 0,
        "notifications reset drops channels",
      );
    } finally {
      await serversHarness.cleanup();
      await appsHarness.cleanup();
      await databasesHarness.cleanup();
      await notificationsHarness.cleanup();
    }
  });

  await check("auth sign-out resets every user-scoped store", async () => {
    const harness = await loadAuthHarness();
    try {
      const { useAuthStore, createPinia, setActivePinia } = harness.module;
      const seen = [];
      const postCalls = [];
      globalThis.__userStores = Object.fromEntries(
        [
          "teams", "servers", "applications", "databases", "notifications",
          "services", "backups", "providers", "templates", "proxy",
          "projects", "github-app",
        ].map((name) => [
          name,
          { reset: () => seen.push(name) },
        ]),
      );
      globalThis.__authHttp = {
        post: async (...args) => {
          postCalls.push(args);
          return { data: {} };
        },
      };
      setActivePinia(createPinia());
      const store = useAuthStore();
      store.setSession({
        user: { email: "a@example.com" },
        access_token: "access",
        refresh_token: "refresh",
      });
      await store.logout();
      assert(postCalls.length === 1, "logout revokes the refresh token");
      assert(seen.length === 12, `all twelve stores reset (saw ${seen.length})`);
      assert(store.accessToken === null, "session cleared");
      store.setSession({
        user: { email: "b@example.com" },
        access_token: "access",
        refresh_token: "refresh",
      });
      store.clearSession();
      assert(seen.length === 24, "clearSession resets too (401 path)");
    } finally {
      delete globalThis.__userStores;
      delete globalThis.__authHttp;
      await harness.cleanup();
    }
  });

  await check("forced logout clears persisted user-scoped keys (fix round 2)", async () => {
    const store = new Map();
    const redirects = [];
    globalThis.window = {
      localStorage: {
        getItem: (key) => (store.has(key) ? store.get(key) : null),
        setItem: (key, value) => {
          store.set(key, String(value));
        },
        removeItem: (key) => {
          store.delete(key);
        },
      },
      location: {
        pathname: "/servers",
        assign: (url) => redirects.push(url),
      },
      addEventListener: () => {},
    };
    let httpBundle = null;
    try {
      httpBundle = await loadHttpReal();
      const { expireSession } = httpBundle.module;
      store.set(
        "gotham.auth.session",
        JSON.stringify({ user: { id: "u" }, accessToken: "a", refreshToken: "r" }),
      );
      store.set("gotham.teams.active", "team-1");
      store.set(
        "gotham-refresh-lock",
        JSON.stringify({ id: "x", expiresAt: Date.now() + 99999 }),
      );
      expireSession();
      assert(!store.has("gotham.auth.session"), "session cleared");
      assert(
        !store.has("gotham.teams.active"),
        "previous user's active team does not survive a 401",
      );
      assert(
        store.has("gotham-refresh-lock"),
        "cross-tab refresh lease is not user-scoped and survives",
      );
      assert(redirects.includes("/login"), "forced logout redirects to login");
    } finally {
      delete globalThis.window;
      if (httpBundle) {
        await httpBundle.cleanup();
      }
    }
  });

  await check("forced logout prefers the in-app router over a hard reload", async () => {
    // B3-3: expireSession must let the SPA router take over (cancelable
    // gotham:session-expired event) so a failed logout never degrades into a
    // document reload; without a listener it keeps the hard assign fallback.
    const store = new Map();
    const redirects = [];
    const seenEvents = [];
    globalThis.window = {
      localStorage: {
        getItem: (key) => (store.has(key) ? store.get(key) : null),
        setItem: (key, value) => {
          store.set(key, String(value));
        },
        removeItem: (key) => {
          store.delete(key);
        },
      },
      location: {
        pathname: "/servers",
        assign: (url) => redirects.push(url),
      },
      addEventListener: () => {},
      dispatchEvent: (event) => {
        seenEvents.push(event.type);
        // Canceled (false) means the SPA router took over in-app.
        return false;
      },
    };
    let httpBundle = null;
    try {
      httpBundle = await loadHttpReal();
      const { expireSession } = httpBundle.module;
      store.set(
        "gotham.auth.session",
        JSON.stringify({ user: { id: "u" }, accessToken: "a", refreshToken: "r" }),
      );
      expireSession();
      assert(!store.has("gotham.auth.session"), "session cleared");
      assert(seenEvents.includes("gotham:session-expired"), "expiry announced");
      assert(redirects.length === 0, "no hard reload while the SPA handles it");
      globalThis.window.dispatchEvent = () => true;
      store.set(
        "gotham.auth.session",
        JSON.stringify({ user: { id: "u" }, accessToken: "a", refreshToken: "r" }),
      );
      expireSession();
      assert(redirects.includes("/login"), "hard fallback without a listener");
    } finally {
      delete globalThis.window;
      if (httpBundle) {
        await httpBundle.cleanup();
      }
    }
  });

  await format.cleanup();
  await servers.cleanup();
  await teams.cleanup();
  await databases.cleanup();
  await applications.cleanup();
  await dashboard.cleanup();

  const failed = results.filter((r) => !r.ok);
  console.log(
    `\n${results.length - failed.length}/${results.length} checks passed`,
  );
  if (failed.length > 0) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
