import assert from "node:assert/strict";
import { test } from "node:test";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { selectJobs, checkGate } from "./ci-scope.mjs";

const enabled = (paths, full = false) => Object.entries(selectJobs(paths, full))
  .filter(([, value]) => value).map(([name]) => name).sort();

test("site and reference docs avoid application suites", () => {
  assert.deepEqual(enabled(["site/guide/install.md", "README.md"]), ["site"]);
  assert.deepEqual(enabled(["AGENTS.md", "docs/development-workflow.md"]), []);
});

test("SPA and generated assets run browser proof without backend E2E", () => {
  for (const path of ["web/src/App.vue", "internal/server/webdist/index.html"]) {
    assert.deepEqual(enabled([path]), ["build-test", "lint", "ui-e2e", "web"]);
  }
});

test("backend changes retain DB, API and browser integration proof", () => {
  for (const path of ["internal/auth/http.go", "agent/files.go", "toolcatalog/catalog.go"]) {
    assert.deepEqual(enabled([path]), ["build-test", "db-test", "e2e", "lint", "ui-e2e"]);
  }
});

test("schema and protocol changes also check their generated consumers", () => {
  assert.ok(enabled(["internal/store/queries/apps.sql"]).includes("sqlc"));
  assert.ok(enabled(["internal/store/migrations/001.sql"]).includes("db-test"));
  assert.ok(enabled(["internal/store/sqlc/apps.sql.go"]).includes("sqlc"));
  assert.ok(enabled(["proto/agent/v1/agent.proto"]).includes("proto"));
});

test("install scripts run installer and Go contract tests", () => {
  assert.deepEqual(enabled(["deploy/install.sh"]), ["build-test", "installer", "lint"]);
});

test("shared config, unknown paths and main always run everything", () => {
  const all = enabled([], true);
  assert.equal(all.length, 10);
  for (const path of ["Makefile", "go.mod", ".github/workflows/ci.yml", "new-module/x.go"]) {
    assert.deepEqual(enabled([path]), all);
  }
  assert.deepEqual(enabled(["site/index.md"], true), all);
});

test("mixed changes take the union and deleted paths still count", () => {
  assert.deepEqual(enabled(["site/index.md", "deploy/old-script.sh"]),
    ["build-test", "installer", "lint", "site"]);
});

test("the merge gate depends on discovery and every selectable workflow job", () => {
  const workflow = readFileSync(new URL("../workflows/ci.yml", import.meta.url), "utf8");
  const jobs = workflow.split("\njobs:\n")[1];
  const names = [...jobs.matchAll(/^  ([a-z][a-z0-9-]+):$/gm)].map((match) => match[1]);
  const selected = Object.keys(selectJobs([], true));
  assert.deepEqual(names.sort(), ["changes", "gate", ...selected].sort());
  const gate = jobs.split("\n  gate:\n")[1];
  assert.match(gate, /if: always\(\)/);
  const dependencies = gate.match(/needs: \[([^\]]+)\]/)[1].split(",").map((name) => name.trim());
  assert.deepEqual(dependencies.sort(), ["changes", ...selected].sort());
});

function results(paths) {
  const jobs = selectJobs(paths);
  return {
    changes: { result: "success", outputs: { jobs: JSON.stringify(jobs), ready: "true" } },
    ...Object.fromEntries(Object.entries(jobs).map(([name, required]) =>
      [name, { result: required ? "success" : "skipped" }])),
  };
}

test("gate accepts successful selected jobs and intentionally skipped jobs", () => {
  checkGate(results(["site/index.md"]));
  checkGate(results(["README.md"]));
});

test("gate rejects failed, cancelled, missing or unexpectedly skipped jobs", () => {
  for (const result of ["failure", "cancelled", "skipped", undefined]) {
    const needs = results(["site/index.md"]);
    needs.site.result = result;
    assert.throws(() => checkGate(needs));
  }
  const needs = results(["site/index.md"]);
  needs.web.result = "failure";
  assert.throws(() => checkGate(needs));
});

test("gate fails closed on failed discovery, drafts and incomplete selection", () => {
  for (const result of ["failure", "cancelled", "skipped"]) {
    const needs = results([]);
    needs.changes.result = result;
    assert.throws(() => checkGate(needs));
  }
  const draft = results([]);
  draft.changes.outputs.ready = "false";
  assert.throws(() => checkGate(draft), /draft/i);
  const incomplete = results([]);
  incomplete.changes.outputs.jobs = "{}";
  assert.throws(() => checkGate(incomplete));
});

test("CLI compares the merge base, includes both renamed paths and handles draft/main", (t) => {
  const root = mkdtempSync(join(tmpdir(), "gotham-ci-scope-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const git = (...args) => execFileSync("git", args, {
    cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
  }).trim();
  git("init", "--initial-branch=main");
  git("config", "user.name", "CI scope test");
  git("config", "user.email", "ci-test@example.com");
  writeFileSync(join(root, "README.md"), "Fixture\n");
  git("add", ".");
  git("commit", "-m", "chore: seed fixture");
  git("switch", "-c", "docs/change");
  // Renaming from a recognized docs path to an unknown filename must select the full suite.
  git("mv", "README.md", "unusual\nfilename");
  git("commit", "-am", "docs: rename fixture");
  const head = git("rev-parse", "HEAD");
  git("switch", "main");
  writeFileSync(join(root, "unrelated.go"), "package unrelated\n");
  git("add", ".");
  git("commit", "-m", "chore: advance base");
  const base = git("rev-parse", "HEAD");
  const eventFile = join(root, "event.json");
  const output = join(root, "output");
  const run = (eventName, draft) => {
    writeFileSync(eventFile, JSON.stringify({ pull_request: { base: { sha: base }, head: { sha: head }, draft } }));
    writeFileSync(output, "");
    const result = spawnSync(process.execPath, [fileURLToPath(new URL("./ci-scope.mjs", import.meta.url))], {
      cwd: root, encoding: "utf8", env: {
        ...process.env, GITHUB_EVENT_NAME: eventName, GITHUB_EVENT_PATH: eventFile,
        GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: join(root, "summary"),
      },
    });
    assert.equal(result.status, 0, result.stderr);
    return Object.fromEntries(readFileSync(output, "utf8").trim().split("\n").map((line) => {
      const at = line.indexOf("=");
      return [line.slice(0, at), line.slice(at + 1)];
    }));
  };
  const pr = run("pull_request", false);
  assert.deepEqual(JSON.parse(pr.jobs), selectJobs([], true));
  assert.equal(pr.ready, "true");
  assert.equal(run("pull_request", true).ready, "false");
  assert.deepEqual(JSON.parse(run("push", false).jobs), selectJobs([], true));
});
