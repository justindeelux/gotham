// Server proxy tab tests (JUS-202): the Proxy & Traefik tab renders this
// node's generated routers from the shared proxy store instead of the stale
// Phase 6 placeholder.
import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

vi.mock("@/features/domains/api/proxy", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/domains/api/proxy")>();
  return { ...actual, listRouters: vi.fn() };
});

import { listRouters } from "@/features/domains/api/proxy";
import domainsEn from "@/features/domains/locales/en";
import serversEn from "@/features/servers/locales/en";
import ServerProxyTab from "@/features/servers/components/ServerProxyTab.vue";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { domains: domainsEn });
  i18n.global.mergeLocaleMessage("en", { servers: serversEn });
  syncComposerLocale("en");
  setActivePinia(createPinia());
  vi.mocked(listRouters).mockReset();
});

const envelope = {
  routers: [
    {
      host: "app.example.com",
      rule: "Host(`app.example.com`)",
      service: "app-1",
      target: "http://172.17.0.1:8080",
      entrypoints: ["web", "websecure"],
      tls_resolver: "letsencrypt",
      kind: "application",
      owner_id: "app-1",
      owner_name: "shop",
      server_id: "node-1",
      server_name: "prod-01",
    },
    {
      host: "other.example.com",
      rule: "Host(`other.example.com`)",
      service: "app-2",
      entrypoints: ["web"],
      kind: "application",
      owner_id: "app-2",
      owner_name: "blog",
      server_id: "node-2",
      server_name: "prod-02",
    },
  ],
  nodes: [{ server_id: "node-1", server_name: "prod-01", sync_status: "synced" }],
};

function mountTab() {
  return mount(defineComponent({ render: () => h(ServerProxyTab, { serverId: "node-1" }) }), {
    global: { plugins: [i18n] },
  });
}

describe("server proxy tab", () => {
  it("renders only this node's routers, never the Phase 6 placeholder", async () => {
    vi.mocked(listRouters).mockResolvedValue(envelope as never);
    const wrapper = mountTab();
    await flushPromises();
    await nextTick();
    const html = wrapper.html();
    expect(html).toContain("app.example.com");
    expect(html).toContain("letsencrypt");
    expect(html).not.toContain("other.example.com");
    expect(html).not.toContain("Phase 6");
  });

  it("shows the empty state when the node has no routers", async () => {
    vi.mocked(listRouters).mockResolvedValue({ routers: [], nodes: [] } as never);
    const wrapper = mountTab();
    await flushPromises();
    await nextTick();
    expect(wrapper.html()).toContain("No routers generated yet.");
  });
});
