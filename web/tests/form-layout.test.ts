// Form row layout contract (JUS-19 + fix round 1), component half.
//
// Short fields share one row via the shared `.form-row` grid utility and
// collapse to a single column once their container narrows below ~600px
// (container query, not viewport). jsdom never applies stylesheets, so the
// collapse rule itself is asserted as CSS text (same pattern as the
// JUS-16/17/18 suite); row membership is structural — each pair must sit
// inside the SAME .form-row block, found with a tag-matching scan, so the
// tests fail when fields are merely stacked in order. Real-DOM mounts cover
// the store-free components. The computed-style half lives in
// form-layout.spec.ts (Playwright, no backend).

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";

import CertificateForm from "../src/features/domains/components/CertificateForm.vue";
import domainsEn from "../src/features/domains/locales/en";
import domainsVi from "../src/features/domains/locales/vi";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";
import DynamicForm from "../src/features/templates/components/DynamicForm.vue";
import EnvEditor from "../src/features/applications/components/EnvEditor.vue";
import type { CertificateDraft } from "../src/features/domains/api/proxy";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

/** mountWithI18n provides the composer every localized component requires. */
function mountWithI18n(component: unknown, options: Record<string, unknown>) {
  return mount(component as never, {
    ...(options as object),
    global: { plugins: [i18n] },
  } as never) as VueWrapper<never>;
}

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainCss = readFileSync(resolve(webRoot, "src/shared/styles/main.css"), "utf8");

/** Feature catalogs render through the real composer in mounted checks. */
beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { domains: domainsEn });
  i18n.global.mergeLocaleMessage("vi", { domains: domainsVi });
  syncComposerLocale("en");
});

/** readSfc returns the raw source of one single-file component. */
function readSfc(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), "utf8");
}

/**
 * rowBlocks returns the inner HTML of each `<div class="...">` block with the
 * given class, in document order. A depth-counting scan pairs each opener
 * with its matching closer, so nested divs (field-stack, strength-row) stay
 * inside their row. Throws on unbalanced markup.
 */
export function rowBlocks(source: string, rowClass = "form-row"): string[] {
  const blocks: string[] = [];
  const openTag = `<div class="${rowClass}`;
  let cursor = 0;
  while (true) {
    const open = source.indexOf(openTag, cursor);
    if (open === -1) {
      return blocks;
    }
    const openEnd = source.indexOf(">", open);
    let depth = 1;
    let i = openEnd + 1;
    while (depth > 0) {
      const nextOpen = source.indexOf("<div", i);
      const nextClose = source.indexOf("</div>", i);
      if (nextClose === -1) {
        throw new Error(`unbalanced divs after offset ${open}`);
      }
      if (nextOpen !== -1 && nextOpen < nextClose) {
        const tagEnd = source.indexOf(">", nextOpen);
        if (source[tagEnd - 1] === "/") {
          i = tagEnd + 1;
          continue;
        }
        depth += 1;
        i = tagEnd + 1;
      } else {
        depth -= 1;
        i = nextClose + "</div>".length;
      }
    }
    blocks.push(source.slice(openEnd + 1, i - "</div>".length));
    cursor = i;
  }
}

/**
 * expectPair asserts markers a and b sit inside the SAME row block, with a
 * first — i.e. the fields share one visual row in tab order, not merely
 * adjacent stacked rows.
 */
function expectPair(source: string, a: string, b: string, rowClass = "form-row"): void {
  const hit = rowBlocks(source, rowClass).some((block) => {
    const first = block.indexOf(a);
    return first !== -1 && block.indexOf(b, first) !== -1;
  });
  expect(hit, `expected "${a}" and "${b}" in the same .${rowClass} block`).toBe(true);
}

/**
 * mediaBlocksMentioning returns the @media bodies that name a selector, so
 * viewport fallbacks can be scoped to one rule instead of the whole file.
 */
