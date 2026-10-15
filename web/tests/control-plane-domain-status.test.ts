import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/domains/api/proxy", () => ({ listRouters: vi.fn() }));

import { listRouters } from "@/features/domains/api/proxy";
import ControlPlaneDomainStatus from "@/features/instance-settings/components/ControlPlaneDomainStatus.vue";
import en from "@/features/instance-settings/locales/en";
import vietnamese from "@/features/instance-settings/locales/vi";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const url = "https://panel.example.com";
const envelope = {
  routers: [{
    host: "panel.example.com", kind: "control_plane" as const, server_id: "local",
    rule: "Host(`panel.example.com`)", service: "gotham-panel", owner_id: "",
    entrypoints: ["web", "websecure"],
  }],
  nodes: [{ server_id: "local", server_name: "localhost", sync_status: "synced" as const }],
};

function mountStatus() {
  return mount(ControlPlaneDomainStatus, {
    props: { url, savedUrl: url },
    global: { plugins: [i18n] },
  });
}

async function settle() {
  await vi.advanceTimersByTimeAsync(300);
  await flushPromises();
}

beforeEach(() => {
  vi.useFakeTimers();
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { "instance-settings": en });
  i18n.global.mergeLocaleMessage("vi", { "instance-settings": vietnamese });
  syncComposerLocale("en");
  vi.mocked(listRouters).mockReset().mockResolvedValue(envelope);
});

afterEach(() => vi.useRealTimers());

describe("control-plane domain status", () => {
  it("reports synced without inventing DNS or certificate observations, in both locales", async () => {
    expect(checkCatalogParity(en, vietnamese)).toEqual([]);
    const wrapper = mountStatus();
    expect(wrapper.text()).toContain(en.domainStatus.loading);
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.synced);
    expect(wrapper.text()).toContain(en.domainStatus.dnsUnknown);
    expect(wrapper.text()).toContain(en.domainStatus.certUnknown);
    syncComposerLocale("vi");
    await flushPromises();
    expect(wrapper.text()).toContain(vietnamese.domainStatus.synced);
    expect(listRouters).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it.each([
    [{ ...envelope, nodes: [] }, "missingLocalhost", "unknown"],
    [{ ...envelope, nodes: [{ ...envelope.nodes[0], error: "offline" }] }, "readFailed", "unknown"],
    [{ ...envelope, routers: [] }, null, "pending"],
    [{ ...envelope, nodes: [{ ...envelope.nodes[0], sync_status: "pending" }] }, null, "pending"],
    [{ ...envelope, nodes: [{ ...envelope.nodes[0], sync_status: "unknown" }] }, null, "unknown"],
  ] as const)("handles missing, unreadable, pending and unknown routers", async (result, warning, status) => {
    vi.mocked(listRouters).mockResolvedValue(result as Awaited<ReturnType<typeof listRouters>>);
    const wrapper = mountStatus();
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus[status]);
    if (warning) expect(wrapper.text()).toContain(en.domainStatus[warning]);
    wrapper.unmount();
  });

  it("rejects old responses after URL changes and refreshes after save", async () => {
    let resolveOld!: (_value: typeof envelope) => void;
    vi.mocked(listRouters).mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }));
    const wrapper = mountStatus();
    await settle();
    await wrapper.setProps({ url: "http://new.example.com" });
    resolveOld(envelope);
    await flushPromises();
    expect(wrapper.text()).not.toContain(en.domainStatus.synced);
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.unsaved);
    expect(wrapper.text()).toContain(en.domainStatus.certNotApplicable);
    vi.mocked(listRouters).mockResolvedValue({
      ...envelope, routers: [{ ...envelope.routers[0], host: "new.example.com" }],
    });
    await wrapper.setProps({ savedUrl: "http://new.example.com" });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.synced);
    wrapper.unmount();
  });

  it("shows failed reads, retries, and skips empty or invalid URLs", async () => {
    vi.mocked(listRouters).mockRejectedValueOnce(new Error("unavailable"));
    const wrapper = mountStatus();
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.readFailed);
    await wrapper.get("button").trigger("click");
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.synced);
    await wrapper.setProps({ url: "" });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.empty);
    await wrapper.setProps({ url: "bad url" });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.invalid);
    expect(listRouters).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("does not let a remote application router stand in for the panel", async () => {
    vi.mocked(listRouters).mockResolvedValue({
      ...envelope,
      routers: [{ ...envelope.routers[0], kind: "application" }],
    });
    const wrapper = mountStatus();
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.pending);
    wrapper.unmount();
  });
});
