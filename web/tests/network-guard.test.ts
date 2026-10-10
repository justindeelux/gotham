// NetworkForm static-host guard (JUS-100): the form loads the host's real
// static configuration, a DNS-only edit saves without touching the interface,
// and a static-to-DHCP switch needs an explicit confirmation.
import { NMessageProvider } from "naive-ui";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/instance-settings/api/instance", () => ({
  saveNetwork: vi.fn(),
}));

import { saveNetwork } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import NetworkForm from "@/features/instance-settings/components/NetworkForm.vue";
import {
  isRiskyInterfaceChange,
  sameInterface,
} from "@/features/instance-settings/schemas/instance";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockSave = vi.mocked(saveNetwork);

function staticState(): InstanceState {
  return {
    network: {
      dns_servers: ["1.1.1.1"],
      ipv4: { mode: "static", address: "103.176.22.225/24", gateway: "103.176.22.1" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    },
    capabilities: { network: true, system: true },
    pending: null,
  } as unknown as InstanceState;
}

function shell(state: InstanceState) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(NetworkForm as never, { state }),
      });
    },
  });
}

async function mountForm(state: InstanceState) {
  const wrapper = mount(shell(state), {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
  await flushPromises();
  await nextTick();
  return wrapper;
}

function dnsInputs(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('input[placeholder="1.1.1.1"], input[placeholder="1.0.0.1"], input[placeholder="9.9.9.9"]');
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
});

describe("NetworkForm static-host guard", () => {
  it("shows the host's real static address instead of DHCP defaults", async () => {
    const wrapper = await mountForm(staticState());
    const address = wrapper.find('input[placeholder="192.168.1.10/24"]');
    expect(address.exists()).toBe(true);
    expect((address.element as { value: string }).value).toBe("103.176.22.225/24");
    wrapper.unmount();
  });

  it("saves a DNS-only edit without the confirmation flag", async () => {
    const stored = staticState();
    mockSave.mockImplementation(async (input) => ({ ...stored, network: { ...stored.network, ...input } }) as InstanceState);
    const wrapper = await mountForm(stored);
    const inputs = dnsInputs(wrapper);
    await inputs[0].setValue("9.9.9.9");
    expect(wrapper.text()).toContain("left untouched");
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();
    await nextTick();
    expect(mockSave).toHaveBeenCalledTimes(1);
    const sent = mockSave.mock.calls[0][0];
    expect(sent.dns_servers).toEqual(["9.9.9.9"]);
    expect(sent.ipv4).toEqual({ mode: "static", address: "103.176.22.225/24", gateway: "103.176.22.1" });
    expect(sent.confirm_interface_change).toBe(false);
    wrapper.unmount();
  });

  it("blocks static-to-DHCP until the confirmation box is ticked", async () => {
    const stored = staticState();
    mockSave.mockImplementation(async (input) => ({ ...stored, network: { ...stored.network, ...input } }) as InstanceState);
    const wrapper = await mountForm(stored);
    const radios = wrapper.findAll(".n-radio-button");
    await radios[0].trigger("click");
    await nextTick();
    expect(wrapper.text()).toContain("may become unreachable");
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();
    await nextTick();
    expect(mockSave).not.toHaveBeenCalled();
    await wrapper.find(".n-checkbox").trigger("click");
    await nextTick();
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();
    await nextTick();
    expect(mockSave).toHaveBeenCalledTimes(1);
    const sent = mockSave.mock.calls[0][0];
    expect(sent.ipv4.mode).toBe("dhcp");
    expect(sent.confirm_interface_change).toBe(true);
    wrapper.unmount();
  });
});

describe("interface comparison helpers", () => {
  const live = staticState().network;
  it("ignores DNS but catches interface changes", () => {
    expect(sameInterface(live, { ...live, dns_servers: ["9.9.9.9"] })).toBe(true);
    expect(sameInterface(live, { ...live, ipv4: { ...live.ipv4, gateway: "103.176.22.2" } })).toBe(false);
  });

  it("flags only disturbances of a live static family", () => {
    expect(isRiskyInterfaceChange(live, { ...live, dns_servers: ["9.9.9.9"] })).toBe(false);
    expect(isRiskyInterfaceChange(live, { ...live, ipv4: { mode: "dhcp", address: "", gateway: "" } })).toBe(true);
    const moved = { ...live, ipv4: { ...live.ipv4, address: "103.176.22.99/24" } };
    expect(isRiskyInterfaceChange(live, moved)).toBe(true);
    const dhcp = {
      dns_servers: ["1.1.1.1"],
      ipv4: { mode: "dhcp", address: "", gateway: "" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    };
    expect(isRiskyInterfaceChange(dhcp, { ...dhcp, dns_servers: ["9.9.9.9"] })).toBe(false);
  });
});