export function mediaBlocksMentioning(source: string, selector: string): string[] {
  const hits: string[] = [];
  let cursor = 0;
  while (true) {
    const at = source.indexOf("@media", cursor);
    if (at === -1) {
      return hits;
    }
    const open = source.indexOf("{", at);
    let depth = 1;
    let i = open + 1;
    while (depth > 0) {
      if (i >= source.length) {
        throw new Error("unbalanced braces in @media rule");
      }
      if (source[i] === "{") {
        depth += 1;
      } else if (source[i] === "}") {
        depth -= 1;
      }
      i += 1;
    }
    const body = source.slice(open + 1, i - 1);
    if (body.includes(selector)) {
      hits.push(body);
    }
    cursor = i;
  }
}

/** rowTexts returns the text of each .form-row in DOM order. */
function rowTexts(wrapper: VueWrapper): string[] {
  return wrapper.findAll(".form-row").map((row) => row.text());
}

describe("JUS-19 shared .form-row utility", () => {
  it("lays out two equal columns that collapse per container below 480px", () => {
    expect(mainCss).toContain(".form-container");
    expect(mainCss).toMatch(/\.form-container\s*\{[^}]*container-type:\s*inline-size/);
    expect(mainCss).toMatch(
      /\.form-row\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/,
    );
    expect(mainCss).toMatch(
      /@container\s*\(max-width:\s*480px\)[\s\S]*?\.form-row\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/,
    );
  });

  it("shares one collapse threshold with EnvEditor and the redirect form", () => {
    /** thresholdOf reads the max-width of the first @container rule. */
    function thresholdOf(source: string): string {
      const match = source.match(/@container\s*\(max-width:\s*(\d+px)\)/);
      expect(match, "expected one @container collapse rule").not.toBeNull();
      return match?.[1] ?? "";
    }
    const shared = thresholdOf(mainCss);
    expect(shared).toBe("480px");
    expect(thresholdOf(readSfc("src/features/applications/components/EnvEditor.vue"))).toBe(shared);
    expect(thresholdOf(readSfc("src/features/domains/components/RedirectCreateCard.vue"))).toBe(shared);
  });

  it("has no viewport fallback for the row collapse", () => {
    // Scoped to @media bodies mentioning .form-row: unrelated future media
    // queries elsewhere in main.css must not fail this test.
    expect(mediaBlocksMentioning(mainCss, ".form-row")).toEqual([]);
  });

  it("gives rows no item margins and lets a lone item span both columns", () => {
    expect(mainCss).toMatch(/\.form-row\s*>\s*\.n-form-item\s*\{[^}]*margin:\s*0/);
    expect(mainCss).toMatch(/\.form-row\s*>\s*:only-child\s*\{[^}]*grid-column:\s*1\s*\/\s*-1/);
  });

  it("reuses the shared rhythm tokens instead of fixed pixel widths", () => {
    const block = mainCss.match(/\.form-row\s*\{[^}]*\}/)?.[0] ?? "";
    expect(block).toContain("var(--form-item-gap)");
    expect(block).toContain("var(--space-3)");
    expect(block).not.toMatch(/\d+px/);
  });
});

describe("JUS-19 CertificateForm rows", () => {
  const draft: CertificateDraft = {
    application_id: "",
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    enabled: true,
  };

  /** mountForm mounts the certificate editor with no options to pick. */
  function mountForm() {
    return mount(CertificateForm, {
      props: { modelValue: draft, applications: [], providers: [] },
      global: { plugins: [i18n] },
    });
  }

  it("pairs Application|Domain, Challenge|DNS provider, Wildcard|Enabled", () => {
    const rows = rowTexts(mountForm());
    expect(rows).toHaveLength(3);
    expect(rows[0]).toContain("Application");
    expect(rows[0]).toContain("Domain");
    expect(rows[1]).toContain("Challenge");
    expect(rows[1]).toContain("DNS provider");
    expect(rows[2]).toContain("Wildcard");
    expect(rows[2]).toContain("Enabled");
  });

  it("keeps labels visible and the tab order left to right, top to bottom", () => {
    const wrapper = mountForm();
    const labels = wrapper.findAll(".n-form-item-label").map((node) => node.text());
    expect(labels).toEqual([
      "Application",
      "Domain (from the application)",
      "Challenge",
      "DNS provider",
      "Wildcard",
      "Enabled",
    ]);
  });

  it("establishes its own container so 560px modals collapse", () => {
    expect(mountForm().find("form").classes()).toContain("form-container");
  });
});

