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
    plugins: [
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

async function main() {
  const format = await loadModule("../src/utils/format.ts");
  const servers = await loadModule("../src/api/servers.ts");
  const teams = await loadModule("../src/api/teams.ts");
  const databases = await loadModule("../src/api/databases.ts");
  const applications = await loadModule("../src/api/applications.ts");

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

  await format.cleanup();
  await servers.cleanup();
  await teams.cleanup();
  await databases.cleanup();
  await applications.cleanup();

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
