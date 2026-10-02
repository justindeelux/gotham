// FX-14b frontend checks for the database pages.
//
// The repo has no web unit-test runner (no vitest/jsdom), so this script
// bundles the pure helpers with the project's own esbuild and asserts their
// behaviour directly. It covers the two frontend findings with pure logic:
// D3-3 (future-aware relativeTime) and D3-5 (explicit target clear marker).
//
// Run from web/:  node scripts/db-web-check.mjs

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
async function loadModule(relativePath) {
  const directory = await mkdtemp(join(tmpdir(), "gotham-db-check-"));
  const outfile = join(directory, "module.mjs");
  await build({
    entryPoints: [new URL(relativePath, import.meta.url).pathname],
    outfile,
    bundle: true,
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

/** isoOffset returns an ISO timestamp offset from now by deltaSeconds. */
function isoOffset(deltaSeconds) {
  return new Date(Date.now() + deltaSeconds * 1000).toISOString();
}

async function main() {
  const format = await loadModule("../src/utils/format.ts");
  const targetBody = await loadModule("../src/utils/backupTarget.ts");

  try {
    const { relativeTime } = format.module;
    const { toTargetBody } = targetBody.module;

    await check("D3-3: a past timestamp still reads as 'ago'", () => {
      assert(relativeTime(isoOffset(-2 * 3600)) === "2h ago", "2h past");
      assert(relativeTime(isoOffset(-30)) === "just now", "just now");
    });

    await check("D3-3: a future timestamp reads as 'in …'", () => {
      assert(relativeTime(isoOffset(2 * 3600)) === "in 2h", "2h future");
      assert(relativeTime(isoOffset(90 * 60)) === "in 2h", "90m future rounds to 2h");
      assert(relativeTime(isoOffset(30)) === "in a moment", "30s future");
      assert(relativeTime(isoOffset(3 * 86400)) === "in 3d", "3d future");
    });

    await check("D3-3: empty/invalid timestamps keep their sentinels", () => {
      assert(relativeTime(null) === "never", "null");
      assert(relativeTime("") === "never", "empty");
      assert(relativeTime("not-a-date") === "unknown", "invalid");
    });

    await check("D3-5: an empty region/prefix is sent as an explicit clear", () => {
      const body = toTargetBody({ name: "t", region: "", prefix: "" });
      assert(body.region === "", "empty region must be sent");
      assert(body.prefix === "", "empty prefix must be sent");
      assert(body.name === "t", "non-empty name is kept");
    });

    await check("D3-5: blank credentials and required fields are omitted", () => {
      const body = toTargetBody({
        name: "t",
        endpoint: "",
        bucket: "",
        access_key: "",
        secret_key: "",
      });
      assert(!("endpoint" in body), "blank endpoint omitted");
      assert(!("bucket" in body), "blank bucket omitted");
      assert(!("access_key" in body), "blank access key omitted");
      assert(!("secret_key" in body), "blank secret key omitted");
    });

    await check("D3-5: an omitted optional field stays omitted", () => {
      const body = toTargetBody({ name: "t" });
      assert(!("region" in body) && !("prefix" in body), "absent stays absent");
    });
  } finally {
    await format.cleanup();
    await targetBody.cleanup();
  }

  const failed = results.filter((r) => !r.ok);
  console.log(`\n${results.length - failed.length}/${results.length} checks passed`);
  if (failed.length > 0) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
