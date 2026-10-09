// Modal scroll contract (JUS-69), structural half.
//
// The header and footer of every card modal stay fixed, only the body
// scrolls, and the modal never exceeds the viewport. jsdom never applies
// stylesheets, so the contract is asserted as CSS text (same pattern as the
// JUS-16/17/18 and JUS-19 suites); modal membership is structural — the
// NModal must carry the contract class. The computed-style half lives in
// modal-scroll.spec.ts (Playwright, no backend) and the real-DOM half in
// environment-layout.spec.ts (Playwright, mocked API).

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainCss = readFileSync(resolve(webRoot, "src/shared/styles/main.css"), "utf8");

/** readSfc returns the raw source of one single-file component. */
function readSfc(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), "utf8");
}

/** ruleBody returns the declaration block of the first rule naming selector. */
function ruleBody(source: string, selector: string): string {
  const at = source.indexOf(selector);
  expect(at, `expected a rule naming ${selector}`).not.toBe(-1);
  const open = source.indexOf("{", at);
  let depth = 1;
  let i = open + 1;
  while (depth > 0) {
    if (i >= source.length) {
      throw new Error(`unbalanced braces after ${selector}`);
    }
    if (source[i] === "{") {
      depth += 1;
    } else if (source[i] === "}") {
      depth -= 1;
    }
    i += 1;
  }
  return source.slice(open + 1, i - 1);
}

describe("JUS-69 modal shell is bounded to the viewport", () => {
  it("caps every card modal below the viewport height in a flex column", () => {
    for (const modal of [".app-modal", ".wizard-modal", ".edit-server-modal"]) {
      const body = ruleBody(mainCss, `${modal}.n-modal.n-card`);
      expect(body, modal).toMatch(/max-height:\s*calc\(100vh\s*-\s*\d+px\)/);
      expect(body, modal).toMatch(/display:\s*flex/);
      expect(body, modal).toMatch(/flex-direction:\s*column/);
    }
  });

  it("pins the header and any footer outside the scrolling region", () => {
    expect(mainCss).toMatch(/\.app-modal\.n-modal\.n-card\s*>\s*\.n-card-header/);
    expect(mainCss).toMatch(/\.wizard-modal\.n-modal\.n-card\s*>\s*\.n-card-header/);
    expect(mainCss).toMatch(/\.edit-server-modal\.n-modal\.n-card\s*>\s*\.n-card__footer/);
    expect(mainCss).toMatch(/\.edit-server-modal\.n-modal\.n-card\s*>\s*\.n-card__action/);
    const pinned = ruleBody(mainCss, ".app-modal.n-modal.n-card > .n-card-header");
    expect(pinned).toMatch(/flex:\s*0\s*0\s*auto/);
  });
});

describe("JUS-69 plain card modals scroll only the body", () => {
  it("scrolls .n-card-content for the Add resource and edit modals", () => {
    const body = ruleBody(mainCss, ".app-modal.n-modal.n-card > .n-card-content");
    expect(body).toMatch(/overflow-y:\s*auto/);
    expect(body).toMatch(/min-height:\s*0/);
  });

  it("applies the contract class to the Add resource modal", () => {
    const source = readSfc("src/features/projects/pages/EnvironmentPage.vue");
    const classAt = source.indexOf("app-modal");
    expect(classAt, "expected the app-modal class on the Add resource NModal").not.toBe(-1);
    const block = source.slice(Math.max(0, classAt - 400), classAt + 200);
    expect(block).toContain("<NModal");
    expect(block).toContain('preset="card"');
  });
});

describe("JUS-69 wizard modals pin the footer and scroll the step body", () => {
  it("fixes the card body and lets only .wizard-body scroll", () => {
    const body = ruleBody(mainCss, ".wizard-modal.n-modal.n-card > .n-card-content");
    expect(body).toMatch(/overflow:\s*hidden/);
    expect(body).not.toMatch(/overflow-y:\s*auto/);
    expect(body).toMatch(/max-height:\s*none/);
    const wizard = ruleBody(mainCss, ".wizard-modal.n-modal.n-card > .n-card-content > .wizard");
    expect(wizard).toMatch(/flex:\s*1\s*1\s*auto/);
    expect(wizard).toMatch(/min-height:\s*0/);
  });

  it("keeps the step body scrollable and the footer pinned in CreateAppWizard", () => {
    const source = readSfc("src/features/applications/components/CreateAppWizard.vue");
    expect(source).toContain("wizard-modal");
    expect(source).toMatch(/\.wizard-body\s*\{[^}]*overflow-y:\s*auto/);
    expect(source).toMatch(/\.wizard-foot\s*\{[^}]*flex:\s*0\s*0\s*auto/);
    const bodyAt = source.indexOf("wizard-body");
    const footAt = source.indexOf("wizard-foot", bodyAt);
    expect(footAt, "expected .wizard-foot after .wizard-body in the template").not.toBe(-1);
  });

  it("no longer scrolls the whole wizard card away with the step", () => {
    const source = readSfc("src/features/applications/components/CreateAppWizard.vue");
    expect(source).not.toMatch(/:deep\(\.n-card-content\)\s*\{[^}]*overflow-y:\s*auto/);
  });
});
