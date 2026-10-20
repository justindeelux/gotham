// JUS-246: the standalone Terminal page, the app-level paused-session dock and
// the lazy server-detail tab. The transport and API seams are faithful fakes
// (they record creates, deletes and server ids) so request counts and target
// identity are asserted, not only rendered text.
/* global document:readonly, HTMLElement:readonly, window:readonly */
import { NSelect } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import type { Pinia } from "pinia";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import enTerminal from "../src/features/terminal/locales/en";
import viTerminal from "../src/features/terminal/locales/vi";

/** runtimeFakes records every renderer the store creates. */
const runtimeFakes = vi.hoisted(() => {
  class FakeRuntime {
    options: {
      onState: (_patch: Record<string, unknown>) => void;
      session: { id: string };
    };
    host: HTMLElement;
    attached: HTMLElement | null = null;
    disposed = false;

    constructor(options: FakeRuntime["options"]) {
      this.options = options;
      this.host = document.createElement("div");
      this.host.className = "term-host";
      instances.push(this);
    }

    async connect(): Promise<void> {
      this.options.onState({ status: "connected", shell: "/bin/bash" });
    }

    attach(container: HTMLElement): void {
      this.attached = container;
      container.appendChild(this.host);
    }

    focus(): void {}

    fit(): void {}

    dispose(): void {
      this.disposed = true;
      this.host.remove();
    }
  }
  const instances: FakeRuntime[] = [];
  return { FakeRuntime, instances };
});

/** apiFakes is the HTTP seam; creates/deletes are counted per server. */
const apiFakes = vi.hoisted(() => ({
  create: vi.fn(),
  close: vi.fn(),
}));

vi.mock("@/features/terminal/transport/terminalRuntime", () => ({
  TerminalRuntime: runtimeFakes.FakeRuntime,
}));

vi.mock("@/features/terminal/api/terminal", () => ({
  createTerminalSession: apiFakes.create,
  closeTerminalSession: apiFakes.close,
  currentTerminalToken: () => "jwt-token",
  terminalFailure: () => ({ code: "generic", message: "start failed" }),
  terminalWebSocketUrl: (path: string) => `ws://test${path}`,
}));

/** serverApiFakes replaces the detail read with a deterministic row. */
const serverApiFakes = vi.hoisted(() => ({ getServer: vi.fn() }));

vi.mock("@/features/servers/api/servers", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/servers/api/servers")>();
  return { ...actual, getServer: serverApiFakes.getServer };
});

vi.mock("@/features/servers/composables/useServerMetrics", () => ({
  ServerMetricsKey: Symbol("server-metrics"),
  useServerMetrics: () => ({
    context: { active: false },
    resetForServer: () => undefined,
  }),
}));

// useMessage needs an NMessageProvider ancestor, which the app shell provides.
// The page calls it at setup; give the page the same toast surface shape.
vi.mock("naive-ui", async (importOriginal) => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useMessage: () => ({
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      info: vi.fn(),
    }),
  };
});

const { useTerminalStore } = await import("@/features/terminal/stores/terminal");
const { useServersStore } = await import("@/features/servers/stores/servers");
const { useAuthStore } = await import("@/features/auth");
const { default: TerminalPage } = await import(
  "../src/features/terminal/pages/TerminalPage.vue"
);
const { default: TerminalPageDock } = await import(
  "../src/features/terminal/components/TerminalPageDock.vue"
);

