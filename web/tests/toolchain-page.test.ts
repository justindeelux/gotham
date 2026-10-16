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
    // The enqueue opens the queue modal (teleported focus trap): unmount so
    // later tests own document focus.
    wrapper.unmount();
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

describe("category tabs (JUS-189)", () => {
  function tabs(wrapper: ReturnType<typeof mount>) {
    return wrapper.findAll('[role="tab"]');
  }

  it("renders one tab per category with counts and keeps selection across switches", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git", category: "build" }),
      tool({ id: "make", name: "Make", category: "build" }),
      tool({ id: "curl", name: "Curl", category: "network" }),
    ]);
    const wrapper = await mountPage();
    expect(wrapper.find('[role="tablist"]').exists()).toBe(true);
    expect(tabs(wrapper).map((tab) => tab.text())).toEqual(["build (2)", "network (1)"]);
    expect(tabs(wrapper)[0].attributes("aria-selected")).toBe("true");
    expect(tabs(wrapper)[0].attributes("tabindex")).toBe("0");
    expect(tabs(wrapper)[1].attributes("tabindex")).toBe("-1");
    expect(wrapper.find('[role="tabpanel"]').exists()).toBe(true);
    // Build tab sorts by name (Git, Make); header checkbox is first.
    let boxes = wrapper.findAll('.n-checkbox[role="checkbox"]');
    await boxes[1].trigger("click");
    await flushPromises();
    // Switch to the network tab: only Curl shows, Git stays checked.
    await tabs(wrapper)[1].trigger("click");
    await flushPromises();
    expect(tabs(wrapper)[1].attributes("aria-selected")).toBe("true");
    expect(tabs(wrapper)[0].attributes("tabindex")).toBe("-1");
    const visibleRows = wrapper.findAll("tbody tr");
    expect(visibleRows).toHaveLength(1);
    expect(visibleRows[0].text()).toContain("Curl");
    await wrapper.findAll('.n-checkbox[role="checkbox"]')[1].trigger("click");
    await flushPromises();
    // Switch back: the build tab badge reports the selection, Git is kept.
    await tabs(wrapper)[0].trigger("click");
    await flushPromises();
    expect(tabs(wrapper)[0].text()).toContain("1 selected");
    boxes = wrapper.findAll('.n-checkbox[role="checkbox"]');
    expect(boxes[1].attributes("aria-checked")).toBe("true");
    // One bulk install covers tools from both tabs.
    const action = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Install selected"));
    await action?.trigger("click");
    await flushPromises();
    expect(enqueueJobsMock).toHaveBeenCalledWith("srv-1", ["git", "curl"], "install");
    wrapper.unmount();
  });

  it("moves and activates tabs with ArrowLeft/Right/Home/End", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git", category: "build" }),
      tool({ id: "curl", name: "Curl", category: "network" }),
      tool({ id: "node", name: "Node", category: "runtime" }),
    ]);
    const wrapper = await mountPage();
    // Focus assertions need a live document (detached nodes never take focus).
    globalThis.document.body.appendChild(wrapper.element);
    const tablist = wrapper.find('[role="tablist"]');
    expect(tabs(wrapper)[0].attributes("aria-selected")).toBe("true");
    await tablist.trigger("keydown", { key: "ArrowRight" });
    await flushPromises();
    expect(tabs(wrapper)[1].attributes("aria-selected")).toBe("true");
    expect(tabs(wrapper)[1].attributes("tabindex")).toBe("0");
    expect(globalThis.document.activeElement).toBe(tabs(wrapper)[1].element);
    expect(wrapper.findAll("tbody tr")[0].text()).toContain("Curl");
    await tablist.trigger("keydown", { key: "End" });
    await flushPromises();
    expect(tabs(wrapper)[2].attributes("aria-selected")).toBe("true");
    await tablist.trigger("keydown", { key: "ArrowRight" });
    await flushPromises();
    expect(tabs(wrapper)[0].attributes("aria-selected")).toBe("true");
    await tablist.trigger("keydown", { key: "ArrowLeft" });
    await flushPromises();
    expect(tabs(wrapper)[2].attributes("aria-selected")).toBe("true");
    await tablist.trigger("keydown", { key: "Home" });
    await flushPromises();
    expect(tabs(wrapper)[0].attributes("aria-selected")).toBe("true");
    expect(globalThis.document.activeElement).toBe(tabs(wrapper)[0].element);
    wrapper.unmount();
  });

  it("counts only selectable rows in the tab badge and footer", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git", category: "build" }),
    ]);
    const wrapper = await mountPage();
    const boxes = wrapper.findAll('.n-checkbox[role="checkbox"]');
    await boxes[1].trigger("click");
    await flushPromises();
    expect(tabs(wrapper)[0].text()).toContain("1 selected");
    // Git reports back installed: the row is pruned, so both badge and
    // footer fall back instead of disagreeing.
    useToolchainStore().tools = [
      tool({ id: "git", name: "Git", category: "build", installed: true, version: "2.47.1" }),
    ];
    await flushPromises();
    expect(tabs(wrapper)[0].text()).not.toContain("selected");
    expect(wrapper.find(".footer-status").text()).toContain("Nothing selected yet");
  });

  it("filters the tabs by search and shows per-tab match counts", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git", category: "build" }),
      tool({ id: "make", name: "Make", category: "build" }),
      tool({ id: "curl", name: "Curl", category: "network" }),
    ]);
    const wrapper = await mountPage();
    const input = wrapper.find(".search-input input");
    await input.setValue("git");
    await flushPromises();
    expect(tabs(wrapper)[0].text()).toContain("1 of 2");
    expect(tabs(wrapper)[1].text()).toContain("0 of 1");
    // Active tab shows only the match; the other tab's hit stays discoverable.
    expect(wrapper.findAll("tbody tr")).toHaveLength(1);
    await tabs(wrapper)[1].trigger("click");
    await flushPromises();
    expect(wrapper.find(".n-empty").exists()).toBe(true);
  });

  it("keeps node select and search in the page header and queue actions in a live footer", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git", category: "build" }),
    ]);
    const wrapper = await mountPage();
    const head = wrapper.find(".page-head");
    expect(head.find(".node-select").exists()).toBe(true);
    expect(head.find(".search-input").exists()).toBe(true);
    const footer = wrapper.find(".table-footer");
    expect(footer.exists()).toBe(true);
    expect(footer.text()).toContain("Nothing selected yet");
    const status = footer.find('[role="status"]');
    expect(status.attributes("aria-live")).toBe("polite");
    const boxes = wrapper.findAll('.n-checkbox[role="checkbox"]');
    await boxes[1].trigger("click");
    await flushPromises();
    expect(footer.text()).toContain("1 selected");
    const action = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Install selected"));
    expect(action).toBeDefined();
    const queue = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Queue"));
    expect(queue).toBeDefined();
  });

  it("keeps Queue reachable when the tool list is empty but jobs run", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([]);
    const wrapper = await mountPage();
    useToolchainStore().applyJob({
      id: "job-1",
      server_id: "srv-1",
      tool_id: "docker",
      action: "install",
      status: "queued",
      created_at: "2026-10-16T12:00:00Z",
    });
    await flushPromises();
    expect(wrapper.text()).toContain("No tools reported for this node");
    const footer = wrapper.find(".table-footer");
    expect(footer.exists()).toBe(true);
    const queue = footer
      .findAll("button")
      .find((button) => button.text().includes("Queue"));
    expect(queue).toBeDefined();
    expect(queue?.attributes("disabled")).toBeUndefined();
  });
});

