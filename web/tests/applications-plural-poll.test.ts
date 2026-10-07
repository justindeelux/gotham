// F7 + poll cadence: library pluralization renders 1/many in a real step,
// and a language switch fires zero API calls without touching poll cadence.
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, reactive } from "vue";

import { useApplicationsStore } from "@/features/applications/stores/applications";
import {
  createWizardKey,
} from "@/features/applications/composables/useCreateAppWizard";
import WizardEnvStep from "@/features/applications/components/WizardEnvStep.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/features/applications/api/applications")>();
  let calls = 0;
  const state = {
    calls: () => calls,
    deployments: [
      {
        id: "deploy-1",
        state: "building",
      },
    ],
  };
  (globalThis as Record<string, unknown>).__pollProbe = state;
  return {
    ...original,
    describeApplicationError: (error: unknown) => String((error as Error)?.message ?? error),
    listDeployments: async () => {
      calls += 1;
      return [...state.deployments];
    },
  };
});

function pollCalls(): number {
  return (globalThis as Record<string, { calls: () => number }>).__pollProbe.calls();
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("wizard dropped rows render library plurals", () => {
  it("shows 1/many forms in both locales from a real step", async () => {
    const form = reactive({
      env: [{ key: "", value: "dropped" }],
      storage: [],
    });
    const state = {
      form,
      envKeyWarnings: computed(() => false),
      droppedEnvRows: computed(() =>
        form.env.filter((row) => row.key.trim() === "" && row.value.trim() !== "").length,
      ),
    };
    const wrapper = mount(WizardEnvStep, {
      global: {
        plugins: [i18n],
        provide: { [createWizardKey as symbol]: state },
      },
    });
    expect(wrapper.text()).toContain("1 variable row without a name will be ignored on create.");
    form.env.push({ key: "", value: "also dropped" });
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("2 variable rows without a name will be ignored on create.");
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("2 biến không tên sẽ bị bỏ qua khi tạo.");
    wrapper.unmount();
  });
});

describe("locale switch fires no API calls and keeps poll cadence", () => {
  it("adds zero calls on switch and keeps the active cadence", async () => {
    setActivePinia(createPinia());
    const store = useApplicationsStore();
    await store.fetchDeployments("app-1");
    expect(pollCalls()).toBe(1);
    await vi.advanceTimersByTimeAsync(9_000);
    const before = pollCalls();
    expect(before).toBeGreaterThan(1);
    setLocale("vi", null);
    await Promise.resolve();
    expect(pollCalls()).toBe(before);
    await vi.advanceTimersByTimeAsync(9_000);
    expect(pollCalls()).toBeGreaterThan(before);
    store.stopAllPolling();
  });
});
