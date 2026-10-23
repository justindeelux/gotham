import { execFileSync } from "node:child_process";
import { appendFileSync, readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const jobNames = [
  "build-test", "lint", "web", "sqlc", "proto", "db-test", "installer", "site", "e2e", "ui-e2e",
];

export function selectJobs(paths, full = false) {
  const jobs = Object.fromEntries(jobNames.map((name) => [name, full]));
  const enable = (...names) => names.forEach((name) => { jobs[name] = true; });
  const backend = () => enable("build-test", "lint", "db-test", "e2e", "ui-e2e");

  for (const path of paths) {
    if (path.startsWith("site/")) {
      enable("site");
    } else if (path.startsWith("web/") || path.startsWith("internal/server/webdist/")) {
      enable("build-test", "lint", "web", "ui-e2e");
    } else if (path.startsWith("internal/store/")) {
      backend();
      enable("sqlc");
    } else if (path.startsWith("proto/")) {
      backend();
      enable("proto");
    } else if (path.startsWith("deploy/")) {
      enable("build-test", "lint", "installer");
    } else if (/^(agent|internal|cmd|updatecore|buildtool|composeguard|toolcatalog|templates)\//.test(path)) {
      backend();
    } else if (path.startsWith("docs/") || ["README.md", "AGENTS.md"].includes(path)) {
      // Reference docs do not change the application or public documentation build.
      continue;
    } else {
      enable(...jobNames);
    }
  }
  return jobs;
}

export function checkGate(needs) {
  if (needs.changes?.result !== "success") {
    throw new Error("CI scope discovery did not succeed");
  }
  if (needs.changes.outputs.ready !== "true") {
    throw new Error("Draft PR: finish local review and mark ready to run required checks");
  }
  const selected = JSON.parse(needs.changes.outputs.jobs);
  if (Object.keys(selected).length !== jobNames.length) {
    throw new Error("Incomplete CI job selection");
  }
  for (const name of jobNames) {
    if (typeof selected[name] !== "boolean") {
      throw new Error(`Missing CI selection for ${name}`);
    }
    const result = needs[name]?.result;
    if (result !== "success" && !(selected[name] === false && result === "skipped")) {
      throw new Error(`${name}: ${result ?? "missing"}`);
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv[2] === "gate") {
    checkGate(JSON.parse(process.env.CI_NEEDS));
    console.log("All required checks passed");
  } else {
    const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, "utf8"));
    const isPR = process.env.GITHUB_EVENT_NAME === "pull_request";
    // Full history and NUL-delimited paths cover large diffs, renames and unusual filenames.
    const paths = isPR ? execFileSync("git", [
      "diff", "--name-only", "--no-renames", "-z",
      `${event.pull_request.base.sha}...${event.pull_request.head.sha}`, "--",
    ], { encoding: "utf8", maxBuffer: 32 * 1024 * 1024 }).split("\0").filter(Boolean) : [];
    const jobs = selectJobs(paths, !isPR);
    const ready = !isPR || !event.pull_request.draft;
    appendFileSync(process.env.GITHUB_OUTPUT, `jobs=${JSON.stringify(jobs)}\nready=${ready}\n`);
    appendFileSync(process.env.GITHUB_STEP_SUMMARY,
      `## CI scope\n\n${ready ? "Ready" : "Draft: expensive checks deferred"}\n\n` +
      Object.entries(jobs).map(([name, selected]) => `- ${name}: ${selected ? "run" : "skip"}\n`).join(""));
  }
}
