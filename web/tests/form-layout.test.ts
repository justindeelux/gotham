
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { beforeEach, describe, expect, it } from "vitest";
import { h } from "vue";

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
import en from "../src/features/databases/locales/en";
import type { CertificateDraft } from "../src/features/domains/api/proxy";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

function mountWithI18n(component: unknown, options: Record<string, unknown>) {
  return mount(component as never, {
    ...(options as object),
    global: { plugins: [i18n] },
  } as never) as VueWrapper<never>;
}

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainCss = readFileSync(resolve(webRoot, "src/shared/styles/main.css"), "utf8");

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { domains: domainsEn });
  i18n.global.mergeLocaleMessage("vi", { domains: domainsVi });
  syncComposerLocale("en");
});

function readSfc(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), "utf8");
}

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

function expectPair(source: string, a: string, b: string, rowClass = "form-row"): void {
  const hit = rowBlocks(source, rowClass).some((block) => {
    const first = block.indexOf(a);
    return first !== -1 && block.indexOf(b, first) !== -1;
  });
  expect(hit, `expected "${a}" and "${b}" in the same .${rowClass} block`).toBe(true);
}

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

describe("JUS-88 CertificateForm single column", () => {
  const draft: CertificateDraft = {
    application_id: "",
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    enabled: true,
  };

  function mountForm() {
    return mount(CertificateForm, {
      props: { modelValue: draft, applications: [], providers: [] },
      global: { plugins: [i18n] },
    });
  }

  it("stacks every field in one column (no .form-row)", () => {
    const wrapper = mountForm();
    expect(wrapper.findAll(".form-row")).toHaveLength(0);
    expect(wrapper.findAll(".n-form-item")).toHaveLength(6);
  });

  it("keeps labels visible and the tab order top to bottom", () => {
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

  it("establishes its own container so the 560px modal never overflows", () => {
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
    const wrapper = mountWithI18n(NMessageProvider, {
      slots: {
        default: () =>
          h(DynamicForm, {
            fields: [
              { key: "a", label: "Alpha", type: "text" },
              { key: "b", label: "Beta", type: "text" },
              { key: "c", label: "Gamma", type: "text" },
            ],
            modelValue: {},
            errors: {},
          }),
      },
    });
    const labels = wrapper.findAll(".n-form-item-label").map((node) => node.text());
    expect(labels).toEqual(["Alpha (optional)", "Beta (optional)", "Gamma (optional)"]);
  });
});

describe("JUS-19 AddServerWizard rows", () => {
  const source = readSfc("src/features/servers/components/wizard/WizardConnectStep.vue");

  it("pairs Node name|SSH user in one row", () => {
    expectPair(source, "add-server-name", "add-server-ssh-user");
  });

  it("keeps Authentication and the key-mode selector on full-width rows", () => {
    const inRow = rowBlocks(source).some(
      (block) =>
        block.includes("servers.wizard.authMethod") || block.includes("servers.wizard.keyModeName"),
    );
    expect(inRow).toBe(false);
  });

  it("keeps Key name on its own row", () => {
    const inRow = rowBlocks(source).some((block) => block.includes("servers.wizard.keyName"));
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
    const order = ["servers.edit.nodeName", "servers.edit.ipLabel", "servers.edit.portLabel", "servers.edit.sshUser", "servers.edit.credentialChange"];
    let cursor = -1;
    for (const label of order) {
      const next = source.indexOf(label, cursor + 1);
      expect(next, `expected ${label} after position ${cursor}`).toBeGreaterThan(cursor);
      cursor = next;
    }
    const credentialsHeading = source.indexOf("servers.edit.credentials");
    expect(source.indexOf("servers.edit.credentialChange", credentialsHeading)).toBeGreaterThan(
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

  it("keeps the node picker and explains domain routing moved to the Domains tab", () => {
    expect(runtimeStep).toContain("applications.wizard.node");
    expect(runtimeStep).toContain("applications.wizard.routingHint");
    expect(runtimeStep).not.toContain("applications.wizard.internalPort");
    expect(runtimeStep).not.toContain("applications.wizard.hostPort");
  });
});

describe("JUS-19 CreateDatabaseWizard rows", () => {
  it("pairs Version|Node in one row", () => {
    const source = readSfc("src/features/databases/components/CreateDatabaseWizard.vue");
    expectPair(source, "databases.wizard.engineStep.version", "databases.wizard.engineStep.node");
    expect(en.wizard.engineStep.version).toBe("Version");
    expect(en.wizard.engineStep.node).toBe("Node");
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
    expect(mainCss).toMatch(/\.auth-page\s*\{[^}]*container-type:\s*inline-size/);
  });
});
