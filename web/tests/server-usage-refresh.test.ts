import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";
import type { Ref } from "vue";

import { useServerUsageRefresh } from "../src/features/servers/composables/useServerUsageRefresh";
import { useServersStore } from "../src/features/servers/stores/servers";

function mountRefresh(activeTab: Ref<string>) {
  const component = defineComponent({
    setup() {
      useServerUsageRefresh(activeTab);
      return () => h("div");
    },
  });
  return mount(component);
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.restoreAllMocks();
});

describe("useServerUsageRefresh", () => {
  it("starts the shared poll when the overview mounts", () => {
    const store = useServersStore();
    const poll = vi.spyOn(store, "pollServers").mockImplementation(() => {});
    const stop = vi.spyOn(store, "stopPolling").mockImplementation(() => {});

    const wrapper = mountRefresh(ref("overview"));

    expect(poll).toHaveBeenCalledTimes(1);
    expect(stop).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("does not poll while another tab is open", () => {
    const store = useServersStore();
    const poll = vi.spyOn(store, "pollServers").mockImplementation(() => {});

    const wrapper = mountRefresh(ref("metrics"));

    expect(poll).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("stops when switching away and restarts when returning", async () => {
    const store = useServersStore();
    const poll = vi.spyOn(store, "pollServers").mockImplementation(() => {});
    const stop = vi.spyOn(store, "stopPolling").mockImplementation(() => {});
    const activeTab = ref("overview");

    const wrapper = mountRefresh(activeTab);
    activeTab.value = "metrics";
    await nextTick();
    expect(stop).toHaveBeenCalledTimes(1);

    activeTab.value = "overview";
    await nextTick();
    expect(poll).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("releases its subscription when the page unmounts", () => {
    const store = useServersStore();
    vi.spyOn(store, "pollServers").mockImplementation(() => {});
    const stop = vi.spyOn(store, "stopPolling").mockImplementation(() => {});

    const wrapper = mountRefresh(ref("overview"));
    wrapper.unmount();

    expect(stop).toHaveBeenCalledTimes(1);
  });
});
