import { NMessageProvider } from "naive-ui";
import { defineComponent, h, nextTick, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/servers/api/host", () => ({
  saveNodeNetwork: vi.fn(),
  saveNodeSystem: vi.fn(),
  installNodeHelper: vi.fn(),
}));

import { installNodeHelper, saveNodeSystem } from "@/features/servers/api/host";
import type { NodeCapability, NodeHostState } from "@/features/servers/api/host";
import HostHelperGate from "@/features/servers/components/host/HostHelperGate.vue";
import NetworkForm from "@/features/servers/components/host/NetworkForm.vue";
import SystemForm from "@/features/servers/components/host/SystemForm.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockInstall = vi.mocked(installNodeHelper);
const mockSaveSystem = vi.mocked(saveNodeSystem);

function stateWith(capability: NodeCapability, repair = false): NodeHostState {
  return {
    capability,
    repair,
    network: {
      dns_servers: ["1.1.1.1"],
      ipv4: { mode: "dhcp", address: "", gateway: "" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    },
    system: { hostname: "gotham-1", ntp_enabled: true, ntp_servers: [], timezone: "Europe/Berlin" },
    pending: null,
  };
}

function mountGate(state: NodeHostState) {
  const saved = ref<NodeHostState | null>(null);
  const wrapperComponent = defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () =>
          h(HostHelperGate as never, {
            state,
            serverId: "srv-1",
            onSaved: (origin: string, next: NodeHostState) => {
              expect(origin).toBe("srv-1");
              saved.value = next;
            },
          }),
      });
    },
  });
  const wrapper = mount(wrapperComponent, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
  return { wrapper, saved };
}

function mountForm(component: unknown, state: NodeHostState) {
  const wrapperComponent = defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(component as never, { state, serverId: "srv-1" }),
      });
    },
  });
  return mount(wrapperComponent, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
}

function installButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes("Install host helper"));
}

async function flushUi() {
  await flushPromises();
  await nextTick();
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
});

describe("HostHelperGate", () => {
  it("renders nothing when the node is ready", async () => {
    const { wrapper } = mountGate(stateWith("ready"));
    await flushUi();
    expect(wrapper.text()).toBe("");
    wrapper.unmount();
  });

  it("offers Install when the helper is missing and the repair path exists", async () => {
    const { wrapper } = mountGate(stateWith("helper_missing", true));
    await flushUi();
    expect(wrapper.text()).toContain("host helper is not installed");
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });

  it("installs and hands the fresh state back on success", async () => {
    mockInstall.mockResolvedValue({ state: stateWith("ready"), log: "" });
    const { wrapper, saved } = mountGate(stateWith("helper_missing", true));
    await flushUi();
    await installButton(wrapper)?.trigger("click");
    await flushUi();
    expect(mockInstall).toHaveBeenCalledWith("srv-1");
    expect(saved.value?.capability).toBe("ready");
    wrapper.unmount();
  });

  it("guards against a double click", async () => {
    mockInstall.mockImplementation(() => new Promise(() => {}));
    const { wrapper } = mountGate(stateWith("helper_missing", true));
    await flushUi();
    const button = installButton(wrapper);
    void button?.trigger("click");
    void button?.trigger("click");
    await nextTick();
    expect(mockInstall).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("does not offer an install for a remote node without the repair path", async () => {
    const { wrapper } = mountGate(stateWith("helper_missing", false));
    await flushUi();
    expect(wrapper.text()).toContain("agent installer on a remote node");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("states the unsupported stack plainly and offers no install", async () => {
    const { wrapper } = mountGate(stateWith("unsupported_stack"));
    await flushUi();
    expect(wrapper.text()).toContain("does not run the systemd network and time stack");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("explains a denied helper and offers no install", async () => {
    const { wrapper } = mountGate(stateWith("helper_denied"));
    await flushUi();
    expect(wrapper.text()).toContain("may not run the host helper");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("asks for an agent update when the agent is outdated", async () => {
    const { wrapper } = mountGate(stateWith("agent_outdated"));
    await flushUi();
    expect(wrapper.text()).toContain("agent is too old for host settings");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("reports an unreachable agent instead of a failing save", async () => {
    const { wrapper } = mountGate(stateWith("offline"));
    await flushUi();
    expect(wrapper.text()).toContain("agent is not reachable");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("shows the installer log and opens the modal on failure", async () => {
    mockInstall.mockRejectedValue({
      message: "the host helper could not be installed",
      cause: { response: { data: { log: "install-tool: boom" } } },
    });
    const { wrapper } = mountGate(stateWith("helper_missing", true));
    await flushUi();
    await installButton(wrapper)?.trigger("click");
    await flushUi();
    expect(wrapper.text()).toContain("could not be installed");
    expect(globalThis.document.body.textContent).toContain("install-tool: boom");
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });
});

describe("host forms while the node is not ready", () => {
  it("keeps the network form read-only and offers no submit", async () => {
    const wrapper = mountForm(NetworkForm, stateWith("offline"));
    await flushUi();
    expect(wrapper.find('input[placeholder="1.1.1.1"]').attributes("disabled")).toBeDefined();
    const submit = wrapper.findAll("button").find((button) => button.text().includes("Apply network changes"));
    expect(submit?.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("keeps the system form read-only with the node's own timezone", async () => {
    const wrapper = mountForm(SystemForm, stateWith("agent_outdated"));
    await flushUi();
    expect(wrapper.find('input[placeholder="gotham-1"]').attributes("disabled")).toBeDefined();
    expect(wrapper.text()).toContain("Europe/Berlin");
    wrapper.unmount();
  });

  it("announces the unlock and focuses the first field after the helper install", async () => {
    mockInstall.mockResolvedValue({ state: stateWith("ready"), log: "" });
    const state = ref<NodeHostState>(stateWith("helper_missing", true));
    const wrapperComponent = defineComponent({
      setup: () => ({ state }),
      render() {
        return h(NMessageProvider, null, {
          default: () => [
            h(HostHelperGate as never, {
              state: state.value,
              serverId: "srv-1",
              onSaved: (_origin: string, next: NodeHostState) => {
                state.value = next;
              },
            }),
            h(NetworkForm as never, { state: state.value, serverId: "srv-1" }),
          ],
        });
      },
    });
    const wrapper = mount(wrapperComponent, {
      attachTo: globalThis.document.body,
      global: { plugins: [i18n], stubs: { transition: false } },
    });
    await flushUi();
    await installButton(wrapper)?.trigger("click");
    await flushUi();
    expect(wrapper.find('[role="status"]').text()).toContain("Host helper installed");
    expect(wrapper.find('input[placeholder="1.1.1.1"]').attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });
});

describe("SystemForm timezone", () => {
  it("saves the node's timezone to that server", async () => {
    const stored = stateWith("ready");
    mockSaveSystem.mockResolvedValue(stored);
    const wrapper = mountForm(SystemForm, stored);
    await flushUi();
    const submit = wrapper.findAll("button").find((button) => button.text().includes("Save system settings"));
    await submit?.trigger("click");
    await flushUi();
    expect(mockSaveSystem).toHaveBeenCalledTimes(1);
    expect(mockSaveSystem.mock.calls[0][0]).toBe("srv-1");
    expect(mockSaveSystem.mock.calls[0][1].timezone).toBe("Europe/Berlin");
    wrapper.unmount();
  });
});
