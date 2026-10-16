import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/domains/api/proxy", () => ({ listRouters: vi.fn() }));

import { listRouters } from "@/features/domains/api/proxy";
import ControlPlaneDomainStatus from "@/features/instance-settings/components/ControlPlaneDomainStatus.vue";
import en from "@/features/instance-settings/locales/en";
import vietnamese from "@/features/instance-settings/locales/vi";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";
import { formatDate } from "@/shared/utils/format";

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
  it.each(["issued", "pending", "failed"] as const)(
    "renders HTTPS certificate state %s and keeps DNS unreported",
    async (status) => {
      const notAfter = "2027-01-14T12:00:00Z";
      vi.mocked(listRouters).mockResolvedValue({
        ...envelope,
        routers: [{ ...envelope.routers[0], certificate: {
          status, ...(status === "issued" ? { not_after: notAfter } : {}),
        } }],
      });
      const wrapper = mountStatus();
      await settle();
      expect(wrapper.text()).toContain(en.domainStatus.certState[status]);
      expect(wrapper.text()).toContain(en.domainStatus.dnsUnknown);
      expect(wrapper.text()).toContain(en.domainStatus.synced);
      expect(wrapper.text()).not.toContain(en.domainStatus.certUnknown);
      if (status === "issued") {
        expect(wrapper.text()).toContain(`Expires ${formatDate(notAfter, "en")}`);
      } else {
        expect(wrapper.text()).not.toContain("Expires");
      }
      syncComposerLocale("vi");
      await flushPromises();
      expect(wrapper.text()).toContain(vietnamese.domainStatus.certState[status]);
      if (status === "issued") {
        expect(wrapper.text()).toContain(`Hết hạn ${formatDate(notAfter, "vi")}`);
      }
      wrapper.unmount();
    },
  );

  it("reports HTTP as not applicable even if certificate data is present", async () => {
    vi.mocked(listRouters).mockResolvedValue({
      ...envelope,
      routers: [{ ...envelope.routers[0], certificate: {
        status: "issued", not_after: "2027-01-14T12:00:00Z",
      } }],
    });
    const wrapper = mountStatus();
    await wrapper.setProps({ url: "http://panel.example.com", savedUrl: "http://panel.example.com" });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.certNotApplicable);
    expect(wrapper.text()).not.toContain("Expires");
    expect(wrapper.text()).not.toContain(en.domainStatus.certState.issued);
    wrapper.unmount();
  });

  it("uses the proposed router during a trial despite the previous saved URL", async () => {
    vi.mocked(listRouters).mockResolvedValue({
      ...envelope,
      routers: [
        { ...envelope.routers[0], host: "previous.example.com" },
        { ...envelope.routers[0], certificate: { status: "pending" } },
      ],
    });
    const wrapper = mountStatus();
    await wrapper.setProps({ savedUrl: "http://previous.example.com" });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.synced);
    expect(wrapper.text()).toContain(en.domainStatus.certState.pending);
    expect(wrapper.text()).not.toContain(en.domainStatus.unsaved);
    wrapper.unmount();
  });

  it("keeps certificate unknown when localhost is missing, even with router data", async () => {
    vi.mocked(listRouters).mockResolvedValue({
      nodes: [],
      routers: [{ ...envelope.routers[0], certificate: { status: "issued" } }],
    });
    const wrapper = mountStatus();
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.missingLocalhost);
    expect(wrapper.text()).toContain(en.domainStatus.certUnknown);
    expect(wrapper.text()).not.toContain(en.domainStatus.certState.issued);
    wrapper.unmount();
  });

  it("reads a failed certificate even when only the fallback HTTP router exists", async () => {
    vi.mocked(listRouters).mockResolvedValue({
      ...envelope,
      routers: [{ ...envelope.routers[0], entrypoints: ["web"], certificate: { status: "failed" } }],
    });
    const wrapper = mountStatus();
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.certState.failed);
    expect(wrapper.text()).toContain(en.domainStatus.pending);
    wrapper.unmount();
  });

  it("reports synced and unreported observations in both locales", async () => {
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
    [
      { ...envelope, nodes: [{ ...envelope.nodes[0], error: "offline" }] },
      "readFailed", "unknown",
    ],
    [{ ...envelope, routers: [] }, null, "pending"],
    [{ ...envelope, nodes: [{ ...envelope.nodes[0], sync_status: "pending" }] }, null, "pending"],
    [{ ...envelope, nodes: [{ ...envelope.nodes[0], sync_status: "unknown" }] }, null, "unknown"],
  ] as const)("handles router state: %j", async (result, warning, status) => {
    vi.mocked(listRouters).mockResolvedValue(result as Awaited<ReturnType<typeof listRouters>>);
    const wrapper = mountStatus();
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus[status]);
    if (warning) expect(wrapper.text()).toContain(en.domainStatus[warning]);
    wrapper.unmount();
  });

  it("rejects old responses after URL changes and refreshes after save", async () => {
    let resolveOld!: (_value: typeof envelope) => void;
    vi.mocked(listRouters).mockImplementationOnce(() => new Promise((resolve) => {
      resolveOld = resolve;
    }));
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

  it.each([
    "https://panel.example.com/",
    "https://PANEL.EXAMPLE.COM",
    "https://panel.example.com:443/",
  ])("accepts equivalent URL origins: %s", async (draft) => {
    const wrapper = mountStatus();
    await wrapper.setProps({ url: draft });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.synced);
    expect(wrapper.text()).not.toContain(en.domainStatus.unsaved);
    wrapper.unmount();
  });

  it("does not report a different port as synced, even after save", async () => {
    const wrapper = mountStatus();
    await wrapper.setProps({ url: "https://panel.example.com:8443/" });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.unsaved);
    expect(wrapper.text()).not.toContain(en.domainStatus.synced);
    await wrapper.setProps({ savedUrl: "https://panel.example.com:8443" });
    await settle();
    expect(wrapper.text()).toContain(en.domainStatus.pending);
    expect(wrapper.text()).not.toContain(en.domainStatus.unsaved);
    wrapper.unmount();
  });
});
