/* global Element:readonly, HTMLButtonElement:readonly */

import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";
import type { Ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import type { Router } from "vue-router";

import { useServersStore } from "@/features/servers/stores/servers";
import { useTemplateWizard } from "@/features/templates/composables/useTemplateWizard";
import { useTemplatesStore } from "@/features/templates/stores/templates";
import WizardConfigureStep from "@/features/templates/components/WizardConfigureStep.vue";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

type Wizard = ReturnType<typeof useTemplateWizard>;

const showRef: Ref<boolean> = ref(false);
const slugRef: Ref<string> = ref("firefly");

const detail = {
  slug: "firefly",
  name: "Firefly III",
  icon: "firefly",
  description: "Personal finance manager.",
  fields: [
    { key: "app_key", label: "Application key", type: "secret", required: false, generate: true },
    { key: "db_password", label: "Database password", type: "secret", required: true, generate: true },
    { key: "domain", label: "Domain", type: "text", required: true },
  ],
};

let wizard: Wizard | null = null;
let wrapper: ReturnType<typeof mount> | null = null;
let router: Router;

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "home", component: { template: "<div />" } },
      {
        path: "/services/:id",
        name: "service-detail",
        component: { template: "<div />" },
      },
    ],
  });
}

const Harness = defineComponent({
  setup() {
    const state = useTemplateWizard(showRef, slugRef);
    wizard = state;
    return () =>
      h("div", [
        h(WizardConfigureStep, {
          slug: "firefly",
          fields: state.fields.value,
          values: state.values.value,
          errors: state.formErrors.value,
          invalidAttempt: state.invalidAttempt.value,
          "onUpdate:values": (next) => (state.values.value = next),
        }),
        h(
          "button",
          { class: "next", type: "button", onClick: () => state.next() },
          "Next",
        ),
      ]);
  },
});

beforeEach(() => {
  setActivePinia(createPinia());
  resetLocaleState();
  registerDiscoveredCatalogs();
  syncComposerLocale("en");
  showRef.value = false;
  slugRef.value = "firefly";
  router = makeRouter();
  wizard = null;
  wrapper?.unmount();
  wrapper = null;
  vi.restoreAllMocks();
});

async function openWizard(): Promise<void> {
  const templates = useTemplatesStore();
  vi.spyOn(templates, "fetchDetail").mockResolvedValue(
    globalThis.structuredClone(detail) as never,
  );
  const servers = useServersStore();
  vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);
  wrapper = mount(
    {
      render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
    },
    { attachTo: globalThis.document.body, global: { plugins: [router] } },
  );
  showRef.value = true;
  await flushPromises();
}

function feedbackOf(key: string): string {
  const feedback = globalThis.document.querySelector(
    `.field-${key} .n-form-item-feedback`,
  );
  return feedback?.textContent ?? "";
}

async function clickNext(): Promise<void> {
  const button = globalThis.document.querySelector<HTMLButtonElement>("button.next");
  button?.click();
  await flushPromises();
  await nextTick();
}

describe("template wizard Next feedback", () => {
  it("shows the inline error, focuses the first invalid field and stays on step 1", async () => {
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    await openWizard();

    await clickNext();

    expect(wizard?.step.value).toBe(1);
    expect(feedbackOf("domain")).toBe("This field is required.");
    const domain = globalThis.document.querySelector(".field-domain input");
    expect(globalThis.document.activeElement).toBe(domain);
    expect(scrollIntoView).toHaveBeenCalled();
  });

  it("does not block on an empty generated field", async () => {
    await openWizard();

    await clickNext();

    expect(feedbackOf("db_password")).toBe(
      "Leave empty to generate it automatically.",
    );
    expect(
      globalThis.document.querySelector(".field-db_password .n-form-item-blank--error"),
    ).toBeNull();
    expect(feedbackOf("domain")).toBe("This field is required.");
  });

  it("clears the error once the field is filled and then advances", async () => {
    await openWizard();
    await clickNext();
    expect(feedbackOf("domain")).toBe("This field is required.");

    const input = wrapper?.find(".field-domain input");
    await input?.setValue("money.example.com");
    await flushPromises();

    expect(feedbackOf("domain")).toBe("");
    expect(wizard?.formErrors.value).toEqual({});

    await clickNext();
    expect(wizard?.step.value).toBe(2);
  });

  it("shows the Vietnamese copy without a raw key", async () => {
    await openWizard();
    syncComposerLocale("vi");

    await clickNext();

    expect(feedbackOf("domain")).toBe("Trường này là bắt buộc.");
  });
});
