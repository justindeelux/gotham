import { mount } from "@vue/test-utils";
import { defineComponent, h, ref } from "vue";
import { beforeEach, describe, expect, it } from "vitest";

import type { EnvVar } from "@/features/applications/api/applications";
import ApplicationEnvTab from "@/features/applications/components/ApplicationEnvTab.vue";
import type { InheritedVariable } from "@/features/projects/schemas/variables";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const inherited: InheritedVariable[] = [
  { key: "LOG_LEVEL", value: "debug", secret: false, origin: "project" },
  { key: "API_TOKEN", value: "", secret: true, origin: "environment" },
];

function mountTab(
  draft: EnvVar[] = [
    { key: "NODE_ENV", value: "production" },
    { key: "TOKEN", value: "secret:db-url" },
  ],
) {
  const draftRef = ref<EnvVar[]>(draft);
  const host = defineComponent({
    setup: () => () =>
      h(ApplicationEnvTab, {
        envDraft: draftRef.value,
        envLoading: false,
        envError: null,
        saveDisabled: false,
        saving: false,
        inherited,
        inheritedLoading: false,
        "onUpdate:draft": (value: EnvVar[]) => {
          draftRef.value = value;
        },
        onSave: () => undefined,
        onRetry: () => undefined,
      }),
  });
  return mount(host, { global: { plugins: [i18n] } });
}

function valueInputs(wrapper: ReturnType<typeof mountTab>) {
  return wrapper.findAll('input[aria-label="Variable value"]');
}

function inputTypes(wrapper: ReturnType<typeof mountTab>): (string | undefined)[] {
  return valueInputs(wrapper).map((input) => input.attributes("type"));
}

function toggle(wrapper: ReturnType<typeof mountTab>, label: string) {
  return wrapper.get(`button[aria-label="${label}"]`);
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

describe("environment value masking", () => {
  it("starts hidden: every value input is masked, keys and sealed tags stay", () => {
    const wrapper = mountTab();
    expect(inputTypes(wrapper)).toEqual(["password", "password"]);
    expect(wrapper.findAll('input[aria-label="Variable name"]').map((input) => input.element.value))
      .toEqual(["NODE_ENV", "TOKEN"]);
    expect(wrapper.find(".env-editor__secret").text()).toContain("sealed");
    expect(wrapper.text()).not.toContain("debug");
    wrapper.unmount();
  });

  it("reveals and hides all rows with one click each way", async () => {
    const wrapper = mountTab();
    await toggle(wrapper, "Show values").trigger("click");
    expect(inputTypes(wrapper)).toEqual(["text", "text"]);
    expect(wrapper.text()).toContain("debug");

    await toggle(wrapper, "Hide values").trigger("click");
    expect(inputTypes(wrapper)).toEqual(["password", "password"]);
    expect(wrapper.text()).not.toContain("debug");
    wrapper.unmount();
  });

  it("masks rows added while hidden and reveals them with the same toggle", async () => {
    const wrapper = mountTab([]);
    await toggle(wrapper, "Show values").trigger("click");
    const addButton = wrapper.findAll("button").find((button) => button.text() === "Add variable");
    await addButton!.trigger("click");
    await toggle(wrapper, "Hide values").trigger("click");
    expect(inputTypes(wrapper)).toEqual(["password"]);
    await toggle(wrapper, "Show values").trigger("click");
    expect(inputTypes(wrapper)).toEqual(["text"]);
    wrapper.unmount();
  });

  it("keeps the real value in the masked input, never a placeholder", async () => {
    const wrapper = mountTab();
    expect(valueInputs(wrapper)[0].element.value).toBe("production");
    expect(valueInputs(wrapper)[1].element.value).toBe("secret:db-url");
    await toggle(wrapper, "Show values").trigger("click");
    expect(valueInputs(wrapper)[0].element.value).toBe("production");
    wrapper.unmount();
  });

  it("shows the Vietnamese toggle labels", async () => {
    syncComposerLocale("vi");
    const wrapper = mountTab();
    await toggle(wrapper, "Hiện giá trị").trigger("click");
    expect(toggle(wrapper, "Ẩn giá trị").exists()).toBe(true);
    wrapper.unmount();
  });
});
