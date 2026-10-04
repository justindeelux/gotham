// FX-15b frontend checks.
//
// Like the other web checks, this bundles the changed modules with the
// project's own esbuild (there is no vitest/jsdom runner) and asserts the
// behaviour the fixes introduce:
//   · C4-18 — a 404 on stop/start is "no container in that state", not
//     "application not found".
//   · C4-14 — editor row keys stay stable across a middle removal/insert.
//   · B4-13 — one danger threshold shared by the dashboard and server list.
//
// Run from web/:  node scripts/fx15b-check.mjs

import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { build } from "esbuild";

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

async function loadModule(relativePath) {
  const directory = await mkdtemp(join(tmpdir(), "gotham-fx15b-check-"));
  // CJS output: applications.ts pulls in axios, whose transitive form-data
  // uses require("util"); an ESM bundle turns that into an unsupported dynamic
  // require. A CJS bundle keeps native require, and import() still loads it.
  const outfile = join(directory, "module.cjs");
  await build({
    entryPoints: [new URL(relativePath, import.meta.url).pathname],
    outfile,
    bundle: true,
    format: "cjs",
    platform: "node",
    target: "node20",
    logLevel: "silent",
    alias: { "@": srcDir },
    plugins: [vueStubPlugin],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    module,
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

/** loadInline bundles a small entry that imports both vue and the module. */
async function loadInline(contents, resolveDir) {
  const directory = await mkdtemp(join(tmpdir(), "gotham-fx15b-check-"));
  const outfile = join(directory, "module.mjs");
  await build({
    stdin: { contents, resolveDir, sourcefile: "entry.ts", loader: "ts" },
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

/** apiError builds the normalized shape isApiError accepts. */
function apiError(status, message = "") {
  return { status, message, cause: null };
}

async function main() {
  const applications = await loadModule("../src/features/applications/api/applications.ts");
  const format = await loadModule("../src/shared/utils/format.ts");
  const version = await loadModule("../src/features/version/api/version.ts");
  const rowKeys = await loadInline(
    `import { ref } from "vue";
     import { useStableRowKeys } from "./useStableRowKeys";
     export { ref, useStableRowKeys };`,
    new URL("../src/shared/composables/", import.meta.url).pathname,
  );

  try {
    const { describeApplicationError } = applications.module;
    const { toPercent, USAGE_DANGER_PERCENT } = format.module;
    const { formatVersionTag } = version.module;
    const { ref, useStableRowKeys } = rowKeys.module;

    await check("C4-18: a stop 404 means no running container, not a missing app", () => {
      const message = describeApplicationError(apiError(404), "stop");
      assert(
        message === "No running container to stop. It may already be stopped.",
        `unexpected: ${message}`,
      );
    });

    await check("C4-18: a start 404 means no container to start", () => {
      const message = describeApplicationError(apiError(404), "start");
      assert(
        message === "No container to start. Deploy the application first.",
        `unexpected: ${message}`,
      );
    });

    await check("C4-18: every other 404 still reads as not found", () => {
      assert(
        describeApplicationError(apiError(404)).startsWith("Application not found"),
        "plain 404 must stay not-found",
      );
    });

    await check("C4-18: other statuses keep their mapping with a control action", () => {
      assert(
        describeApplicationError(apiError(409), "stop").includes("already in progress"),
        "409 must stay in-progress",
      );
      assert(
        describeApplicationError(apiError(502), "start").includes("node agent"),
        "502 must stay agent unreachable",
      );
    });

    await check("C4-14: removing a middle row does not reuse its neighbours' keys", () => {
      const count = ref(3);
      const { keys, removeAt } = useStableRowKeys(() => count.value);
      assert(keys.value.length === 3, "three keys minted");
      const before = [...keys.value];
      removeAt(1);
      count.value = 2;
      assert(keys.value.length === 2, "key removed with the row");
      assert(keys.value[0] === before[0], "first row keeps its key");
      assert(keys.value[1] === before[2], "the surviving tail keeps its own key");
    });

    await check("C4-14: appending mints a fresh, distinct key", () => {
      const count = ref(1);
      const { keys, insertAt } = useStableRowKeys(() => count.value);
      const first = keys.value[0];
      insertAt(1);
      count.value = 2;
      assert(keys.value.length === 2, "key added");
      assert(keys.value[1] !== first, "new key is distinct");
    });

    await check("C4-14: external loads reconcile to the new length", () => {
      const count = ref(1);
      const { keys } = useStableRowKeys(() => count.value);
      const before = [...keys.value];
      count.value = 4;
      // The watcher is sync, so a length change is reflected immediately.
      assert(keys.value.length === 4, "keys grow with the loaded collection");
      assert(keys.value[0] === before[0], "existing head is preserved");
    });

    await check("B4-13: the danger threshold is one shared value at 80", () => {
      assert(USAGE_DANGER_PERCENT === 80, "threshold is 80");
      assert(toPercent(0.8) === 80, "0.8 fraction normalizes to the threshold");
      assert(toPercent(80) === 80, "80 already reads as a percentage");
    });

    await check("B4-13: dashboard and server list share the threshold (no hard-coded 80)", async () => {
      const pages = ["../src/features/dashboard/pages/DashboardPage.vue", "../src/features/servers/pages/ServersPage.vue"];
      for (const page of pages) {
        const source = await readFile(new URL(page, import.meta.url), "utf8");
        assert(
          source.includes("USAGE_DANGER_PERCENT") ||
            source.includes("usageLevel") ||
            source.includes("usageView"),
          `${page} must reference the shared threshold`,
        );
        assert(
          !/[><]=?\s*80\b/.test(source),
          `${page} must not hard-code a literal 80 comparison`,
        );
      }
    });

    await check("B4-13: exactly 80% counts as danger (inclusive >=)", async () => {
      for (const page of ["../src/features/dashboard/pages/DashboardPage.vue", "../src/features/servers/pages/ServersPage.vue"]) {
        const source = await readFile(new URL(page, import.meta.url), "utf8");
        assert(
          source.includes(">= USAGE_DANGER_PERCENT") ||
            source.includes("usageLevel") ||
            source.includes("usageView"),
          `${page} must compare with >= (a node at exactly 80% is danger-red)`,
        );
      }
      // Boundary behaviour through the shared normalizer: 0.8 lands exactly
      // on the threshold, so an inclusive comparison flags it.
      assert(
        toPercent(0.8) >= USAGE_DANGER_PERCENT,
        "0.8 must reach the danger threshold",
      );
      assert(
        toPercent(0.794) < USAGE_DANGER_PERCENT,
        "just under 0.8 must stay below the danger threshold",
      );
    });

    await check("C4-14: EnvEditor/StorageEditor keep stable row keys wired", async () => {
      const editors = [
        "../src/features/applications/components/EnvEditor.vue",
        "../src/features/applications/components/StorageEditor.vue",
      ];
      for (const editor of editors) {
        const source = await readFile(new URL(editor, import.meta.url), "utf8");
        assert(source.includes("useStableRowKeys"), `${editor} must use useStableRowKeys`);
        assert(source.includes("insertAt"), `${editor} must wire insertAt`);
        assert(source.includes("removeAt"), `${editor} must wire removeAt`);
        assert(
          source.includes("rowKeys[index]"),
          `${editor} must key rows by rowKeys[index]`,
        );
      }
    });
    await check("JUS-7: the sidebar tag prefixes a bare version and never doubles v", () => {
      assert(formatVersionTag("0.2.0") === "v0.2.0", "bare version gains v");
      assert(formatVersionTag("v0.2.0") === "v0.2.0", "v-prefixed version stays single-v");
      assert(formatVersionTag("dev") === "dev", "dev builds render bare dev");
    });

    await check("JUS-7: an empty version hides the tag instead of rendering", () => {
      assert(formatVersionTag("") === null, "empty hides");
      assert(formatVersionTag(null) === null, "null (loading/error) hides");
      assert(formatVersionTag(undefined) === null, "undefined hides");
      assert(formatVersionTag("v") === null, "lone v hides");
    });

  } finally {
    await applications.cleanup();
    await format.cleanup();
    await version.cleanup();
    await rowKeys.cleanup();
  }

  const failed = results.filter((result) => !result.ok);
  console.log(`\n${results.length - failed.length}/${results.length} checks passed`);
  if (failed.length > 0) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