/** serverRow builds one managed server. */
function serverRow(
  id: string,
  name: string,
  status: "ready" | "offline" | "pending" = "ready",
) {
  return {
    id,
    name,
    ip: `10.0.0.${id.slice(-1)}`,
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status,
    node_id: id,
    os: "linux",
    docker_version: "27",
    arch: "amd64",
    total_mem: 1,
    total_disk: 1,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: 0,
    last_seen: null,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

const SERVER_A = serverRow("11111111-1111-4111-8111-111111111111", "prod-01");
const SERVER_B = serverRow("22222222-2222-4222-8222-222222222222", "prod-02");
const SERVER_OFFLINE = serverRow(
  "33333333-3333-4333-8333-333333333333",
  "prod-03",
  "offline",
);

/** stub is the lazy route component used outside the page under test. */
const stub = { template: "<div />" };

/** pageRouter is the minimal route table (names match the app router). */
function pageRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/terminal", name: "terminal", component: stub },
      { path: "/dashboard", name: "dashboard", component: stub },
      { path: "/servers", name: "servers", component: stub },
      {
        path: "/servers/:id",
        name: "server-detail",
        component: stub,
      },
    ],
  });
}

let seq = 0;

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  document.body.innerHTML = "";
  window.localStorage.clear();
  setActivePinia(createPinia());
  runtimeFakes.instances.length = 0;
  seq = 0;
  apiFakes.create.mockReset();
  apiFakes.close.mockReset();
  apiFakes.close.mockResolvedValue(undefined);
  apiFakes.create.mockImplementation(
    async (
      serverId: string,
      input: { target?: string; root?: string; cwd?: string; cols: number; rows: number },
    ) => {
      seq += 1;
      return {
        id: `session-${seq}`,
        server_id: serverId,
        target: input.target ?? "host",
        root: input.root ?? "volumes",
        cwd: input.cwd ?? "app",
        cols: input.cols,
        rows: input.rows,
        expires_at: "2026-10-20T12:01:00Z",
        websocket_path: `/api/v1/servers/${serverId}/terminal/sessions/session-${seq}/ws`,
        subprotocol: "gotham.terminal.v1",
      };
    },
  );
  useServersStore().servers = [SERVER_A, SERVER_B, SERVER_OFFLINE];
  serverApiFakes.getServer.mockReset();
  serverApiFakes.getServer.mockImplementation(async (id: string) => {
    const row = [SERVER_A, SERVER_B, SERVER_OFFLINE].find(
      (server) => server.id === id,
    );
    if (!row) {
      throw new Error("not found");
    }
    return row;
  });
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

/** login sets the platform-operator flag the page and route guard read. */
function login(pinia: Pinia, isAdmin = true): void {
  setActivePinia(pinia);
  useAuthStore().user = {
    id: "u-1",
    email: "ada@gotham.dev",
    display_name: "Ada",
    created_at: "2026-01-01T00:00:00Z",
    is_platform_admin: isAdmin,
  };
}

/** mountPage mounts the standalone page on one URL. */
async function mountPage(path = "/terminal") {
  const pinia = createPinia();
  setActivePinia(pinia);
  login(pinia);
  useServersStore().servers = [SERVER_A, SERVER_B, SERVER_OFFLINE];
  const router = pageRouter();
  await router.push(path);
  await router.isReady();
  const wrapper = mount(TerminalPage, {
    attachTo: document.body,
    global: { plugins: [pinia, router, i18n] },
  });
  await flushPromises();
  await nextTick();
  return { wrapper, router, terminal: useTerminalStore() };
}

describe("terminal catalogs (JUS-246)", () => {
  it("keeps EN/VI parity for the page and dock keys", () => {
    expect(checkCatalogParity(enTerminal, viTerminal)).toEqual([]);
    const leafKeys = (dict: Record<string, unknown>, prefix = ""): string[] =>
      Object.entries(dict).flatMap(([key, value]) => {
        const next = prefix === "" ? key : `${prefix}.${key}`;
        return typeof value === "object" && value !== null
          ? leafKeys(value as Record<string, unknown>, next)
          : [next];
      });
    const keys = leafKeys(enTerminal);
    for (const required of [
      "page.navLabel",
      "page.title",
      "page.tabLabel",
      "page.notReadyTitle",
      "page.notReadyBody",
      "dock.heading",
      "dock.paused",
      "dock.resume",
      "rail.pageWithCount",
      "shortcuts.hint",
    ]) {
      expect(keys).toContain(required);
    }
  });
});

