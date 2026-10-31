import { NMessageProvider } from "naive-ui";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/servers/api/host", () => ({
  saveNodeAgentRoot: vi.fn(),
}));

import { saveNodeAgentRoot } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import AgentRootForm from "@/features/servers/components/host/AgentRootForm.vue";
import { i18n, registerDiscoveredCatalogs, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const mockSave = vi.mocked(saveNodeAgentRoot);

function stateWith(root: boolean): NodeHostState {
  return {
    capability: "ready",
    repair: true,
    network: {
      dns_servers: [],
      ipv4: { mode: "dhcp", address: "", gateway: "" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    },
    system: { hostname: "gotham-1", ntp_enabled: true, ntp_servers: [], timezone: "UTC" },
    ssh: { available: true, port: 22, password_authentication: true, key_auth_available: true },
    agent_root: root,
    pending: null,
  };
}

function mountForm(state: NodeHostState) {
  const shell = defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(AgentRootForm as never, { state, serverId: "srv-1" }),
      });
    },
  });
  return mount(shell, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { teleport: true, transition: false } },
  });
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

describe("AgentRootForm", () => {
  it("translates the agent-restarting conflict", async () => {
    mockSave.mockRejectedValue({
      status: 409,
      message: "instance: the agent is restarting; reload when it reconnects",
      cause: { response: { data: { code: "agent_restarting" } } },
    });
    const wrapper = mountForm(stateWith(false));
    await flushUi();

    await wrapper.find('[role="switch"]').trigger("click");
    await flushUi();
    const confirm = wrapper.findAll("button").find((button) => button.text().includes("Change access"));
    expect(confirm).toBeTruthy();
    await confirm?.trigger("click");
    await flushUi();

    expect(wrapper.text()).toContain("The agent is restarting. Wait a moment, then reload this page.");
    expect(wrapper.text()).not.toContain("instance: the agent is restarting");
    wrapper.unmount();
  });
});
