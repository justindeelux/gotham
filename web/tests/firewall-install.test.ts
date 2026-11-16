import { NMessageProvider } from "naive-ui";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/firewall/api/firewall", () => ({
  getFirewallStatus: vi.fn(),
  listFirewallRules: vi.fn(),
  installFirewall: vi.fn(),
  setFirewallEnabled: vi.fn(),
  addFirewallRule: vi.fn(),
  updateFirewallRule: vi.fn(),
  deleteFirewallRule: vi.fn(),
}));

import {
  getFirewallStatus,
  installFirewall,
  listFirewallRules,
} from "@/features/firewall/api/firewall";
import type { FirewallStatus } from "@/features/firewall/api/firewall";
import ServerFirewallTab from "@/features/firewall/components/ServerFirewallTab.vue";
import en from "@/features/firewall/locales/en";
import viLocale from "@/features/firewall/locales/vi";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockStatus = vi.mocked(getFirewallStatus);
const mockRules = vi.mocked(listFirewallRules);
const mockInstall = vi.mocked(installFirewall);

function status(overrides: Partial<FirewallStatus>): FirewallStatus {
  return {
    backend: "none",
    available: false,
    enabled: false,
    manageable: false,
    ssh_ports: [22],
    agent_port: 9443,
    control_plane_port: 8000,
    ...overrides,
  };
}

function installed(): FirewallStatus {
  return status({
    backend: "ufw",
    available: true,
    enabled: true,
    manageable: true,
  });
}

function apiError(code: string): unknown {
  return {
    status: 409,
    message: "the node refused the change",
    cause: { response: { data: { code } } },
  };
}

function mountTab() {
  const wrapperComponent = defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(ServerFirewallTab, { serverId: "srv-1" }),
      });
    },
  });
  return mount(wrapperComponent, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
}

function buttons() {
  return Array.from(globalThis.document.body.querySelectorAll("button"));
}

function button(text: string) {
  return buttons().find((item) => item.textContent?.trim() === text);
}

async function flushUi(): Promise<void> {
  await flushPromises();
  await nextTick();
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  mockStatus.mockReset();
  mockRules.mockReset();
  mockInstall.mockReset();
  mockRules.mockResolvedValue([]);
  globalThis.document.body.innerHTML = "";
});

describe("firewall install", () => {
  it("offers Install only when the node has no firewall", async () => {
    mockStatus.mockResolvedValue(
      status({ detail_code: "no_firewall", detail: "No supported firewall is installed on this node." }),
    );
    const wrapper = mountTab();
    await flushUi();
    expect(button(en.status.install)).toBeDefined();
    wrapper.unmount();
  });

  it("hides Install for a backend Gotham cannot edit", async () => {
    mockStatus.mockResolvedValue(
      status({ backend: "nftables", detail_code: "unsupported_backend" }),
    );
    const wrapper = mountTab();
    await flushUi();
    expect(button(en.status.install)).toBeUndefined();
    wrapper.unmount();
  });

  it("confirms, installs and shows the working firewall", async () => {
    mockStatus.mockResolvedValue(
      status({ detail_code: "no_firewall", detail: "No supported firewall is installed on this node." }),
    );
    mockInstall.mockResolvedValue({ status: installed(), rules: [] });
    const wrapper = mountTab();
    await flushUi();

    button(en.status.install)?.click();
    await flushUi();
    expect(globalThis.document.body.textContent).toContain(en.status.installTitle);

    button(en.status.installConfirm)?.click();
    await flushUi();
    expect(mockInstall).toHaveBeenCalledWith("srv-1");
    expect(globalThis.document.body.textContent).toContain(en.toasts.installed);
    expect(globalThis.document.body.textContent).toContain(en.rules.title);
    wrapper.unmount();
  });

  it("shows the translated refusal on an unsupported distribution", async () => {
    mockStatus.mockResolvedValue(
      status({ detail_code: "no_firewall", detail: "No supported firewall is installed on this node." }),
    );
    mockInstall.mockRejectedValue(apiError("install_unsupported"));
    const wrapper = mountTab();
    await flushUi();

    button(en.status.install)?.click();
    await flushUi();
    button(en.status.installConfirm)?.click();
    await flushUi();
    expect(globalThis.document.body.textContent).toContain(en.errors.install_unsupported);
    wrapper.unmount();
  });

  it("shows no English or raw key when the UI is Vietnamese", async () => {
    syncComposerLocale("vi");
    mockStatus.mockResolvedValue(
      status({ detail_code: "no_firewall", detail: "No supported firewall is installed on this node." }),
    );
    mockInstall.mockRejectedValue(apiError("install_unsupported"));
    const wrapper = mountTab();
    await flushUi();

    expect(globalThis.document.body.textContent).toContain(viLocale.status.install);
    button(viLocale.status.install)?.click();
    await flushUi();
    expect(globalThis.document.body.textContent).toContain(viLocale.status.installTitle);
    button(viLocale.status.installConfirm)?.click();
    await flushUi();

    const text = globalThis.document.body.textContent ?? "";
    expect(text).toContain(viLocale.errors.install_unsupported);
    expect(text).not.toContain(en.status.install);
    expect(text).not.toContain("install_unsupported");
    wrapper.unmount();
  });
});
