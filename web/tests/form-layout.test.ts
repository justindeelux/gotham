// Form row layout contract (JUS-19), component half.
//
// Short fields share one row via the shared `.form-row` grid utility and
// collapse to a single column below ~720px. jsdom never applies stylesheets,
// so the collapse rule itself is asserted as CSS text (same pattern as the
// JUS-16/17/18 suite); row membership and tab order are asserted on the real
// Naive UI DOM for the store-free components, and on SFC source text for the
// store/router-wired wizards and pages.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import CertificateForm from "../src/components/CertificateForm.vue";
import DynamicForm from "../src/components/DynamicForm.vue";
import EnvEditor from "../src/components/EnvEditor.vue";
import type { CertificateDraft } from "../src/api/proxy";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainCss = readFileSync(resolve(webRoot, "src/styles/main.css"), "utf8");

/** readSfc returns the raw source of one single-file component. */
function readSfc(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), "utf8");
}

/** rowTexts returns the text of each .form-row in DOM order. */
function rowTexts(wrapper: VueWrapper): string[] {
  return wrapper.findAll(".form-row").map((row) => row.text());
}

describe("JUS-19 shared .form-row utility", () => {
  it("lays out two equal columns that collapse below ~720px", () => {
    expect(mainCss).toContain(".form-row");
    expect(mainCss).toMatch(
      /\.form-row\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/,
    );
    expect(mainCss).toMatch(
      /@media\s*\(max-width:\s*720px\)[\s\S]*?\.form-row\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/,
    );
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
    for (const label of ["Application", "Challenge", "DNS provider", "Wildcard", "Enabled"]) {
      expect(wrapper.text()).toContain(label);
    }
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
});

describe("JUS-19 EnvEditor single-row variables", () => {
  it("renders each variable as one row: name | value | delete", () => {
    const wrapper = mount(EnvEditor, {
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
    const wrapper = mount(EnvEditor, {
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

  it("lays out the row as a non-wrapping grid with a single-column fallback", () => {
    const source = readSfc("src/components/EnvEditor.vue");
    expect(source).toMatch(
      /\.env-editor__row\s*\{[^}]*display:\s*grid[^}]*grid-template-columns:\s*minmax\(140px,\s*220px\)\s*minmax\(0,\s*1fr\)\s*auto/,
    );
    expect(source).not.toContain("flex-wrap");
    expect(source).toMatch(
      /@media\s*\(max-width:\s*720px\)[\s\S]*?\.env-editor__row\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/,
    );
  });
});

describe("JUS-19 DynamicForm stays two columns", () => {
  it("keeps the 2-column grid with the single-column fallback and schema order", () => {
    const source = readSfc("src/components/DynamicForm.vue");
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

describe("JUS-19 wired-form pairings (source structure)", () => {
  it("AddServerWizard pairs Node name|SSH user and Authentication|key mode", () => {
    const source = readSfc("src/components/AddServerWizard.vue");
    expect(source).toContain("form-row");
    const order = [
      "Node name",
      "SSH user",
      "IP address or hostname",
      "SSH port",
      "Authentication",
      "SSH key mode",
      "Key name",
    ];
    let cursor = -1;
    for (const label of order) {
      const next = source.indexOf(label, cursor + 1);
      expect(next, `expected ${label} after position ${cursor}`).toBeGreaterThan(cursor);
      cursor = next;
    }
    for (const name of ["add-server-name", "add-server-ssh-user", "add-server-ip", "add-server-port"]) {
      expect(source).toContain(name);
    }
  });

  it("EditServerModal pairs SSH user|Credentials and keeps IP|port", () => {
    const source = readSfc("src/components/EditServerModal.vue");
    expect(source).toContain("form-row");
    const order = ["Node name", "IP address or hostname", "SSH port", "SSH user", "Credential change"];
    let cursor = -1;
    for (const label of order) {
      const next = source.indexOf(label, cursor + 1);
      expect(next, `expected ${label} after position ${cursor}`).toBeGreaterThan(cursor);
      cursor = next;
    }
  });

  it("CreateAppWizard pairs Provider|Repository, Branch|name, Node|domain, ports", () => {
    const source = readSfc("src/components/CreateAppWizard.vue");
    // The Branch|name pair is a grid now, not an NSpace flex that wrapped.
    expect(source).not.toMatch(/<NSpace[^>]*>\s*<NFormItem label="Branch"/);
    const order = ["Provider", "Repository", "Branch", "Application name", "Node", "Domain (optional)", "Internal port", "Host port"];
    let cursor = -1;
    for (const label of order) {
      const next = source.indexOf(label, cursor + 1);
      expect(next, `expected ${label} after position ${cursor}`).toBeGreaterThan(cursor);
      cursor = next;
    }
  });

  it("CreateDatabaseWizard pairs Version|Node", () => {
    const source = readSfc("src/components/CreateDatabaseWizard.vue");
    expect(source).toContain("form-row");
    expect(source.indexOf("Version")).toBeLessThan(source.indexOf("Node"));
  });

  it("DNS provider modal pairs Provider|Name with Enabled on the last row", () => {
    const source = readSfc("src/pages/DomainsPage.vue");
    const order = ["Provider", "Provider type", "Provider name", "Zones", "Provider credential", "Provider enabled"];
    let cursor = -1;
    for (const label of order) {
      const next = source.indexOf(label, cursor + 1);
      expect(next, `expected ${label} after position ${cursor}`).toBeGreaterThan(cursor);
      cursor = next;
    }
  });

  it("Add redirect puts Preserve path|Enabled beside the submit button", () => {
    const source = readSfc("src/pages/DomainsPage.vue");
    expect(source).toContain("redirect-form__bottom");
    const bottom = source.indexOf("redirect-form__bottom");
    for (const label of ["Redirect preserve path", "Redirect enabled now", "Add redirect"]) {
      expect(source.indexOf(label, bottom)).toBeGreaterThan(bottom);
    }
  });

  it("Notifications modal pairs Name|Kind and Resource scope|Enabled", () => {
    const source = readSfc("src/pages/NotificationsPage.vue");
    const order = ["Channel name", "Channel kind", "Webhook URL", "Resource scope", "Channel enabled"];
    let cursor = -1;
    for (const label of order) {
      const next = source.indexOf(label, cursor + 1);
      expect(next, `expected ${label} after position ${cursor}`).toBeGreaterThan(cursor);
      cursor = next;
    }
    expect(source).toContain("form-row");
  });

  it("RegisterPage pairs Password|Confirm password", () => {
    const source = readSfc("src/pages/RegisterPage.vue");
    expect(source).toContain("form-row");
    expect(source.indexOf("register-password")).toBeLessThan(
      source.indexOf("register-confirm-password"),
    );
  });
});
