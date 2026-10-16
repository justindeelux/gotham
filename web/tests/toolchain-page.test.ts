// Toolchain App Store page (JUS-177, fix round 1): error state is
// exclusive with the table/empty states, and detect-only plus
// already-installed rows render a disabled checkbox with a visible reason.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useWebSocket } from "@/shared/composables/useWebSocket";

vi.mock("@/shared/composables/useWebSocket", () => ({
  useWebSocket: vi.fn(),
}));

vi.mock("@/features/toolchain/api/toolchain", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/toolchain/api/toolchain")>();
  return {
    ...actual,
    listTools: vi.fn(),
    listJobs: vi.fn(),
    enqueueJobs: vi.fn(),
    cancelJob: vi.fn(),
  };
});

vi.mock("@/features/servers/api/servers", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/servers/api/servers")>();
  return { ...actual, listServers: vi.fn().mockResolvedValue([]) };
});

import { useServersStore } from "@/features/servers/stores/servers";
import type { Server } from "@/features/servers";
import { listJobs, listTools, enqueueJobs } from "@/features/toolchain/api/toolchain";
import type { ToolchainTool } from "@/features/toolchain/api/toolchain";
import toolchainEn from "@/features/toolchain/locales/en";
import ToolchainPage from "@/features/toolchain/pages/ToolchainPage.vue";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const listToolsMock = vi.mocked(listTools);
const listJobsMock = vi.mocked(listJobs);
const enqueueJobsMock = vi.mocked(enqueueJobs);
const useWebSocketMock = vi.mocked(useWebSocket);

const socketStub = {
  subscribe: vi.fn(),
  unsubscribe: vi.fn(),
  close: vi.fn(),
};

function server(): Server {
  return {
    id: "srv-1",
    name: "alpha",
    ip: "10.0.0.2",
    port: 9442,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: null,
    os: null,
    docker_version: null,
    arch: null,
    total_mem: null,
    total_disk: null,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: null,
    last_seen: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  } as Server;
}

function tool(overrides: Partial<ToolchainTool> = {}): ToolchainTool {
  return {
    id: "docker",
    name: "Docker",
    category: "runtime",
    installed: false,
    installable: true,
    uninstallable: false,
    needs_root: true,
    version_ok: true,
    ...overrides,
  };
}

const Shell = defineComponent({
  setup: () => () => h(NMessageProvider, null, { default: () => h(ToolchainPage) }),
});

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { toolchain: toolchainEn });
  syncComposerLocale("en");
  setActivePinia(createPinia());
  listToolsMock.mockReset();
  listJobsMock.mockReset();
  listJobsMock.mockResolvedValue([]);
  enqueueJobsMock.mockReset();
  enqueueJobsMock.mockResolvedValue({ jobs: [], skipped: [] });
  socketStub.subscribe.mockClear();
  socketStub.unsubscribe.mockClear();
  socketStub.close.mockClear();
  useWebSocketMock.mockReset();
  useWebSocketMock.mockReturnValue(socketStub as never);
});

async function mountPage() {
  const wrapper = mount(Shell, { global: { plugins: [i18n] } });
  await flushPromises();
  return wrapper;
}

describe("error state", () => {
  it("shows the alert with retry and no table or empty state on load failure", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockRejectedValue(new Error("boom"));
    const wrapper = mount(Shell, { global: { plugins: [i18n] } });
    await flushPromises();
    expect(wrapper.find(".n-alert").exists()).toBe(true);
    expect(wrapper.find(".n-data-table").exists()).toBe(false);
    expect(wrapper.find(".n-empty").exists()).toBe(false);
    expect(wrapper.text()).toContain("Retry");
  });
});

describe("socket lifecycle", () => {
  it("releases the socket on unmount and re-establishes it on remount", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([tool({ id: "git", name: "Git" })]);
    const first = await mountPage();
    expect(useWebSocketMock).toHaveBeenCalledTimes(1);
    expect(socketStub.subscribe).toHaveBeenCalledWith("logs:srv-1:toolchain");
    expect(useToolchainStore().tools).toHaveLength(1);
    first.unmount();
    expect(socketStub.unsubscribe).toHaveBeenCalledWith("logs:srv-1:toolchain");
    expect(socketStub.close).toHaveBeenCalledTimes(1);
    // Remount keeps the node: the page must resubscribe (not just refresh),
    // opening exactly one new socket and one new channel subscription.
    useServersStore().servers = [server()];
    const second = await mountPage();
    expect(useWebSocketMock).toHaveBeenCalledTimes(2);
    expect(socketStub.subscribe).toHaveBeenCalledTimes(2);
    expect(useToolchainStore().tools).toHaveLength(1);
    second.unmount();
    expect(socketStub.close).toHaveBeenCalledTimes(2);
  });
});

describe("install selected", () => {
  it("enqueues only the selectable checked rows", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git" }),
      tool({ id: "docker", name: "Docker", installed: true, version: "27.1.1" }),
      tool({
        id: "probe",
        name: "Probe",
        installed: false,
        installable: false,
        uninstallable: false,
      }),
    ]);
    const wrapper = await mountPage();
    // Rows sort by name (Docker, Git, Probe); the header checkbox is first.
    // Table checkboxes are role=checkbox divs, not native inputs.
    const boxes = wrapper.findAll('.n-checkbox[role="checkbox"]');
    expect(boxes.length).toBe(4);
    await boxes[2].trigger("click");
    await flushPromises();
    expect(boxes[2].attributes("aria-checked")).toBe("true");
    const action = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Install selected"));
    expect(action).toBeDefined();
    await action?.trigger("click");
    await flushPromises();
    // The checked installed + detect-only rows are excluded server-side too.
    expect(enqueueJobsMock).toHaveBeenCalledWith("srv-1", ["git"], "install");
  });
});

describe("row selectability", () => {
  it("disables the checkbox for installed and detect-only rows only", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git" }),
      tool({ id: "docker", name: "Docker", installed: true, version: "27.1.1" }),
      tool({
        id: "probe",
        name: "Probe",
        installed: false,
        installable: false,
        uninstallable: false,
      }),
    ]);
    const wrapper = await mountPage();
    // One table (single category), three rows: exactly two disabled boxes.
    expect(wrapper.findAll(".n-data-table").length).toBe(1);
    expect(wrapper.findAll(".n-checkbox--disabled").length).toBe(2);
    // The disabled rows still explain themselves in text.
    expect(wrapper.text()).toContain("Detect-only");
    expect(wrapper.text()).toContain("Installed");
    // Column titles come from the catalog, not hard-coded English.
    expect(wrapper.text()).toContain("Tool");
    expect(wrapper.text()).toContain("Actions");
    expect(useToolchainStore().tools).toHaveLength(3);
  });
});
