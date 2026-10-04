// JUS-20 follow-up check: the wizard install step keeps the removed card's guidance.
//
// The repo has no web test runner (no vitest/jsdom), so this script stands in
// for a unit test like scripts/jus11-check.mjs: it asserts the Add server
// wizard's install command carries the full guidance the Servers page card
// used to show, and that the rendered/copied text cannot silently drop it.
//
// Run from web/:  node scripts/jus20-check.mjs

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const wizard = readFileSync(join(root, "src/features/servers/components/AddServerWizard.vue"), "utf8");

const results = [];

function check(name, fn) {
  try {
    fn();
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

/** installLines returns the installCommand array entries from the wizard source. */
function installLines(source) {
  const start = source.indexOf("const installCommand = [");
  assert(start !== -1, "installCommand array not found");
  const end = source.indexOf('].join("\\n")', start);
  assert(end !== -1, "installCommand array end not found");
  const body = source.slice(start, end);
  return [...body.matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((match) => match[1]);
}

check("install command carries the --full flag and its comment", () => {
  const lines = installLines(wizard);
  assert(
    lines.some((line) => line.includes("install-agent.sh --ca ./ca.crt --full")),
    "no --full on the install-agent.sh line",
  );
  assert(
    lines.some((line) => line.includes("--full installs Docker Engine")),
    "no --full explainer comment",
  );
});

check("install command notes the default localhost agent and ends with status", () => {
  const text = installLines(wizard).join("\n");
  assert(text.includes("--no-local-agent"), "no localhost-agent opt-out note");
  assert(
    text.includes("systemctl status gotham-agent"),
    "no systemctl status gotham-agent line",
  );
});

check("copy button and textarea use the full install command", () => {
  assert(
    wizard.includes("await navigator.clipboard.writeText(installCommand)"),
    "copy handler does not write installCommand",
  );
  assert(wizard.includes('@click="copyInstallCommand"'), "copy button not wired");
  assert(wizard.includes(':value="installCommand"'), "textarea not bound to installCommand");
});

check("textarea fits the longer text without clipping", () => {
  const lineCount = installLines(wizard).length;
  const anchor = wizard.indexOf(':value="installCommand"');
  assert(anchor !== -1, "install textarea not found");
  const window = wizard.slice(anchor, anchor + 300);
  const match = /:autosize="\{\s*minRows:\s*(\d+),\s*maxRows:\s*(\d+)\s*\}"/.exec(window);
  assert(match, "install textarea autosize not found");
  const maxRows = Number.parseInt(match[2], 10);
  assert(
    maxRows >= lineCount,
    `maxRows ${maxRows} clips the ${lineCount}-line install command`,
  );
});

const failed = results.filter((result) => !result.ok);
console.log(`\n${results.length - failed.length}/${results.length} checks passed`);
if (failed.length > 0) {
  process.exit(1);
}
