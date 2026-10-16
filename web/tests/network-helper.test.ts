// NetworkForm host-helper states (JUS-185): one accurate message per cause,
// an in-product Install action only when the host supports the stack, and no
// action when the host does not run systemd-networkd/resolved.
import { NMessageProvider } from "naive-ui";
import { defineComponent, h, nextTick, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/instance-settings/api/instance", () => ({
  saveNetwork: vi.fn(),
  installHostHelper: vi.fn(),
  errorText: (error: unknown) => String(error),
  hostHelperErrorLog: () => "",
}));

import {
  installHostHelper,
  saveNetwork,
} from "@/features/instance-settings/api/instance";
import type { HostState, InstanceState } from "@/features/instance-settings/api/instance";
import NetworkForm from "@/features/instance-settings/components/NetworkForm.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockInstall = vi.mocked(installHostHelper);
vi.mocked(saveNetwork);

function stateWith(host: HostState): InstanceState {
  return {
    network: {
      dns_servers: ["1.1.1.1"],
      ipv4: { mode: "dhcp", address: "", gateway: "" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    },
    capabilities: { network: host.network, system: host.system },
    host,
    pending: null,
  } as unknown as InstanceState;
}

const ready: HostState = {
  helper: "ready",
  network_stack: true,
  system_stack: true,
  network: true,
  system: true,
};

function shell(state: InstanceState) {
  const saved = ref<InstanceState | null>(null);
  const component = defineComponent({
    setup: () => ({ saved }),
    render() {
      return h(NMessageProvider, null, {
        default: () =>
          h(NetworkForm as never, {
            state,
            onSaved: (next: InstanceState) => {
              saved.value = next;
            },
          }),
      });
    },
  });
  return { component, saved };
}

async function mountForm(state: InstanceState) {
  const { component, saved } = shell(state);
  const wrapper = mount(component, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
  await flushPromises();
  await nextTick();
  return { wrapper, saved };
}

function installButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes("Install host helper"));
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
});

describe("NetworkForm host-helper states", () => {
  it("offers Install when the helper is missing and the host supports the stack", async () => {
    const { wrapper } = await mountForm(
      stateWith({ helper: "missing", network_stack: true, system_stack: true, network: false, system: false }),
    );
    expect(wrapper.text()).toContain("host helper is not installed");
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });

  it("installs the helper and re-renders from the returned state", async () => {
    const missing = stateWith({
      helper: "missing",
      network_stack: true,
      system_stack: true,
      network: false,
      system: false,
    });
    mockInstall.mockResolvedValue({ settings: stateWith(ready), log: "installed gotham-hostctl" });
    const { wrapper, saved } = await mountForm(missing);
    await installButton(wrapper)?.trigger("click");
    await flushPromises();
    await nextTick();
    expect(mockInstall).toHaveBeenCalledTimes(1);
    expect(saved.value?.host.helper).toBe("ready");
    wrapper.unmount();
  });

  it("names the refused permission as its own cause", async () => {
    const { wrapper } = await mountForm(
      stateWith({ helper: "denied", network_stack: true, system_stack: true, network: false, system: false }),
    );
    expect(wrapper.text()).toContain("installed but not permitted");
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });

  it("states the unsupported stack plainly and offers no install", async () => {
    const { wrapper } = await mountForm(
      stateWith({ helper: "missing", network_stack: false, system_stack: false, network: false, system: false }),
    );
    expect(wrapper.text()).toContain("does not run systemd-networkd");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("keeps the form editable with no alert when the helper is ready", async () => {
    const { wrapper } = await mountForm(stateWith(ready));
    expect(installButton(wrapper)).toBeUndefined();
    expect(wrapper.text()).not.toContain("read-only");
    wrapper.unmount();
  });
});