describe("standalone Terminal page (JUS-246)", () => {  it("creates nothing before a server is chosen, then exactly one shell", async () => {
    const { wrapper, router, terminal } = await mountPage("/terminal");
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(wrapper.find(".term-surface").exists()).toBe(false);

    const cards = wrapper.findAll(".server-pick");
    expect(cards).toHaveLength(3);
    expect(cards[2]!.text()).toContain("Offline");

    await cards[0]!.trigger("click");
    await flushPromises();
    await nextTick();

    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.create.mock.calls[0]?.[0]).toBe(SERVER_A.id);
    expect(router.currentRoute.value.query.server).toBe(SERVER_A.id);
    const tab = terminal.pageTabs[0]!;
    expect(tab.serverId).toBe(SERVER_A.id);
    expect(tab.mode).toBe("page");
    expect(tab.sessionId).toBe("session-1");
    expect(tab.sourcePath).toContain(`server=${SERVER_A.id}`);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    expect(runtimeFakes.instances[0]?.attached).not.toBeNull();

    // Choosing the same server again is a no-op, not a second shell.
    wrapper.findComponent(NSelect).vm.$emit("update:value", SERVER_A.id);
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
  });

  it("re-entering the route reuses the live session instead of duplicating it", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    login(pinia);
    useServersStore().servers = [SERVER_A, SERVER_B];
    const router = pageRouter();
    await router.push(`/terminal?server=${SERVER_A.id}`);
    await router.isReady();
    const first = mount(TerminalPage, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    });
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const tabId = useTerminalStore().pageTabs[0]!.id;
    first.unmount();

    // The same store (the app shell survives a route change) reopens the
    // source route: the session is reattached, never recreated.
    const second = mount(TerminalPage, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    });
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(useTerminalStore().pageTabs[0]!.id).toBe(tabId);
    expect(second.find(".term-surface").exists()).toBe(true);
    second.unmount();
  });

  it("switching servers confirms before releasing the page shell", async () => {
    const { wrapper, router, terminal } = await mountPage(
      `/terminal?server=${SERVER_A.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    // Cancel: the old shell stays, the selector stays on the old server.
    wrapper.findComponent(NSelect).vm.$emit("update:value", SERVER_B.id);
    await flushPromises();
    expect(terminal.closeRequest?.ids).toHaveLength(1);
    terminal.cancelClose();
    await flushPromises();
    await nextTick();
    expect(router.currentRoute.value.query.server).toBe(SERVER_A.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.close).not.toHaveBeenCalled();

    // Confirm: exactly one DELETE for the old shell, one create for the new.
    wrapper.findComponent(NSelect).vm.$emit("update:value", SERVER_B.id);
    await flushPromises();
    terminal.confirmClose();
    await flushPromises();
    await nextTick();
    expect(apiFakes.close).toHaveBeenCalledTimes(1);
    expect(apiFakes.close.mock.calls[0]?.[0]).toBe(SERVER_A.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[0]).toBe(SERVER_B.id);
    expect(router.currentRoute.value.query.server).toBe(SERVER_B.id);
    expect(terminal.pageTabs).toHaveLength(1);
    expect(terminal.pageTabs[0]!.serverId).toBe(SERVER_B.id);
  });

  it("never dials an offline node and starts once it becomes ready", async () => {
    const { wrapper, terminal } = await mountPage(
      `/terminal?server=${SERVER_OFFLINE.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(wrapper.find(".term-surface").exists()).toBe(false);
    expect(wrapper.text()).toContain("Terminal unavailable");
    expect(wrapper.text()).toContain("Offline");

    useServersStore().servers = [
      SERVER_A,
      SERVER_B,
      serverRow(SERVER_OFFLINE.id, "prod-03", "ready"),
    ];
    await flushPromises();
    await nextTick();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.create.mock.calls[0]?.[0]).toBe(SERVER_OFFLINE.id);
    expect(terminal.pageTabs[0]!.serverId).toBe(SERVER_OFFLINE.id);
  });

  it("returns a detached session as a one-way notice without a new shell", async () => {
    const { wrapper, terminal } = await mountPage(
      `/terminal?server=${SERVER_A.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    await wrapper.find(".term-open-in-window").trigger("click");
    await flushPromises();
    expect(terminal.pageTabs[0]?.mode ?? "window").toBe("window");
    expect(terminal.windowTabs).toHaveLength(1);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".term-detached").exists()).toBe(true);
    expect(wrapper.text()).toContain("open in the window");
    // There is deliberately no window → page action.
    expect(wrapper.find(".term-open-in-page").exists()).toBe(false);
  });
});

describe("paused page terminal dock (JUS-246)", () => {
  it("shows a page-only session off-route, resumes it and never re-creates it", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    login(pinia);
    useServersStore().servers = [SERVER_A];
    const router = pageRouter();
    await router.push(`/terminal?server=${SERVER_A.id}`);
    await router.isReady();
    const page = mount(TerminalPage, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    });
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const tabId = useTerminalStore().pageTabs[0]!.id;

    // Leaving the route tears the pane down but keeps the PTY.
    page.unmount();
    await router.push("/dashboard");
    const dock = mount(TerminalPageDock, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    });
    await flushPromises();
    expect(dock.find(".term-dock").exists()).toBe(true);
    expect(dock.text()).toContain("Paused");
    expect(dock.text()).toContain("prod-01");
    expect(dock.find(".term-host").exists()).toBe(false);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    // Resume goes back to the owning route; the store reuses the session.
    await dock.find(".term-dock-resume").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.fullPath).toContain(`server=${SERVER_A.id}`);
    expect(useTerminalStore().pageTabs[0]!.id).toBe(tabId);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    dock.unmount();
  });

  it("can move a paused session into the window or close it after confirmation", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    login(pinia);
    useServersStore().servers = [SERVER_A];
    const terminal = useTerminalStore();
    const tab = await terminal.openPageSession({
      serverId: SERVER_A.id,
      serverName: SERVER_A.name,
      sourcePath: `/terminal?server=${SERVER_A.id}`,
    });
    const router = pageRouter();
    await router.push("/dashboard");
    await router.isReady();
    const dock = mount(TerminalPageDock, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    });
    await flushPromises();
    expect(dock.find(".term-dock").exists()).toBe(true);

    // Close asks first; cancel keeps the shell and the card.
    await dock.find(".term-dock-close").trigger("click");
    await flushPromises();
    expect(terminal.closeRequest?.ids).toEqual([tab.id]);
    terminal.cancelClose();
    await flushPromises();
    expect(terminal.findTab(tab.id)).not.toBeNull();

    // Confirm removes it and issues exactly one DELETE.
    await dock.find(".term-dock-close").trigger("click");
    await flushPromises();
    terminal.confirmClose();
    await flushPromises();
    expect(terminal.findTab(tab.id)).toBeNull();
    expect(apiFakes.close).toHaveBeenCalledTimes(1);
    expect(dock.find(".term-dock").exists()).toBe(false);

    // A second page session can move into the window (one-way, same id).
    const second = await terminal.openPageSession({
      serverId: SERVER_B.id,
      serverName: SERVER_B.name,
      sourcePath: `/terminal?server=${SERVER_B.id}`,
    });
    await flushPromises();
    const transfer = dock
      .findAll(".term-dock-row")
      .find((row) => row.text().includes("prod-02"));
    expect(transfer).toBeDefined();
    await transfer!.findAll("button")[1]!.trigger("click");
    await flushPromises();
    expect(terminal.findTab(second.id)?.mode).toBe("window");
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    dock.unmount();
  });
});

describe("server detail Terminal tab (JUS-246)", () => {
  it("renders Files → Terminal and stays lazy until the tab activates", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    login(pinia);
    useServersStore().servers = [SERVER_A];
    const router = pageRouter();
    await router.push(`/servers/${SERVER_A.id}`);
    await router.isReady();
    const { default: ServerDetailPage } = await import(
      "../src/features/servers/pages/ServerDetailPage.vue"
    );
    const wrapper = mount(ServerDetailPage, {
      attachTo: document.body,
      global: {
        plugins: [pinia, router, i18n],
        stubs: {
          ServerDetailHeader: true,
          ServerOverviewTab: true,
          ServerMetricsTab: true,
          ServerProxyTab: true,
          ServerSettingsTab: true,
          EditServerModal: true,
        },
      },
    });
    await flushPromises();
    await nextTick();

    // The server read is mocked at the module boundary in this suite.
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    const tabLabels = wrapper.findAll(".n-tabs-tab").map((node) => node.text());
    expect(tabLabels).toContain("Files");
    expect(tabLabels).toContain("Terminal");
    expect(tabLabels.indexOf("Terminal")).toBe(tabLabels.indexOf("Files") + 1);

    await wrapper
      .findAll(".n-tabs-tab")
      .find((tab) => tab.text() === "Terminal")!
      .trigger("click");
    await flushPromises();
    await nextTick();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.create.mock.calls[0]?.[0]).toBe(SERVER_A.id);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    const tab = useTerminalStore().pageTabs[0]!;
    expect(tab.serverId).toBe(SERVER_A.id);
    expect(tab.sourcePath).toContain("tab=terminal");

    // Rapid tab changes and re-entry never open a concurrent duplicate.
    const overview = wrapper
      .findAll(".n-tabs-tab")
      .find((node) => node.text() === "Overview")!;
    for (let index = 0; index < 3; index += 1) {
      await overview.trigger("click");
      await flushPromises();
      await wrapper
        .findAll(".n-tabs-tab")
        .find((node) => node.text() === "Terminal")!
        .trigger("click");
      await flushPromises();
    }
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(useTerminalStore().pageTabs).toHaveLength(1);
    expect(useTerminalStore().pageTabs[0]!.sessionId).toBe("session-1");
    wrapper.unmount();
  });

  it("shows the not-ready state and creates no shell for an offline server", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    login(pinia);
    useServersStore().servers = [SERVER_OFFLINE];
    const router = pageRouter();
    await router.push(`/servers/${SERVER_OFFLINE.id}?tab=terminal`);
    await router.isReady();
    const { default: ServerDetailPage } = await import(
      "../src/features/servers/pages/ServerDetailPage.vue"
    );
    const wrapper = mount(ServerDetailPage, {
      attachTo: document.body,
      global: {
        plugins: [pinia, router, i18n],
        stubs: {
          ServerDetailHeader: true,
          ServerOverviewTab: true,
          ServerMetricsTab: true,
          ServerProxyTab: true,
          ServerSettingsTab: true,
          EditServerModal: true,
        },
      },
    });
    await flushPromises();
    await nextTick();
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(wrapper.text()).toContain("Terminal unavailable");
    expect(wrapper.find(".term-surface").exists()).toBe(false);
    wrapper.unmount();
  });

  it("hides the Files and Terminal tabs from a non-operator", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    login(pinia, false);
    useServersStore().servers = [SERVER_A];
    const router = pageRouter();
    await router.push(`/servers/${SERVER_A.id}`);
    await router.isReady();
    const { default: ServerDetailPage } = await import(
      "../src/features/servers/pages/ServerDetailPage.vue"
    );
    const wrapper = mount(ServerDetailPage, {
      attachTo: document.body,
      global: {
        plugins: [pinia, router, i18n],
        stubs: {
          ServerDetailHeader: true,
          ServerOverviewTab: true,
          ServerMetricsTab: true,
          ServerProxyTab: true,
          ServerSettingsTab: true,
          EditServerModal: true,
        },
      },
    });
    await flushPromises();
    await nextTick();
    const labels = wrapper.findAll(".n-tabs-tab").map((node) => node.text());
    expect(labels).not.toContain("Files");
    expect(labels).not.toContain("Terminal");
    wrapper.unmount();
  });
});
