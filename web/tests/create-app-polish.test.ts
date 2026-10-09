// JUS-75 create-application modal polish: the step counter matches the
// stepper length for every source type, the stale helper sentence is gone,
// and notices render as small inline alerts under their field.
import { mount } from "@vue/test-utils";
import { NAlert, NMessageProvider } from "naive-ui";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { defineComponent, h, ref } from "vue";

import BuildArgsEditor from "@/features/applications/components/BuildArgsEditor.vue";
import WizardSourceStep from "@/features/applications/components/WizardSourceStep.vue";
import {
  provideCreateWizard,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import en from "@/features/applications/locales/en";
import vi from "@/features/applications/locales/vi";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/** readSfc returns the raw source of one single-file component. */
function readSfc(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), "utf8");
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

/** mountSourceStep renders the Source step for one source type. */
function mountSourceStep(sourceType: string) {
  setActivePinia(createPinia());
  let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
  const Harness = defineComponent({
    setup() {
      wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
      provideCreateWizard(wiz);
      wiz.form.sourceType = sourceType as never;
      return () => h(WizardSourceStep);
    },
  });
  const wrapper = mount(
    { render: () => h(NMessageProvider, null, { default: () => h(Harness) }) },
    { global: { plugins: [i18n], stubs: { RouterLink: true } } },
  );
  return { w: wiz!, wrapper };
}

/** mountWizard exposes the raw wizard state without rendering a step. */
function mountWizard() {
  setActivePinia(createPinia());
  let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
  const Harness = defineComponent({
    setup() {
      wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
      return () => h("div");
    },
  });
  const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
  return { w: wiz!, wrapper };
}

describe("JUS-75 step counter matches the stepper", () => {
  const skipped = ["dockerfile", "image"];
  const full = ["git_public", "git_private", "github_app", "gitlab_app", "compose"];

  it.each([...skipped, ...full])(
    "derives the rail and the counter from the same step list for %s",
    (sourceType) => {
      const { w, wrapper } = mountWizard();
      w.form.sourceType = sourceType as never;
      const total = skipped.includes(sourceType) ? 4 : 5;
      expect(w.stepTotal.value).toBe(total);
      expect(w.visibleStepNames.value).toHaveLength(total);
      expect(w.visibleStepNames.value[0]).toBe(w.stepNames.value[0]);
      // Walk every visited step: positions run 1..total with no gap.
      const seen: number[] = [w.stepPosition.value];
      while (w.visibleStepIndex.value < total - 1) {
        w.nextStep();
        seen.push(w.stepPosition.value);
      }
      expect(seen).toEqual(Array.from({ length: total }, (_, index) => index + 1));
      expect(w.visibleStepIndex.value).toBe(total - 1);
      wrapper.unmount();
    },
  );

  it("renders the rail from the visible steps, keeping the scroll contract", () => {
    const source = readSfc("src/features/applications/components/CreateAppWizard.vue");
    expect(source).toContain("wizard.visibleStepNames.value");
    expect(source).toContain("wizard.visibleStepIndex.value");
    expect(source).not.toMatch(/v-for="\(label, index\) in wizard\.stepNames\.value"/);
    expect(source).toContain("wizard-modal");
    expect(source).toMatch(/\.wizard-body\s*\{[^}]*overflow-y:\s*auto/);
    expect(source).toMatch(/\.wizard-foot\s*\{[^}]*flex:\s*0\s*0\s*auto/);
  });
});

describe("JUS-75 stale helper copy is gone", () => {
  it("drops the repeated sentence and keeps a short helper in both locales", () => {
    expect(JSON.stringify(en)).not.toContain("deploy today");
    expect(JSON.stringify(vi)).not.toContain("triển khai được ngay");
    for (const catalog of [en, vi]) {
      expect(catalog.wizard.sourceTypeHint).not.toContain("today");
      expect(catalog.wizard.sourceTypeHint.length).toBeLessThan(80);
    }
  });

  it("shortens the intro to one line in both locales", () => {
    for (const catalog of [en, vi]) {
      expect(catalog.wizard.intro).not.toContain("\n");
      expect(catalog.wizard.intro.length).toBeLessThan(80);
    }
  });

  it("shortens the registry placeholders so they no longer truncate", () => {
    for (const catalog of [en, vi]) {
      expect(catalog.wizard.registryUsernamePlaceholder.length).toBeLessThanOrEqual(12);
      expect(catalog.wizard.registryPasswordPlaceholder.length).toBeLessThanOrEqual(12);
    }
  });
});

describe("JUS-75 notices are small inline alerts under their field", () => {
  it("renders the latest-tag notice iconless and inline under the image field", () => {
    const { wrapper } = mountSourceStep("image");
    const alerts = wrapper.findAllComponents(NAlert).filter((alert) => alert.props("type") === "warning");
    expect(alerts).toHaveLength(1);
    expect(alerts[0].props("showIcon")).toBe(false);
    expect(wrapper.find(".n-alert.field-alert").exists()).toBe(true);
    expect(alerts[0].text()).toContain("latest");
    // Order: the image input renders before the notice.
    const html = wrapper.html();
    expect(html.indexOf("registry.example.com/team/app:1.2")).toBeLessThan(html.indexOf("latest"));
    wrapper.unmount();
  });

  it("renders the compose secrets notice iconless and inline under the document", () => {
    const { wrapper } = mountSourceStep("compose");
    const alerts = wrapper.findAllComponents(NAlert).filter((alert) => alert.props("type") === "warning");
    expect(alerts).toHaveLength(1);
    expect(alerts[0].props("showIcon")).toBe(false);
    expect(wrapper.find(".n-alert.field-alert").exists()).toBe(true);
    const html = wrapper.html();
    expect(html.indexOf("services:")).toBeLessThan(html.indexOf(alerts[0].text().slice(0, 24)));
    wrapper.unmount();
  });

  it("renders the build-args warning iconless and below the rows", () => {
    const editor = mount(BuildArgsEditor, {
      props: { modelValue: [{ key: "APP_ENV", value: "production" }] },
      global: { plugins: [i18n] },
    });
    const alerts = editor.findAllComponents(NAlert);
    expect(alerts).toHaveLength(1);
    expect(alerts[0].props("type")).toBe("warning");
    expect(alerts[0].props("showIcon")).toBe(false);
    expect(editor.find(".n-alert.arg-alert").exists()).toBe(true);
    const html = editor.html();
    expect(html.indexOf("arg-row")).toBeLessThan(html.indexOf("n-alert"));
    // Existing aria labels used by e2e stay untouched.
    expect(editor.find("input[aria-label='KEY']").exists()).toBe(true);
    expect(editor.find("input[aria-label='value']").exists()).toBe(true);
    editor.unmount();
  });

  it("keeps every notice inside its field in the template source", () => {
    const source = readSfc("src/features/applications/components/WizardSourceStep.vue");
    const imageBlock = source.slice(
      source.indexOf("wizard.imageRef"),
      source.indexOf("wizard.registryUsername"),
    );
    expect(imageBlock).toContain("imageLatestWarn");
    expect(imageBlock).toMatch(/class="field-alert"[^>]*type="warning"[^>]*:show-icon="false"/);
    const composeBlock = source.slice(
      source.indexOf("wizard.composeContent"),
      source.indexOf("wizard.composeServicePlaceholder"),
    );
    expect(composeBlock).toContain("composeSecretsHint");
    expect(composeBlock).toMatch(/class="field-alert"[^>]*type="warning"[^>]*:show-icon="false"/);
  });
});
