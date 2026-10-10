// NetworkForm DNS inputs (JUS-99): Primary/Alternate by default, a stored
// third server stays editable behind an "Add DNS server" link, and saving
// keeps the dns_servers array format with empties dropped.
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
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockSave = vi.mocked(saveNetwork);

function stateWith(dns: string[]): InstanceState {
  return {
    network: {
      dns_servers: dns,
      ipv4: { mode: "dhcp", address: "", gateway: "" },
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
  return wrapper.findAll('input[placeholder="1.1.1.1"], input[placeholder^="2606"], input[placeholder="9.9.9.9"]');
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
});

describe("NetworkForm DNS inputs", () => {
  it("keeps a stored third server editable and hides the add link", async () => {
    const wrapper = await mountForm(stateWith(["1.1.1.1", "8.8.8.8", "9.9.9.9"]));
    const inputs = dnsInputs(wrapper);
    expect(inputs).toHaveLength(3);
    expect((inputs[2].element as HTMLInputElement).value).toBe("9.9.9.9");
    expect(wrapper.text()).not.toContain("Add DNS server");
    wrapper.unmount();
  });

  it("shows Primary/Alternate with an add link when fewer are stored", async () => {
    const wrapper = await mountForm(stateWith(["1.1.1.1"]));
    expect(dnsInputs(wrapper)).toHaveLength(2);
    expect(wrapper.text()).toContain("Add DNS server");
    wrapper.unmount();
  });

  it("saves Primary/Alternate as an array and drops an emptied third", async () => {
    const stored = stateWith(["1.1.1.1", "8.8.8.8", "9.9.9.9"]);
    mockSave.mockImplementation(async (input) => ({ ...stored, network: { ...stored.network, ...input } }) as InstanceState);
    const wrapper = await mountForm(stored);
    const inputs = dnsInputs(wrapper);
    await inputs[1].setValue("1.0.0.1");
    await inputs[2].setValue("");
    await wrapper.find("form").trigger("submit.prevent");
    await flushPromises();
    await nextTick();
    expect(mockSave).toHaveBeenCalledTimes(1);
    expect(mockSave.mock.calls[0][0].dns_servers).toEqual(["1.1.1.1", "1.0.0.1"]);
    wrapper.unmount();
  });
});
