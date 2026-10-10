// Router list UI tests (JUS-90): the routers tab renders the generated
// routers from the API, filters them through the search box, and reports an
// unreadable node instead of inventing rows.
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
import domainsVi from "@/features/domains/locales/vi";
import RoutersPanel from "@/features/domains/components/RoutersPanel.vue";
import { useProxyStore } from "@/features/domains/stores/proxy";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { domains: domainsEn });
  i18n.global.mergeLocaleMessage("vi", { domains: domainsVi });
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
      middlewares: ["gotham-https-redirect"],
      tls_resolver: "letsencrypt",
      kind: "application",
      owner_id: "app-1",
      owner_name: "shop",
      server_id: "node-1",
      server_name: "prod-01",
    },
    {
      host: "old.example.com",
      rule: "Host(`old.example.com`)",
      service: "gotham-redirect-noop",
      target: "https://app.example.com",
      entrypoints: ["web"],
      middlewares: ["gotham-redirect-1"],
      kind: "redirect",
      owner_id: "rule-1",
      owner_name: "shop",
      server_id: "node-1",
      server_name: "prod-01",
    },
  ],
  nodes: [{ server_id: "node-1", server_name: "prod-01", sync_status: "synced" }],
};

function mountPanel() {
  const wrapper = mount(defineComponent({ render: () => h(RoutersPanel) }), {
    global: { plugins: [i18n] },
  });
  return wrapper;
}

describe("routers panel", () => {
  it("renders the generated routers with source, node and TLS", async () => {
    vi.mocked(listRouters).mockResolvedValue(envelope as never);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();
    const html = wrapper.html();
    expect(html).toContain("app.example.com");
    expect(html).toContain("old.example.com");
    expect(html).toContain("letsencrypt");
    expect(html).toContain("prod-01");
    expect(html).toContain("web/websecure");
    wrapper.unmount();
  });

  it("filters rows through the search box", async () => {
    vi.mocked(listRouters).mockResolvedValue(envelope as never);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();
    const search = wrapper.find("input");
    await search.setValue("old.example");
    await nextTick();
    const html = wrapper.html();
    expect(html).toContain("old.example.com");
    expect(wrapper.findAll("tbody tr")).toHaveLength(1);
    wrapper.unmount();
  });

  it("reports a failed read instead of inventing rows", async () => {
    vi.mocked(listRouters).mockRejectedValue(
      Object.assign(new Error("node down"), { status: 502 }),
    );
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();
    const store = useProxyStore();
    expect(store.routersError).not.toBeNull();
    expect(wrapper.html()).not.toContain("app.example.com");
    wrapper.unmount();
  });

  it("marks the unreachable node while keeping the healthy rows", async () => {
    vi.mocked(listRouters).mockResolvedValue({
      routers: [envelope.routers[0]],
      nodes: [
        { server_id: "node-1", server_name: "prod-01", sync_status: "synced" },
        { server_id: "node-2", server_name: "edge-01", sync_status: "unknown", error: "agent down" },
      ],
    } as never);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();
    const html = wrapper.html();
    expect(html).toContain("app.example.com");
    expect(html).toContain("edge-01");
    expect(html).toContain("agent down");
    wrapper.unmount();
  });
});