describe("JUS-19 EnvEditor single-row variables", () => {
  it("renders each variable as one row: name | value | delete", () => {
    const wrapper = mountWithI18n(EnvEditor, {
      props: { modelValue: [{ key: "NODE_ENV", value: "production" }] },
    });
    const rows = wrapper.findAll(".env-editor__row");
    expect(rows).toHaveLength(1);
    const row = rows[0];
    expect(row.find('[aria-label="Variable name"]').exists()).toBe(true);
    expect(row.find('[aria-label="Variable value"]').exists()).toBe(true);
    expect(row.find('[aria-label="Remove variable"]').exists()).toBe(true);
  });

  it("keeps Add variable, validation and the secret: prefix behaviour", () => {
    const wrapper = mountWithI18n(EnvEditor, {
      props: {
        modelValue: [
          { key: "bad name", value: "x" },
          { key: "DB_URL", value: "secret:db-url" },
        ],
      },
    });
    expect(wrapper.findAll(".env-editor__row")).toHaveLength(2);
    expect(wrapper.find(".env-editor__secret").text()).toContain("sealed secret");
    expect(wrapper.text()).toContain("Add variable");
  });

  it("lays out the row as a non-wrapping grid with a container fallback", () => {
    const source = readSfc("src/features/applications/components/EnvEditor.vue");
    expect(source).toMatch(
      /\.env-editor__row\s*\{[^}]*display:\s*grid[^}]*grid-template-columns:\s*minmax\(140px,\s*220px\)\s*minmax\(0,\s*1fr\)\s*auto/,
    );
    expect(source).not.toContain("flex-wrap");
    expect(source).not.toContain("@media");
    expect(source).toMatch(/\.env-editor\s*\{[^}]*container-type:\s*inline-size/);
    expect(source).toMatch(
      /@container\s*\(max-width:\s*480px\)[\s\S]*?\.env-editor__row\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/,
    );
  });
});

describe("JUS-19 DynamicForm stays two columns", () => {
  it("keeps the 2-column grid with the single-column fallback and schema order", () => {
    const source = readSfc("src/features/templates/components/DynamicForm.vue");
    expect(source).toMatch(
      /\.dynamic-form\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/,
    );
    expect(source).toMatch(
      /@media\s*\(max-width:\s*720px\)[\s\S]*?\.dynamic-form\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/,
    );
    const wrapper = mount(DynamicForm, {
      props: {
        fields: [
          { key: "a", label: "Alpha", type: "text" },
          { key: "b", label: "Beta", type: "text" },
          { key: "c", label: "Gamma", type: "text" },
        ],
        modelValue: {},
        errors: {},
      },
    });
    const labels = wrapper.findAll(".n-form-item-label").map((node) => node.text());
    expect(labels).toEqual(["Alpha", "Beta", "Gamma"]);
  });
});

describe("JUS-19 AddServerWizard rows", () => {
  // The connect step lives in WizardConnectStep (JUS-24 split); the shell
  // keeps the modal chrome, rail, install step and footer.
  const source = readSfc("src/features/servers/components/wizard/WizardConnectStep.vue");

  it("pairs Node name|SSH user in one row", () => {
    expectPair(source, "add-server-name", "add-server-ssh-user");
  });

  it("keeps Authentication and the key-mode selector on full-width rows", () => {
    // Measured with the real Naive controls: the nowrap key-mode group needs
    // 294px but a half column at the real 590px container is 289px, so the
    // pair would clip by 5px. Neither control may share a row.
    const inRow = rowBlocks(source).some(
      (block) =>
        block.includes("Authentication method") || block.includes("SSH key mode"),
    );
    expect(inRow).toBe(false);
  });

  it("keeps Key name on its own row", () => {
    const inRow = rowBlocks(source).some((block) => block.includes('label="Key name"'));
    expect(inRow).toBe(false);
  });

  it("keeps every locator id and label", () => {
    for (const name of ["add-server-name", "add-server-ssh-user", "add-server-ip", "add-server-port"]) {
      expect(source).toContain(name);
    }
  });
});

