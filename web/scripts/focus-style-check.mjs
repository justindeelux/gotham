// Single-border focus regression check (JUS-13).
//
// A focused field must show exactly one border: its own border in the accent
// color, with no extra glow ring (box-shadow), no outline on the inner input
// element, and a still-visible :focus-visible indicator elsewhere. This script
// stands in for a unit test (the repo has no web test runner): it asserts the
// shared-style rules exist and that the focus border color keeps >= 3:1
// contrast against the input surface.
//
// Run from web/:  node scripts/focus-style-check.mjs

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const css = readFileSync(join(root, "src/styles/main.css"), "utf8");
const tokens = readFileSync(join(root, "src/styles/tokens.css"), "utf8");
const appVue = readFileSync(join(root, "src/App.vue"), "utf8");

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

function hexToRgb(hex) {
  const match = /^#([0-9a-f]{6})$/i.exec(hex.trim());
  assert(match, `not a 6-digit hex color: ${hex}`);
  const value = Number.parseInt(match[1], 16);
  return [(value >> 16) & 255, (value >> 8) & 255, value & 255];
}

function luminance([r, g, b]) {
  const linear = (channel) => {
    const s = channel / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b);
}

function contrast(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

// NInputNumber renders an inner NInput, so one shared override covers NInput,
// NInputNumber and the password fields on every form.
check("focused input state-border has no glow ring", () => {
  const body = ruleBody(css, ".n-input.n-input--focus .n-input__state-border");
  assert(/box-shadow\s*:\s*none/.test(body), "expected box-shadow: none on focus");
});

check("inner input elements draw no second outline on focus-visible", () => {
  assert(css.includes(".n-input__input-el:focus-visible"), "missing input-el focus-visible rule");
  assert(css.includes(".n-input__textarea-el:focus-visible"), "missing textarea-el focus-visible rule");
  const probe = ruleBody(css, ".n-input__input-el:focus-visible");
  assert(/outline\s*:\s*none/.test(probe), "expected outline: none on inner input focus");
});

check("a visible keyboard focus indicator is kept elsewhere", () => {
  const body = ruleBody(css, ":focus-visible");
  assert(/outline\s*:/.test(body), "the global :focus-visible outline must stay");
});

check("focus border uses the accent color (not a removed indicator)", () => {
  const match = /borderFocus:\s*"([^"]+)"/.exec(appVue);
  assert(match, "Input.borderFocus override not found in App.vue");
  assert(/var\(--accent-ink\)|#[0-9a-f]{6}/i.test(match[1]), `unexpected borderFocus: ${match[1]}`);
});

check("focus border color keeps >= 3:1 contrast on the input surface", () => {
  const accent = /--accent:\s*(#[0-9a-f]{6})/i.exec(tokens)?.[1];
  assert(accent, "--accent token not found");
  assert(/--accent-ink:\s*color-mix\(in oklab,\s*var\(--accent\)\s*([\d.]+)%\s*,\s*white\)/.test(tokens), "--accent-ink mix not found");
  const weight = Number.parseFloat(/--accent-ink:\s*color-mix\(in oklab,\s*var\(--accent\)\s*([\d.]+)%/.exec(tokens)[1]) / 100;
  // sRGB-space approximation of the oklab mix; the assertion margin is wide.
  const mixed = hexToRgb(accent).map((channel) => Math.round(channel * weight + 255 * (1 - weight)));
  const inputBg = hexToRgb("#1e1f22");
  const ratio = contrast(mixed, inputBg);
  console.log(`    accent-ink ≈ rgb(${mixed.join(", ")}) vs #1e1f22 → ${ratio.toFixed(2)}:1`);
  assert(ratio >= 3, `focus border contrast ${ratio.toFixed(2)}:1 is below 3:1`);
});

const failed = results.filter((item) => !item.ok);
console.log(`focus-style-check: ${results.length - failed.length}/${results.length} passed`);
if (failed.length > 0) {
  process.exit(1);
}
