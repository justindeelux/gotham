import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const script = fileURLToPath(new URL("../../scripts/start-work.mjs", import.meta.url));
const env = { ...process.env, GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_NOSYSTEM: "1" };
const git = (cwd, ...args) => execFileSync("git", args, {
  cwd, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
}).trim();

function fixture(t) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), "gotham-workflow-")));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const repo = join(root, "gotham");
  git(root, "init", "--bare", "origin.git");
  git(root, "init", "--initial-branch=main", repo);
  git(repo, "config", "user.email", "workflow-test@example.com");
  git(repo, "config", "user.name", "Workflow test");
  writeFileSync(join(repo, "README.md"), "Fixture\n");
  writeFileSync(join(repo, ".gitignore"), "opencode.json\ndocs/ORCHESTRATION.md\ndocs/test-server.md\n");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "chore: seed fixture");
  git(repo, "remote", "add", "origin", join(root, "origin.git"));
  git(repo, "push", "-u", "origin", "main");
  mkdirSync(join(root, "bin"));
  writeFileSync(join(root, "bin", "gh"),
    `#!/bin/sh\nprintf '%s\\n' "$TEST_MERGED_PRS"\n`, { mode: 0o755 });
  const taskEnv = { ...env, PATH: `${join(root, "bin")}:${env.PATH}`, TEST_MERGED_PRS: "[]" };
  const run = (...args) => spawnSync(process.execPath, [script, ...args], {
    cwd: repo, env: taskEnv, encoding: "utf8",
  });
  return { repo, root, work: join(root, "gotham-work"), run, taskEnv };
}

test("sequential checkout is reused, keeps primary main and local model config", (t) => {
  const { repo, work, run } = fixture(t);
  let result = run("fix/first");
  assert.equal(result.status, 0, result.stderr);
  assert.equal(git(work, "branch", "--show-current"), "fix/first");
  writeFileSync(join(work, "opencode.json"), '{"model":"owner-choice"}\n');
  result = run("docs/next");
  assert.equal(result.status, 0, result.stderr);
  assert.equal(git(work, "branch", "--show-current"), "docs/next");
  assert.equal(git(repo, "branch", "--show-current"), "main");
  assert.equal(JSON.parse(readFileSync(join(work, "opencode.json"), "utf8")).model, "owner-choice");
});

test("parallel work gets a separate checkout and existing paths are preserved", (t) => {
  const { root, run, work } = fixture(t);
  assert.equal(run("fix/first").status, 0);
  const result = run("fix/second", "--parallel");
  assert.equal(result.status, 0, result.stderr);
  assert.equal(git(join(root, "gotham-fix-second"), "branch", "--show-current"), "fix/second");
  assert.equal(git(work, "branch", "--show-current"), "fix/first");
  assert.notEqual(run("fix/second", "--parallel").status, 0);
});

test("dirty and untracked files prevent reuse without changing the branch", (t) => {
  const { work, run } = fixture(t);
  assert.equal(run("fix/first").status, 0);
  writeFileSync(join(work, "untracked.txt"), "Keep me\n");
  assert.notEqual(run("fix/next").status, 0);
  assert.equal(git(work, "branch", "--show-current"), "fix/first");
  assert.equal(readFileSync(join(work, "untracked.txt"), "utf8"), "Keep me\n");
});

test("unmerged and unpushed commits prevent reuse", (t) => {
  const { work, run } = fixture(t);
  assert.equal(run("fix/first").status, 0);
  writeFileSync(join(work, "README.md"), "Unpushed work\n");
  git(work, "commit", "-am", "fix: keep this work");
  const result = run("fix/next");
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /unpushed/);
  assert.equal(git(work, "branch", "--show-current"), "fix/first");
});

test("invalid branch names are rejected before creating a checkout", (t) => {
  const { work, run } = fixture(t);
  for (const args of [["fix/../../oops"], ["fix/ok", "--unknown"], ["main"]]) {
    assert.notEqual(run(...args).status, 0);
  }
  assert.equal(existsSync(work), false);
});

test("squash-merged checkout is reusable only with proof of its exact pushed head", (t) => {
  const { repo, root, work, run, taskEnv } = fixture(t);
  assert.equal(run("fix/first").status, 0);
  writeFileSync(join(work, "README.md"), "Delivered change\n");
  git(work, "commit", "-am", "fix: deliver change");
  git(work, "push", "-u", "origin", "fix/first");
  const head = git(work, "rev-parse", "HEAD");
  // Advance main first so the squash commit cannot have the feature commit's identity.
  writeFileSync(join(repo, "other.txt"), "Independent change\n");
  git(repo, "add", ".");
  git(repo, "commit", "-m", "chore: advance main");
  git(repo, "merge", "--squash", "fix/first");
  git(repo, "commit", "-m", "fix: squash delivery");
  git(repo, "push", "origin", "main");
  git(repo, "push", "origin", "--delete", "fix/first");
  git(repo, "config", "fetch.prune", "true");
  taskEnv.TEST_MERGED_PRS = JSON.stringify([{ headRefOid: "other-head" }]);
  assert.notEqual(run("fix/next").status, 0);
  assert.equal(git(work, "branch", "--show-current"), "fix/first");
  taskEnv.TEST_MERGED_PRS = JSON.stringify([{ headRefOid: head }]);
  const result = run("fix/next");
  assert.equal(result.status, 0, result.stderr);
  assert.equal(git(work, "rev-parse", "HEAD"), git(repo, "rev-parse", "HEAD"));
});

test("remote branch names are not reused and dangling local config is preserved", (t) => {
  const { repo, root, work, run } = fixture(t);
  git(repo, "push", "origin", "main:fix/existing");
  assert.notEqual(run("fix/existing").status, 0);
  assert.equal(existsSync(work), false);
  assert.equal(run("fix/first").status, 0);
  rmSync(join(work, "opencode.json"));
  symlinkSync(join(root, "missing-config"), join(work, "opencode.json"));
  const result = run("fix/next");
  assert.equal(result.status, 0, result.stderr);
  assert.equal(git(work, "branch", "--show-current"), "fix/next");
});

test("operator notes are linked to their original and symlinked checkouts are refused", (t) => {
  const { repo, root, work, run } = fixture(t);
  mkdirSync(join(repo, "docs"));
  const source = join(repo, "docs", "ORCHESTRATION.md");
  writeFileSync(source, "Local operator notes\n");
  assert.equal(run("fix/first").status, 0);
  assert.equal(realpathSync(join(work, "docs", "ORCHESTRATION.md")), source);
  writeFileSync(source, "Updated notes\n");
  assert.equal(readFileSync(join(work, "docs", "ORCHESTRATION.md"), "utf8"), "Updated notes\n");
  git(repo, "worktree", "move", work, join(root, "moved-work"));
  symlinkSync(repo, work);
  assert.notEqual(run("fix/next").status, 0);
  assert.equal(git(repo, "branch", "--show-current"), "main");
});
