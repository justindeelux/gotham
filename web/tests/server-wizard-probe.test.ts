// Probe-failure verbatim regression (JUS-46 review HIGH): a plain string
// outcome.message from the server probe renders verbatim instead of
// collapsing to the generic unexpected summary. Only thrown errors go
// through the localized failureText.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

import { i18n, setLocale } from "@/shared/i18n";
import { useAddServerWizard } from "@/features/servers/composables/useAddServerWizard";
import { useServersStore } from "@/features/servers/stores/servers";
import type { AddServerWizardContext } from "@/features/servers/composables/useAddServerWizard";

beforeEach(() => {
  setActivePinia(createPinia());
  setLocale("en", null);
  vi.restoreAllMocks();
});

function harness() {
  let wizard: AddServerWizardContext | null = null;
  const Child = defineComponent({
    setup() {
      wizard = useAddServerWizard(() => undefined);
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({
      render: () => h(NMessageProvider, null, { default: () => h(Child) }),
    }),
    { global: { plugins: [i18n] } },
  );
  return { wrapper, wizard: wizard as unknown as AddServerWizardContext };
}

describe("wizard probe failure renders server text verbatim", () => {
  it("keeps outcome.message instead of the generic summary", async () => {
    const { wrapper, wizard } = harness();
    const store = useServersStore();
    vi.spyOn(store, "addServer").mockResolvedValue({ id: "srv-1" } as never);
    vi.spyOn(store, "validate").mockResolvedValue({
      ok: false,
      message: "docker not installed",
      checks: [],
    } as never);
    wizard.form.keyMode = "existing";
    wizard.form.keyId = "key-1";
    await wizard.handleCreate();
    await nextTick();
    await wizard.handleValidate();
    await nextTick();
    expect(wizard.validateMessage.value).toBe("docker not installed");
    setLocale("vi", null);
    await nextTick();
    expect(wizard.validateMessage.value).toBe("docker not installed");
    wrapper.unmount();
  });
});
