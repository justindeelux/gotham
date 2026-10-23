#!/usr/bin/env node
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, realpathSync, symlinkSync, writeFileSync } from "node:fs";
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

try {
  const common = git(process.cwd(), "rev-parse", "--path-format=absolute", "--git-common-dir");
  const repo = realpathSync(dirname(common));
  const target = join(dirname(repo), `${basename(repo)}-${mode ? branch.replace("/", "-") : "work"}`);
  git(repo, "fetch", "origin");
  git(repo, "rev-parse", "--verify", "origin/main");
  if (spawnSync("git", ["-C", repo, "show-ref", "--verify", "--quiet", `refs/heads/${branch}`]).status === 0) {
    throw new Error(`Branch ${branch} already exists; resume its checkout explicitly`);
  }

  if (existsSync(target)) {
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
      if (git(target, "rev-parse", "@{upstream}") !== head) {
        throw new Error("Previous branch has unpushed commits; preserve it before reusing this checkout");
      }
      const previous = git(target, "symbolic-ref", "--short", "HEAD");
      const prs = JSON.parse(execFileSync("gh", [
        "pr", "list", "--head", previous, "--base", "main", "--state", "merged",
        "--json", "headRefOid", "--limit", "100",
      ], { cwd: target, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }));
      if (!prs.some((pr) => pr.headRefOid === head)) {
        throw new Error("Previous branch has no merged PR at its current head");
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
    if (existsSync(source) && !existsSync(link)) symlinkSync(source, link);
  }
  const config = join(target, "opencode.json");
  if (!existsSync(config)) {
    writeFileSync(config, JSON.stringify({
      "$schema": "https://opencode.ai/config.json",
      model: "opencode-go/deepseek-v4.1-flash",
    }, null, 2) + "\n", { flag: "wx" });
  }
  console.log(target);
  console.log(`cd '${target.replaceAll("'", "'\\''")}'`);
} catch (error) {
  console.error(error.message);
  process.exit(1);
}
