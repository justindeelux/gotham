import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getInstanceInfo } = vi.hoisted(() => ({ getInstanceInfo: vi.fn() }));

vi.mock("@/features/version", () => ({
  formatVersionTag: () => null,
  getInstanceInfo: () => getInstanceInfo(),
}));

import AppTopbar from "@/app/layouts/AppTopbar.vue";
import { provideMobileNav } from "@/app/layouts/useMobileNav";
import {
  i18n,
  registerDiscoveredCatalogs,
  syncComposerLocale,
} from "@/shared/i18n";

function mountTopbar(): ReturnType<typeof mount> {
  setActivePinia(createPinia());
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/dashboard", name: "dashboard", component: { template: "<div />" } }],
  });
  const host = defineComponent({
    setup() {
      provideMobileNav();
      return () => h(AppTopbar);
    },
  });
  return mount(host, { global: { plugins: [router, i18n] } });
}

beforeEach(() => {
  if (!globalThis.window.matchMedia) {
    globalThis.window.matchMedia = (() => ({
      matches: false,
      media: "",
      addEventListener: () => {},
      removeEventListener: () => {},
    })) as never;
  }
  registerDiscoveredCatalogs();
  syncComposerLocale("en");
  getInstanceInfo.mockReset();
});

describe("topbar control-plane chip", () => {
  it("shows the port reported by the control plane", async () => {
    getInstanceInfo.mockResolvedValue({ version: "1.2.3", port: 8000 });
    const wrapper = mountTopbar();
    await flushPromises();
    expect(wrapper.find(".channel-chip").text()).toBe("CP :8000");
  });

  it("follows a custom server port", async () => {
    getInstanceInfo.mockResolvedValue({ version: "1.2.3", port: 9100 });
    const wrapper = mountTopbar();
    await flushPromises();
    expect(wrapper.find(".channel-chip").text()).toBe("CP :9100");
  });

  it("hides the chip when the port is unknown", async () => {
    getInstanceInfo.mockResolvedValue({ version: "1.2.3" });
    const wrapper = mountTopbar();
    await flushPromises();
    expect(wrapper.find(".channel-chip").exists()).toBe(false);
  });
});