describe("platform tools", () => {
  it("hides uninstall even for an older agent and explains the impact", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git", installed: true, uninstallable: true }),
      tool({ installed: true, uninstallable: true }),
      tool({ id: "pack", name: "Pack", installed: true, uninstallable: true }),
    ]);
    const wrapper = await mountPage();
    const rows = wrapper.findAll("tbody tr");
    for (const name of ["Git", "Docker"]) {
      const row = rows.find((item) => item.text().includes(name));
      expect(row?.text()).toContain("Required");
      expect(row?.text()).toContain("Cannot be uninstalled");
      expect(row?.findAll("button")).toHaveLength(0);
    }
    const uninstall = wrapper.findAll("button").filter((button) => button.text() === "Uninstall");
    expect(uninstall).toHaveLength(1);
    await uninstall[0].trigger("click");
    await flushPromises();
    expect(enqueueJobsMock).toHaveBeenCalledWith("srv-1", ["pack"], "uninstall");
    wrapper.unmount();
  });

  it("offers install/update for outdated git through the existing install action", async () => {
    useServersStore().servers = [server()];
    listToolsMock.mockResolvedValue([
      tool({ id: "git", name: "Git", installed: true, version: "2.34.1", version_ok: false }),
    ]);
    const wrapper = await mountPage();
    expect(wrapper.text()).toContain("Update available");
    const update = wrapper.findAll("button").find((button) => button.text() === "Install / Update");
    expect(update).toBeDefined();
    await update?.trigger("click");
    await flushPromises();
    expect(enqueueJobsMock).toHaveBeenCalledWith("srv-1", ["git"], "install");
    wrapper.unmount();
  });
});
