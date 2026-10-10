// JUS-88 UI polish: disconnect modal width, certificate table columns and
// sidebar footer alignment. jsdom never applies stylesheets, so widths and
// alignment are asserted from SFC source text plus structural mounts (same
// pattern as the JUS-16/17/18 and JUS-74 suites).
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/** readSfc returns the raw source of one single-file component. */
function readSfc(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), "utf8");
}

/** modalWidthPx reads the NModal style width in pixels. */
function modalWidthPx(source: string): number {
  const match = source.match(/width:\s*(\d+)px/);
  expect(match, "expected an NModal style width in px").not.toBeNull();
  return Number(match?.[1] ?? Number.NaN);
}

describe("JUS-88 disconnect git source modal is compact", () => {
  const source = readSfc(
    "src/features/applications/components/GitSourceDisconnectDialog.vue",
  );

  it("reuses the shared .app-modal scroll contract", () => {
    expect(source).toContain("app-modal");
    expect(source).not.toContain("dialog-card");
  });

  it("is narrow (<= 440px)", () => {
    const width = modalWidthPx(source);
    expect(width).toBeLessThanOrEqual(440);
    expect(width).toBeGreaterThanOrEqual(320);
  });

  it("keeps actions in a right-aligned footer with primary last", () => {
    expect(source).toContain("#footer");
    expect(source).toContain('justify="end"');
    const footerAt = source.indexOf("#footer");
    const cancelAt = source.indexOf("applications.gitSources.cancel", footerAt);
    const confirmAt = source.indexOf(
      "applications.gitSources.disconnectConfirm",
      footerAt,
    );
    expect(cancelAt).toBeGreaterThan(-1);
    expect(confirmAt).toBeGreaterThan(cancelAt);
  });
});

describe("JUS-88 certificate configurations table keeps four columns", () => {
  const source = readSfc("src/features/domains/components/CertificatesPanel.vue");

  it("renders Domain, Status, Expires and Actions only", () => {
    for (const key of ["domain", "status", "not_after", "actions"]) {
      expect(source).toContain(`key: "${key}"`);
    }
    for (const key of ["challenge", "dns_provider_id", "wildcard", "enabled", "updated_at"]) {
      expect(source).not.toContain(`key: "${key}"`);
    }
  });
});

describe("JUS-88 follow-up certificate hints render below their field", () => {
  const source = readSfc("src/features/domains/components/CertificateForm.vue");

  it("uses the shared .field-hint pattern, never a side-by-side .hint", () => {
    expect(source).not.toMatch(/class="hint"/);
    expect(source).toContain("field-hint");
  });

  it("wraps the Naive input row so hints drop below the control", () => {
    expect(source).toMatch(/\.certificate-form\s*:deep\(\.n-form-item-blank\)\s*\{[^}]*flex-wrap:\s*wrap/);
  });
});

describe("JUS-88 follow-up certificate cells stack with a gap", () => {
  const source = readSfc("src/features/domains/components/CertificatesPanel.vue");

  it("lays out two-line cells inline: column flex with a token gap", () => {
    // The cells render via h() inside a computed, outside the scoped-style
    // scope, so the layout must ride inline (regression: scoped .cell-main
    // never matched and the lines rendered glued together).
    expect(source).toContain("cellStackStyle");
    expect(source).toMatch(/flexDirection:\s*"column"/);
    expect(source).toMatch(/gap:\s*"var\(--space-1\)"/);
    expect(source).not.toContain(".cell-main {");
  });
});

describe("JUS-88 follow-up footer chevron is right-aligned", () => {
  const source = readSfc("src/app/layouts/AccountMenu.vue");

  it("keeps the trigger content left-aligned", () => {
    expect(source).toMatch(/\.me-card\s*\{[^}]*justify-content:\s*flex-start/);
    expect(source).toMatch(/\.me-card\s*\{[^}]*text-align:\s*left/);
    expect(source).toMatch(/\.me-meta\s*\{[^}]*text-align:\s*left/);
    expect(source).toMatch(/\.me-meta\s*\{[^}]*align-items:\s*flex-start/);
  });

  it("renders NSpace children as direct flex items and pushes the chevron right", () => {
    // NSpace wraps children in anonymous divs by default, which swallowed
    // the inner flex rules (regression: the chevron sat after the name).
    expect(source).toContain(':wrap-item="false"');
    expect(source).toMatch(/\.me-chevron\s*\{[^}]*margin-left:\s*auto/);
  });
});

describe("JUS-88 follow-up menus breathe with a spacing token", () => {
  it("gaps sidebar entries with a token", () => {
    const source = readSfc("src/app/layouts/AppSidebar.vue");
    expect(source).toMatch(/\.sidebar-body\s*\{[^}]*gap:\s*var\(--space-1\)/);
  });

  it("gaps language options with a token on that menu only", () => {
    const source = readSfc("src/shared/ui/LanguageSelect.vue");
    expect(source).toContain("language-menu");
    expect(source).toMatch(/\.language-menu\.n-dropdown-menu\)?\s*\{[^}]*gap:\s*var\(--space-1\)/);
  });
});
