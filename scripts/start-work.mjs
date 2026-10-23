#!/usr/bin/env node
import { execFileSync, spawnSync } from "node:child_process";
import { lstatSync, mkdirSync, realpathSync, symlinkSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";

const [branch, mode, ...extra] = process.argv.slice(2);
if (!/^(feat|fix|refactor|perf|docs|test|ci|build|chore)\/[a-z0-9]+(?:-[a-z0-9]+)*$/.test(branch ?? "") ||
    (mode !== undefined && mode !== "--parallel") || extra.length) {
  console.error("Usage: node scripts/start-work.mjs <type>/<slug> [--parallel]");
  process.exit(1);
}

const git = (cwd, ...args) => execFileSync("git", ["-C", cwd, ...args], {
  encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
}).trim();
const exists = (path) => lstatSync(path, { throwIfNoEntry: false }) !== undefined;

try {
  const common = git(process.cwd(), "rev-parse", "--path-format=absolute", "--git-common-dir");
  const repo = realpathSync(dirname(common));
  const target = join(dirname(repo), `${basename(repo)}-${mode ? branch.replace("/", "-") : "work"}`);
  git(repo, "fetch", "origin");
  git(repo, "rev-parse", "--verify", "origin/main");
  for (const ref of [`refs/heads/${branch}`, `refs/remotes/origin/${branch}`]) {
    if (spawnSync("git", ["-C", repo, "show-ref", "--verify", "--quiet", ref]).status === 0) {
      throw new Error(`Branch ${branch} already exists; resume it explicitly`);
    }
  }

  if (exists(target)) {
    if (mode) throw new Error(`Parallel checkout already exists: ${target}`);
    if (realpathSync(target) !== resolve(target)) throw new Error("Refusing a symlinked checkout path");
    if (realpathSync(git(target, "rev-parse", "--path-format=absolute", "--git-common-dir")) !==
        realpathSync(common) || realpathSync(git(target, "rev-parse", "--show-toplevel")) !== realpathSync(target)) {
      throw new Error(`Target is not a checkout of this repository: ${target}`);
    }
    if (git(target, "status", "--porcelain")) {
      throw new Error(`Checkout has uncommitted or untracked work: ${target}`);
    }
    if (spawnSync("git", ["-C", target, "merge-base", "--is-ancestor", "HEAD", "origin/main"]).status !== 0) {
      const head = git(target, "rev-parse", "HEAD");
      const previous = git(target, "symbolic-ref", "--short", "HEAD");
      const prs = JSON.parse(execFileSync("gh", [
        "pr", "list", "--head", previous, "--base", "main", "--state", "merged",
        "--json", "headRefOid", "--limit", "100",
      ], { cwd: target, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }));
      if (!prs.some((pr) => pr.headRefOid === head)) {
        throw new Error("Previous branch has unpushed or unmerged work: no merged PR at its current head");
      }
    }
    git(target, "switch", "-c", branch, "origin/main");
  } else {
    git(repo, "worktree", "add", "-b", branch, target, "origin/main");
  }

  mkdirSync(join(target, "docs"), { recursive: true });
  for (const name of ["ORCHESTRATION.md", "test-server.md"]) {
    const source = join(repo, "docs", name);
    const link = join(target, "docs", name);
    if (exists(source) && !exists(link)) symlinkSync(source, link);
  }
  console.log(target);
  console.log(`cd '${target.replaceAll("'", "'\\''")}'`);
} catch (error) {
  console.error(error.message);
  process.exit(1);
}
