import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { IncidentPage } from "@/features/incidents/api/incidents";

const api = vi.hoisted(() => ({
  listIncidents: vi.fn(),
}));

vi.mock("@/features/incidents/api/incidents", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/incidents/api/incidents")>();
  return { ...actual, listIncidents: api.listIncidents };
});

import NeedsAttentionSection from "@/features/incidents/components/NeedsAttentionSection.vue";
import { useIncidentsStore } from "@/features/incidents/stores/incidents";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

function emptyPage(): IncidentPage {
  return { incidents: [], next_cursor: "", active_count: 0 };
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (_value: T) => void;
} {
  let resolve: (_value: T) => void = () => undefined;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  api.listIncidents.mockReset();
});

describe("needs attention compact state", () => {
  it("keeps the collapsed line while a refresh is in flight", async () => {
    const store = useIncidentsStore();
    api.listIncidents.mockResolvedValue(emptyPage());
    const wrapper = mount(NeedsAttentionSection, {
      global: { plugins: [i18n] },
    });
    await flushPromises();
    expect(store.state).toBe("ready");
    expect(wrapper.find(".n-card").exists()).toBe(false);
    expect(wrapper.text()).toContain("Nothing needs attention");

    const live = deferred<IncidentPage>();
    api.listIncidents.mockReturnValueOnce(live.promise);
    const refresh = store.refresh();
    await flushPromises();
    expect(store.loading).toBe(false);
    expect(wrapper.find(".n-card").exists()).toBe(false);

    store.loading = true;
    await wrapper.vm.$nextTick();
    expect(wrapper.find(".n-card").exists()).toBe(false);
    expect(wrapper.text()).toContain("Nothing needs attention");

    live.resolve(emptyPage());
    await refresh;
    await flushPromises();
    expect(wrapper.find(".n-card").exists()).toBe(false);
    wrapper.unmount();
  });
});
