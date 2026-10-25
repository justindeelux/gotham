/* global document:readonly, HTMLElement:readonly, window:readonly */
import { NTabs } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
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
  applicationTerminalFailure: () => ({ code: "generic", message: "start failed" }),
  listTerminalTargets: async () => ({ servers: [], applications: [], errors: [] }),
  terminalWebSocketUrl: (path: string) => `ws://test${path}`,
}));

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
const { useTerminalWindowStore } = await import(
  "@/features/terminal/stores/terminalWindow"
);
const { useServersStore } = await import("@/features/servers/stores/servers");
const { useAuthStore } = await import("@/features/auth");
const { default: TerminalPage } = await import(
  "../src/features/terminal/pages/TerminalPage.vue"
);
const { default: TerminalPageDock } = await import(
  "../src/features/terminal/components/TerminalPageDock.vue"
);

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

const stub = { template: "<div />" };

function pageRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/terminal", name: "terminal", component: stub },
      { path: "/dashboard", name: "dashboard", component: stub },
      { path: "/login", name: "login", component: stub },
      { path: "/forbidden", name: "forbidden", component: stub },
      { path: "/servers", name: "servers", component: stub },
      {
        path: "/servers/:id",
        name: "server-detail",
        component: stub,
      },
      {
        path: "/servers/:id/containers",
        name: "server-containers",
        component: stub,
      },
      {
        path: "/servers/:id/files",
        name: "server-files",
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

function login(pinia: Pinia, isAdmin = true): void {
  setActivePinia(pinia);
  const auth = useAuthStore();
  auth.accessToken = "test-access";
  auth.refreshToken = "test-refresh";
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    display_name: "Ada",
    created_at: "2026-01-01T00:00:00Z",
    role: isAdmin ? "owner" : "member",
  };
}

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

async function mountDetail(path: string, isAdmin = true) {
  const pinia = createPinia();
  setActivePinia(pinia);
  login(pinia, isAdmin);
  useServersStore().servers = [SERVER_A, SERVER_B, SERVER_OFFLINE];
  const router = pageRouter();
  await router.push(path);
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
  return { wrapper, router, terminal: useTerminalStore() };
}

