
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const css = readFileSync(join(root, "src/shared/styles/main.css"), "utf8");
const rail = readFileSync(join(root, "src/features/servers/components/ServerRail.vue"), "utf8");
const edit = readFileSync(join(root, "src/features/servers/components/EditServerModal.vue"), "utf8");
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

function ruleBody(source, sel) {
  const index = source.indexOf(sel);
  assert(index !== -1, `selector not found: ${sel}`);
  const open = source.indexOf("{", index);
  const close = source.indexOf("}", open);
  assert(open !== -1 && close !== -1, `malformed rule for: ${sel}`);
  return source.slice(open + 1, close);
}

function pxValue(body, prop) {
  const match = new RegExp(`${prop}\\s*:\\s*(\\d+)px`).exec(body);
  assert(match, `no ${prop} in px found`);
  return Number.parseInt(match[1], 10);
}

check("rail status dot hugs the button corner, clear of the monogram", () => {
  const body = ruleBody(rail, ".rail-dot");
  const right = pxValue(body, "right");
  const bottom = pxValue(body, "bottom");
  assert(right <= 8, `dot right ${right}px covers the monogram center`);
  assert(bottom <= 8, `dot bottom ${bottom}px covers the monogram center`);
});

check("edit modal and wizard share one hint gap", () => {
  const body = ruleBody(css, ".field-hint {");
  assert(/margin-top\s*:\s*var\(--space-1\)/.test(body), "shared hint gap missing");
  for (const [name, source] of [["EditServerModal", edit], ["AddServerWizard", wizard]]) {
    if (name === "EditServerModal") {
      assert(source.includes('class="field-hint"'), `${name} lost its hint spans`);
    }
    const styles = source.slice(source.indexOf("<style"));
    assert(!/field-hint/.test(styles), `${name} overrides the shared hint gap`);
  }
});

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
