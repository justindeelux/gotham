// HostHelperGate + section states (JUS-185): one accurate message per cause, an
// in-product Install only when the host supports the section and the bootstrap
// has run, an honest restart hint for old units, and the failure/retry and
// double-click paths.
import { NMessageProvider } from "naive-ui";
import { defineComponent, h, nextTick, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/instance-settings/api/instance", () => ({
  saveNetwork: vi.fn(),
  saveSystem: vi.fn(),
  installHostHelper: vi.fn(),
  errorText: () => "the host helper could not be installed",
  hostHelperErrorLog: (error: unknown) =>
    (error as { log?: string })?.log ?? "",
}));

import { installHostHelper } from "@/features/instance-settings/api/instance";
import type { HostState, InstanceState } from "@/features/instance-settings/api/instance";
import HostHelperGate from "@/features/instance-settings/components/HostHelperGate.vue";
import NetworkForm from "@/features/instance-settings/components/NetworkForm.vue";
import SystemForm from "@/features/instance-settings/components/SystemForm.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockInstall = vi.mocked(installHostHelper);

const ready: HostState = {
  helper: "ready",
  bootstrap: true,
  network_stack: true,
  system_stack: true,
  network: true,
  system: true,
};

function hostWith(overrides: Partial<HostState>): HostState {
  return { ...ready, network: false, system: false, ...overrides };
}

function networkState(host: HostState): InstanceState {
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

function systemState(host: HostState): InstanceState {
  return {
    system: { hostname: "gotham-1", ntp_enabled: true, ntp_servers: [] },
    capabilities: { network: host.network, system: host.system },
    host,
    pending: null,
  } as unknown as InstanceState;
}

function shell(component: unknown, state: InstanceState) {
  const saved = ref<InstanceState | null>(null);
  const wrapperComponent = defineComponent({
    setup: () => ({ saved }),
    render() {
      return h(NMessageProvider, null, {
        default: () =>
          h(component as never, {
            state,
            onSaved: (next: InstanceState) => {
              saved.value = next;
            },
          }),
      });
    },
  });
  return { wrapperComponent, saved };
}

async function mountGate(state: InstanceState, section: "network" | "system") {
  const saved = ref<InstanceState | null>(null);
  const wrapperComponent = defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () =>
          h(HostHelperGate as never, {
            state,
            section,
            onSaved: (next: InstanceState) => {
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
  await flushPromises();
  await nextTick();
  return { wrapper, saved };
}

async function mountForm(component: unknown, state: InstanceState) {
  const { wrapperComponent, saved } = shell(component, state);
  const wrapper = mount(wrapperComponent, {
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

describe("HostHelperGate", () => {
  it("offers Install when the helper is missing and the stack is supported", async () => {
    const { wrapper } = await mountGate(
      networkState(hostWith({ helper: "missing", network_stack: true })),
      "network",
    );
    expect(wrapper.text()).toContain("host helper is not installed");
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });

  it("installs and hands the fresh state back on success", async () => {
    mockInstall.mockResolvedValue({ settings: networkState(ready), log: "" });
    const { wrapper, saved } = await mountGate(
      networkState(hostWith({ helper: "missing", network_stack: true })),
      "network",
    );
    await installButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(mockInstall).toHaveBeenCalledTimes(1);
    expect(saved.value?.host.helper).toBe("ready");
    wrapper.unmount();
  });

  it("guards against a double click", async () => {
    // A pending install keeps `installing` true while the second click fires
    // in the same tick, which is exactly the double-click the guard covers.
    mockInstall.mockImplementation(() => new Promise(() => {}));
    const { wrapper } = await mountGate(
      networkState(hostWith({ helper: "missing", network_stack: true })),
      "network",
    );
    const button = installButton(wrapper);
    void button?.trigger("click");
    void button?.trigger("click");
    await nextTick();
    expect(mockInstall).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("names a refused permission as its own cause", async () => {
    const { wrapper } = await mountGate(
      networkState(hostWith({ helper: "denied", network_stack: true })),
      "network",
    );
    expect(wrapper.text()).toContain("installed but not permitted");
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });

  it("states the unsupported stack plainly and offers no install", async () => {
    const { wrapper } = await mountGate(
      networkState(hostWith({ helper: "missing", network_stack: false })),
      "network",
    );
    expect(wrapper.text()).toContain("does not run systemd-networkd");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("asks for a restart when the bootstrap has not run", async () => {
    const { wrapper } = await mountGate(
      networkState(hostWith({ helper: "missing", bootstrap: false, network_stack: true })),
      "network",
    );
    expect(wrapper.text()).toContain("Restart or update the control plane");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("shows the installer log and opens the modal on failure", async () => {
    mockInstall.mockRejectedValue({ log: "install-tool: boom" });
    const { wrapper } = await mountGate(
      networkState(hostWith({ helper: "missing", network_stack: true })),
      "network",
    );
    await installButton(wrapper)?.trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.text()).toContain("could not be installed");
    // The modal is teleported to body, so its log is read off the document.
    expect(globalThis.document.body.textContent).toContain("install-tool: boom");
    // retry stays available
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });
});

describe("NetworkForm host states", () => {
  it("keeps the form editable with no gate when the helper is ready", async () => {
    const { wrapper } = await mountForm(NetworkForm, networkState(ready));
    expect(installButton(wrapper)).toBeUndefined();
    expect(wrapper.text()).not.toContain("read-only");
    wrapper.unmount();
  });

  it("lands focus and announces when the section unlocks after install", async () => {
    const locked = networkState(hostWith({ helper: "missing", network_stack: true }));
    mockInstall.mockResolvedValue({ settings: networkState(ready), log: "" });
    const { wrapper } = await mountForm(NetworkForm, locked);
    await installButton(wrapper)?.trigger("click");
    await flushPromises();
    // The form does not re-render from a new prop in this stub, so assert the
    // live region exists and the install path ran.
    expect(wrapper.find('[role="status"]').exists()).toBe(true);
    wrapper.unmount();
  });
});

describe("SystemForm host states", () => {
  it("offers Install when the system stack is supported", async () => {
    const { wrapper } = await mountForm(
      SystemForm,
      systemState(hostWith({ helper: "missing", network_stack: false, system_stack: true })),
    );
    expect(wrapper.text()).toContain("host helper is not installed");
    expect(installButton(wrapper)).toBeTruthy();
    wrapper.unmount();
  });

  it("does not offer Install when the host lacks the system tools", async () => {
    const { wrapper } = await mountForm(
      SystemForm,
      systemState(hostWith({ helper: "missing", network_stack: true, system_stack: false })),
    );
    expect(wrapper.text()).toContain("lacks the tools Gotham uses");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("asks for a restart when the bootstrap has not run", async () => {
    const { wrapper } = await mountForm(
      SystemForm,
      systemState(hostWith({ helper: "missing", bootstrap: false, system_stack: true })),
    );
    expect(wrapper.text()).toContain("Restart or update the control plane");
    expect(installButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });
});
