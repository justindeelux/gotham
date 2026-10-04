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

// Web source root for the "@" import alias (mirrors vite.config.ts).
const srcDir = new URL("../src", import.meta.url).pathname;

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
    alias: { "@": srcDir },
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
  const format = await loadModule("../src/shared/utils/format.ts");
  const targetBody = await loadModule("../src/features/databases/utils/backupTarget.ts");
  const storeMerge = await loadModule("../src/shared/utils/storeMerge.ts");
  const restoreOutcomes = await loadModule("../src/features/databases/utils/restoreOutcomes.ts");

  try {
    const { relativeTime } = format.module;
    const { toTargetBody } = targetBody.module;
    const { mergeBackupsById, mergeDatabasesById } = storeMerge.module;
    const { advanceRestoreStatuses } = restoreOutcomes.module;

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

    await check("D3-15/U4: a queued running backup is kept and sorted first", () => {
      const queued = { id: "queued", status: "running", created_at: "2026-10-03T12:00:00Z" };
      const older = { id: "older", status: "completed", created_at: "2026-10-03T10:00:00Z" };
      const merged = mergeBackupsById([queued], [older]);
      assert(merged.length === 2, "both rows survive");
      assert(merged[0].id === "queued", "the queued row is first, not appended");
    });

    await check("D3-15: the server wins for a known id", () => {
      const local = { id: "b1", status: "running", created_at: "2026-10-03T12:00:00Z" };
      const server = { id: "b1", status: "completed", created_at: "2026-10-03T12:00:00Z" };
      const merged = mergeBackupsById([local], [server]);
      assert(merged.length === 1 && merged[0].status === "completed", "server status wins");
    });

    await check("U3: a stale list cannot resurrect a deleted database", () => {
      const stale = [{ id: "deleted" }, { id: "live" }];
      const merged = mergeDatabasesById(stale, [], new Set(["deleted"]));
      assert(merged.length === 1 && merged[0].id === "live", "deleted id is dropped");
    });

    await check("D3-15: a local-only database is preserved, server wins otherwise", () => {
      const server = [{ id: "live", name: "server-name" }];
      const local = [{ id: "live", name: "local-name" }, { id: "fresh", name: "fresh" }];
      const merged = mergeDatabasesById(server, local);
      assert(merged.length === 2, "local-only row kept");
      assert(merged.find((d) => d.id === "live").name === "server-name", "server wins");
    });

    await check("U1: a poll observing running→completed announces exactly once", () => {
      const seen = new Map();
      const running = advanceRestoreStatuses(seen, [{ id: "r1", status: "running" }]);
      assert(running.length === 0, "running is not announced");
      const completed = advanceRestoreStatuses(seen, [{ id: "r1", status: "completed" }]);
      assert(completed.length === 1 && completed[0].status === "completed", "completion announced");
      const again = advanceRestoreStatuses(seen, [{ id: "r1", status: "completed" }]);
      assert(again.length === 0, "not announced twice");
    });

    await check("U4b: a stale running response cannot re-arm a terminal toast", () => {
      const seen = new Map([["r2", "running"]]);
      advanceRestoreStatuses(seen, [{ id: "r2", status: "failed", error: "boom" }]);
      const stale = advanceRestoreStatuses(seen, [{ id: "r2", status: "running" }]);
      assert(stale.length === 0, "stale running does not re-arm");
      assert(seen.get("r2") === "failed", "terminal status is retained");
    });

    await check("U2: an unseeded terminal restore is silent (202 seeding covers it)", () => {
      const seen = new Map();
      const outcomes = advanceRestoreStatuses(seen, [{ id: "r3", status: "completed" }]);
      assert(outcomes.length === 0, "unseeded terminal is not announced");
    });
  } finally {
    await format.cleanup();
    await targetBody.cleanup();
    await storeMerge.cleanup();
    await restoreOutcomes.cleanup();
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
