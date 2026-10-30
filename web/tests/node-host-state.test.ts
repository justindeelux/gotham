import { defineComponent, h, nextTick, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/servers/api/host", () => ({
  getNodeHost: vi.fn(),
}));

import { getNodeHost } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { useNodeHost } from "@/features/servers/composables/useNodeHost";

const mockGet = vi.mocked(getNodeHost);

function stateFor(hostname: string): NodeHostState {
  return {
    capability: "ready",
    repair: false,
    network: {
      dns_servers: ["1.1.1.1"],
      ipv4: { mode: "dhcp", address: "", gateway: "" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    },
    system: { hostname, ntp_enabled: true, ntp_servers: [], timezone: "UTC" },
    pending: null,
  };
}

function mountHost(serverId: ReturnType<typeof ref<string>>) {
  let host: ReturnType<typeof useNodeHost> | null = null;
  const wrapper = mount(
    defineComponent({
      setup() {
        host = useNodeHost(() => serverId.value);
        return () => h("div");
      },
    }),
  );
  return { wrapper, host: host as unknown as ReturnType<typeof useNodeHost> };
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("useNodeHost", () => {
  it("loads each node's own host state and follows a server switch", async () => {
    mockGet.mockImplementation(async (id) =>
      stateFor(id === "srv-a" ? "node-a" : "node-b"),
    );
    const serverId = ref("srv-a");
    const { wrapper, host } = mountHost(serverId);

    await host.load();
    expect(mockGet).toHaveBeenCalledWith("srv-a");
    expect(host.state.value?.system.hostname).toBe("node-a");

    serverId.value = "srv-b";
    await nextTick();
    await flushPromises();
    expect(mockGet).toHaveBeenLastCalledWith("srv-b");
    expect(host.state.value?.system.hostname).toBe("node-b");
    wrapper.unmount();
  });

  it("surfaces a forbidden node without an error banner", async () => {
    mockGet.mockRejectedValue({ status: 403, message: "forbidden" });
    const { wrapper, host } = mountHost(ref("srv-a"));
    await host.load();
    expect(host.forbidden.value).toBe(true);
    expect(host.loadError.value).toBe("");
    wrapper.unmount();
  });

  it("keeps a failing read visible with the API message", async () => {
    mockGet.mockRejectedValue({ status: 502, message: "the host helper failed" });
    const { wrapper, host } = mountHost(ref("srv-a"));
    await host.load();
    expect(host.forbidden.value).toBe(false);
    expect(host.loadError.value).toBe("the host helper failed");
    wrapper.unmount();
  });
});