describe("JUS-19 fix 1 EditServerModal: Credentials on its own row", () => {
  const source = readSfc("src/features/servers/components/EditServerModal.vue");

  it("uses no .form-row: no segmented control shares a ~250px column", () => {
    expect(rowBlocks(source)).toHaveLength(0);
  });

  it("keeps the logical field order with Credentials under its own heading", () => {
    const order = ["Node name", "IP address or hostname", "SSH port", "SSH user", "Credential change"];
    let cursor = -1;
    for (const label of order) {
      const next = source.indexOf(label, cursor + 1);
      expect(next, `expected ${label} after position ${cursor}`).toBeGreaterThan(cursor);
      cursor = next;
    }
    const credentialsHeading = source.indexOf('aria-label="Credentials"');
    expect(source.indexOf("Credential change", credentialsHeading)).toBeGreaterThan(
      credentialsHeading,
    );
  });
});

describe("JUS-19 CreateAppWizard rows", () => {
  const sourceStep = readSfc("src/features/applications/components/WizardSourceStep.vue");
  const runtimeStep = readSfc("src/features/applications/components/WizardRuntimeStep.vue");

  it("pairs Provider|Repository in one row", () => {
    expectPair(sourceStep, "applications.wizard.providerPlaceholder", "applications.wizard.repositoryPlaceholder");
  });

  it("pairs Branch|Application name in a grid row, not a wrapping flex", () => {
    expectPair(sourceStep, "applications.wizard.branch", "applications.wizard.appName");
    expect(sourceStep).not.toMatch(/<NSpace[^>]*>\s*<NFormItem[^>]*wizard\.branch"/);
  });

  it("pairs Node|Domain and Internal port|Host port in their own rows", () => {
    expectPair(runtimeStep, "applications.wizard.node", "app.gotham.dev");
    expectPair(runtimeStep, "applications.wizard.internalPort", "applications.wizard.hostPort");
  });
});

describe("JUS-19 CreateDatabaseWizard rows", () => {
  it("pairs Version|Node in one row", () => {
    expectPair(readSfc("src/features/databases/components/CreateDatabaseWizard.vue"), 'label="Version"', 'label="Node"');
  });
});

describe("JUS-19 DomainsPage rows", () => {
  const providerSource = readSfc("src/features/domains/components/ProviderDialog.vue");
  const redirectSource = readSfc("src/features/domains/components/RedirectCreateCard.vue");

  it("pairs Provider|Name in one row of the DNS provider modal", () => {
    expectPair(providerSource, "domains.providerDialog.provider", "domains.providerDialog.name");
  });

  it("puts Preserve path|Enabled beside the submit button", () => {
    expectPair(redirectSource, "domains.redirects.preserveAria", "domains.redirects.addRedirect", "redirect-form__bottom");
    expectPair(redirectSource, "domains.redirects.enabledNowAria", "domains.redirects.addRedirect", "redirect-form__bottom");
  });

  it("collapses the redirect bottom row per container, not viewport", () => {
    expect(redirectSource).not.toMatch(/@media[^{]*\{[^}]*\.redirect-form__bottom/);
    expect(redirectSource).toMatch(
      /@container\s*\(max-width:\s*480px\)[\s\S]*?\.redirect-form__bottom/,
    );
  });
});

describe("JUS-19 NotificationsPage rows", () => {
  const source = readSfc("src/features/notifications/components/ChannelFormDialog.vue");

  it("pairs Name|Kind in one row", () => {
    expectPair(source, "notifications.dialog.name", "notifications.dialog.kind");
  });

  it("pairs Resource scope|Enabled in one row", () => {
    expectPair(source, "notifications.dialog.resourceScope", "notifications.dialog.enabled");
  });
});

describe("JUS-19 RegisterPage rows", () => {
  it("pairs Password|Confirm password in one row", () => {
    expectPair(
      readSfc("src/features/auth/pages/RegisterPage.vue"),
      "register-password",
      "register-confirm-password",
    );
  });

  it("keys the collapse off the page width, not the 420px card", () => {
    // The card itself is narrower than the collapse threshold, so it cannot
    // be the container — otherwise the required pairing would never render.
    expect(mainCss).toMatch(/\.auth-page\s*\{[^}]*container-type:\s*inline-size/);
  });
});
