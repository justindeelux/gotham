// JUS-11 regression check: rail status dot, edit-modal hint gap, wizard grid.
//
// The repo has no web test runner (no vitest/jsdom), so this script stands in
// for a unit test like scripts/focus-style-check.mjs: it asserts the shared
// styles and markup that keep the rail monogram readable and the Edit modal
// hints on the same gap as the Add wizard.
//
// Run from web/:  node scripts/jus11-check.mjs

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const css = readFileSync(join(root, "src/styles/main.css"), "utf8");
const rail = readFileSync(join(root, "src/components/ServerRail.vue"), "utf8");
const edit = readFileSync(join(root, "src/components/EditServerModal.vue"), "utf8");
const wizard = readFileSync(join(root, "src/components/AddServerWizard.vue"), "utf8");

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

/** ruleBody returns the declaration block of the first rule containing sel. */
function ruleBody(source, sel) {
  const index = source.indexOf(sel);
  assert(index !== -1, `selector not found: ${sel}`);
  const open = source.indexOf("{", index);
  const close = source.indexOf("}", open);
  assert(open !== -1 && close !== -1, `malformed rule for: ${sel}`);
  return source.slice(open + 1, close);
}

/** pxValue reads a `prop: Npx` number out of a declaration block. */
function pxValue(body, prop) {
  const match = new RegExp(`${prop}\\s*:\\s*(\\d+)px`).exec(body);
  assert(match, `no ${prop} in px found`);
  return Number.parseInt(match[1], 10);
}

// The 48px rail button centers a two-letter monogram, so the 10px dot (plus
// its 3px ring: 16px across) must hug the button corner: with right/bottom
// offsets above 8px its box covers the button center and the second letter.
check("rail status dot hugs the button corner, clear of the monogram", () => {
  const body = ruleBody(rail, ".rail-dot");
  const right = pxValue(body, "right");
  const bottom = pxValue(body, "bottom");
  assert(right <= 8, `dot right ${right}px covers the monogram center`);
  assert(bottom <= 8, `dot bottom ${bottom}px covers the monogram center`);
});

// Both the Add wizard and the Edit modal render hints as .field-hint on its
// own line below the field, with the gap from the single shared rule: no
// component may carry its own hint margin.
check("edit modal and wizard share one hint gap", () => {
  // The trailing " {" skips the comment that names the pattern.
  const body = ruleBody(css, ".field-hint {");
  assert(/margin-top\s*:\s*var\(--space-1\)/.test(body), "shared hint gap missing");
  for (const [name, source] of [["EditServerModal", edit], ["AddServerWizard", wizard]]) {
    assert(source.includes('class="field-hint"'), `${name} lost its hint spans`);
    const styles = source.slice(source.indexOf("<style"));
    assert(!/field-hint/.test(styles), `${name} overrides the shared hint gap`);
  }
});

// The IP/port row is a grid (not a wrapping flex), so the fields stay on one
// row whether or not the modal shows a scrollbar (JUS-13): both modals share
// the rule.
check("wizard and edit modal address rows share the grid layout", () => {
  assert(css.includes(".wizard-modal .addr-row"), "wizard addr-row rule missing");
  assert(css.includes(".edit-server-modal .addr-row"), "edit modal addr-row rule missing");
  const body = ruleBody(css, ".wizard-modal .addr-row");
  assert(/display\s*:\s*grid/.test(body), "address row is not a grid");
});

const failed = results.filter((result) => !result.ok);
if (failed.length > 0) {
  console.error(`jus11-check: ${failed.length} check(s) failed`);
  process.exit(1);
}
console.log("jus11-check: all checks passed");