function clickButtonByText(wrapper: VueWrapper, label: string) {
  const button = wrapper
    .findAll("button")
    .find((node) => node.text().trim().includes(label));
  if (!button) {
    throw new Error(`button ${label} not found`);
  }
  return button.trigger("click");
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

    expect(wrapper.findAll(".term-page-tab")).toHaveLength(1);
    expect(wrapper.find(".term-add-tab").exists()).toBe(true);
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

  it("keeps several page tabs and closes only the confirmed one", async () => {
    const { wrapper, router, terminal } = await mountPage(
      `/terminal?server=${SERVER_A.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    const second = await terminal.openPageSession({
      serverId: SERVER_B.id,
      serverName: SERVER_B.name,
      sourcePath: `/terminal?server=${SERVER_B.id}`,
      fresh: true,
    });
    await flushPromises();
    expect(terminal.pageTabs).toHaveLength(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[0]).toBe(SERVER_B.id);
    expect(router.currentRoute.value.query.server).toBe(SERVER_B.id);
    expect(wrapper.findAll(".term-page-tab")).toHaveLength(2);
    expect(wrapper.find(".term-surface").exists()).toBe(true);

    terminal.requestClose([second.id]);
    await flushPromises();
    terminal.cancelClose();
    await flushPromises();
    expect(terminal.pageTabs).toHaveLength(2);
    expect(apiFakes.close).not.toHaveBeenCalled();

    terminal.requestClose([second.id]);
    await flushPromises();
    terminal.confirmClose();
    await flushPromises();
    expect(apiFakes.close).toHaveBeenCalledTimes(1);
    expect(apiFakes.close.mock.calls[0]?.[0]).toBe(SERVER_B.id);
    expect(terminal.pageTabs).toHaveLength(1);
    expect(terminal.pageTabs[0]!.serverId).toBe(SERVER_A.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    wrapper.unmount();
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

    await dock.find(".term-dock-close").trigger("click");
    await flushPromises();
    expect(terminal.closeRequest?.ids).toEqual([tab.id]);
    terminal.cancelClose();
    await flushPromises();
    expect(terminal.findTab(tab.id)).not.toBeNull();

    await dock.find(".term-dock-close").trigger("click");
    await flushPromises();
    terminal.confirmClose();
    await flushPromises();
    expect(terminal.findTab(tab.id)).toBeNull();
    expect(apiFakes.close).toHaveBeenCalledTimes(1);
    expect(dock.find(".term-dock").exists()).toBe(false);

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

describe("page session recovery (JUS-246 fix round 1)", () => {
  it("shows an explicit restart state on /terminal after its session closes", async () => {
    const { wrapper, terminal } = await mountPage(
      `/terminal?server=${SERVER_A.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const tabId = terminal.pageTabs[0]!.id;

    terminal.closeTabs([tabId]);
    await flushPromises();
    await nextTick();

    expect(wrapper.find(".term-surface").exists()).toBe(false);
    expect(wrapper.text()).toContain("Session closed");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    await clickButtonByText(wrapper, "Start new session");
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    expect(terminal.pageTabs).toHaveLength(1);
    wrapper.unmount();
  });

  it("offers an explicit new page session after a one-way move to the window", async () => {
    const { wrapper, terminal } = await mountPage(
      `/terminal?server=${SERVER_A.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    await wrapper.find(".term-open-in-window").trigger("click");
    await flushPromises();
    expect(wrapper.find(".term-detached").exists()).toBe(true);
    expect(wrapper.find(".term-detached-actions").exists()).toBe(false);
    expect(wrapper.find(".term-detached").text()).toContain("Minimize the window");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    const windowStore = useTerminalWindowStore();
    windowStore.setMinimized(true);
    await flushPromises();
    expect(wrapper.find(".term-detached-actions").exists()).toBe(true);

    await clickButtonByText(wrapper, "Show window");
    await flushPromises();
    expect(windowStore.minimized).toBe(false);
    expect(wrapper.find(".term-detached-actions").exists()).toBe(false);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    windowStore.setMinimized(true);
    await flushPromises();
    const detached = wrapper.find(".term-detached");
    const start = detached
      .findAll("button")
      .find((node) => node.text().includes("Start new session"));
    await start!.trigger("click");
    await flushPromises();

    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(terminal.pageTabs).toHaveLength(1);
    expect(terminal.windowTabs).toHaveLength(1);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    expect(wrapper.find(".term-detached").exists()).toBe(false);
    wrapper.unmount();
  });

  it("shows the restart state on the server detail tab after its session closes", async () => {
    const { wrapper, terminal } = await mountDetail(
      `/servers/${SERVER_A.id}?tab=terminal`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    const tabId = terminal.pageTabs[0]!.id;

    terminal.closeTabs([tabId]);
    await flushPromises();
    await nextTick();
    await nextTick();

    expect(wrapper.find(".term-surface").exists()).toBe(false);
    expect(wrapper.find(".term-stage-closed").exists()).toBe(true);
    expect(wrapper.text()).toContain("Session closed");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    expect(globalThis.document.activeElement?.textContent).toContain(
      "Start new session",
    );

    const tabButton = (label: string) =>
      wrapper.findAll(".n-tabs-tab").find((node) => node.text() === label)!;
    await tabButton("Overview").trigger("click");
    await flushPromises();
    await tabButton("Terminal").trigger("click");
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".term-stage-closed").exists()).toBe(true);

    await clickButtonByText(wrapper, "Start new session");
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    expect(wrapper.find(".n-tabs-tab--active").text()).toBe("Terminal");
    wrapper.unmount();
  });
});

describe("role demotion (JUS-246 fix round 1)", () => {
  it("leaves /terminal for /forbidden when the account loses operator access", async () => {
    const { wrapper, router } = await mountPage(
      `/terminal?server=${SERVER_A.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    useAuthStore().user = {
      ...useAuthStore().user!,
      role: "member",
    };
    await flushPromises();
    await nextTick();
    expect(router.currentRoute.value.name).toBe("forbidden");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("does not treat a sign-out as demotion (JUS-246 r2 finding 8)", async () => {
    const { wrapper, router } = await mountPage(
      `/terminal?server=${SERVER_A.id}`,
    );
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const auth = useAuthStore();

    auth.accessToken = null;
    auth.refreshToken = null;
    auth.user = null;
    await flushPromises();
    await nextTick();
    expect(router.currentRoute.value.name).not.toBe("forbidden");

    await router.push({ name: "login" });
    expect(router.currentRoute.value.fullPath).toBe("/login");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("resets the detail tab to Overview instead of leaving a blank body", async () => {
    const { wrapper } = await mountDetail(
      `/servers/${SERVER_A.id}?tab=terminal`,
    );
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    useAuthStore().user = {
      ...useAuthStore().user!,
      role: "member",
    };
    await flushPromises();
    await nextTick();

    expect(wrapper.find(".term-surface").exists()).toBe(false);
    expect(wrapper.findComponent(NTabs).props("value")).toBe("overview");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });
});

describe("non-operator terminal deep link (JUS-246 fix round 1)", () => {
  it("renders a clear not-available notice instead of silently falling back", async () => {
    const { wrapper } = await mountDetail(
      `/servers/${SERVER_A.id}?tab=terminal`,
      false,
    );
    expect(wrapper.text()).toContain("Terminal not available");
    expect(wrapper.text()).toContain("platform operators");
    const labels = wrapper.findAll(".n-tabs-tab").map((node) => node.text());
    expect(labels).not.toContain("Terminal");
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    wrapper.unmount();
  });
});

describe("paused dock accessibility and demotion (JUS-246 fix round 1)", () => {
  it("gives every card a unique accessible name, drops the redundant role and collapses", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    login(pinia);
    useServersStore().servers = [SERVER_A, SERVER_B];
    const terminal = useTerminalStore();
    await terminal.openPageSession({
      serverId: SERVER_A.id,
      serverName: SERVER_A.name,
      root: "volumes",
      cwd: "one",
      sourcePath: `/terminal?server=${SERVER_A.id}`,
    });
    await terminal.openPageSession({
      serverId: SERVER_B.id,
      serverName: SERVER_B.name,
      root: "volumes",
      cwd: "two",
      sourcePath: `/terminal?server=${SERVER_B.id}`,
    });
    const router = pageRouter();
    await router.push("/dashboard");
    await router.isReady();
    const wrapper = mount(TerminalPageDock, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    });
    await flushPromises();

    const resumeLabels = wrapper
      .findAll(".term-dock-resume")
      .map((button) => button.attributes("aria-label"));
    expect(resumeLabels).toHaveLength(2);
    expect(new Set(resumeLabels).size).toBe(2);
    expect(resumeLabels[0]).toContain("Resume");
    expect(resumeLabels[0]).toContain("prod-01 · one");
    expect(resumeLabels[1]).toContain("prod-02 · two");

    expect(wrapper.find(".term-dock").attributes("role")).toBeUndefined();

    const head = wrapper.find(".term-dock-head");
    expect(head.attributes("aria-expanded")).toBe("true");
    await head.trigger("click");
    expect(head.attributes("aria-expanded")).toBe("false");
    expect(wrapper.find(".term-dock-list").isVisible()).toBe(false);
    await head.trigger("click");
    expect(wrapper.find(".term-dock-list").isVisible()).toBe(true);

    useAuthStore().user = {
      ...useAuthStore().user!,
      role: "member",
    };
    await flushPromises();
    expect(wrapper.find(".term-dock").exists()).toBe(false);
    wrapper.unmount();
  });

  it("renders the standalone page in Vietnamese", async () => {
    syncComposerLocale("vi");
    const { wrapper } = await mountPage("/terminal");
    expect(wrapper.text()).toContain("Chọn máy chủ");
    expect(wrapper.text()).toContain("Mở shell trên máy chủ");
    wrapper.unmount();
  });
});
