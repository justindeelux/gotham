/* global AbortSignal:readonly, document:readonly, HTMLElement:readonly, localStorage:readonly, window:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

const runtimeFakes = vi.hoisted(() => {
  class FakeRuntime {
    options: {
      onState: (_patch: Record<string, unknown>) => void;
      session: { id: string };
      theme?: string;
      banner?: {
        version: string | null;
        serverName: string;
        serverId: string;
        target: string;
        serverLabel?: string;
      };
      bannerVersion?: Promise<string | null>;
    };
    host: HTMLElement;
    attached: HTMLElement | null = null;
    disposed = false;
    focused = false;
    fitted = 0;
    connected = false;
    theme: string | undefined;

    constructor(options: FakeRuntime["options"]) {
      this.options = options;
      this.theme = options.theme;
      this.host = document.createElement("div");
      this.host.className = "term-host";
      instances.push(this);
    }

    async connect(): Promise<void> {
      this.connected = true;
      this.options.onState({ status: "connected", shell: "/bin/bash" });
    }

    attach(container: HTMLElement): void {
      this.attached = container;
      container.appendChild(this.host);
    }

    focus(): void {
      this.focused = true;
    }

    fit(): void {
      this.fitted += 1;
    }

    setTheme(theme: string): void {
      this.theme = theme;
    }

    dispose(): void {
      this.disposed = true;
      this.host.remove();
    }

    emit(patch: Record<string, unknown>): void {
      this.options.onState(patch);
    }
  }
  const instances: FakeRuntime[] = [];
  return { FakeRuntime, instances };
});

const apiFakes = vi.hoisted(() => ({
  create: vi.fn(),
  close: vi.fn(),
  applicationCreate: vi.fn(),
  applicationClose: vi.fn(),
}));

const versionFakes = vi.hoisted(() => ({
  running: vi.fn(),
  known: vi.fn(),
}));

vi.mock("@/features/terminal/transport/terminalRuntime", () => ({
  TerminalRuntime: runtimeFakes.FakeRuntime,
}));

vi.mock("@/features/terminal/utils/terminalBanner", async (importOriginal) => {
  const actual =
    await importOriginal<
      typeof import("@/features/terminal/utils/terminalBanner")
    >();
  return {
    ...actual,
    runningGothamVersion: () => versionFakes.running(),
    knownGothamVersion: () => versionFakes.known(),
  };
});

vi.mock("@/features/terminal/api/terminal", () => ({
  createTerminalSession: apiFakes.create,
  closeTerminalSession: apiFakes.close,
  createApplicationTerminalSession: apiFakes.applicationCreate,
  closeApplicationTerminalSession: apiFakes.applicationClose,
  currentTerminalToken: () => "jwt-token",
  terminalFailure: () => ({ code: "generic", message: "start failed" }),
  applicationTerminalFailure: () => ({ code: "generic", message: "start failed" }),
  terminalWebSocketUrl: (path: string) => `ws://test${path}`,
}));

const { useTerminalStore, WINDOW_SINGLE_FLIGHT_HOLD_MS, PAGE_SINGLE_FLIGHT_HOLD_MS } =
  await import("@/features/terminal/stores/terminal");
const { useTerminalWindowStore } = await import(
  "@/features/terminal/stores/terminalWindow"
);
const { default: TerminalPane } = await import(
  "../src/features/terminal/components/TerminalPane.vue"
);
const { default: TerminalWindow } = await import(
  "../src/features/terminal/components/TerminalWindow.vue"
);

let sessionSeq = 0;

function options(cwd = "app", serverId = "server-1") {
  return { serverId, serverName: "prod-01", root: "volumes", cwd };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  document.body.innerHTML = "";
  window.localStorage.clear();
  setActivePinia(createPinia());
  runtimeFakes.instances.length = 0;
  sessionSeq = 0;
  versionFakes.running.mockReset();
  versionFakes.running.mockResolvedValue("v0.2.1-dev");
  versionFakes.known.mockReset();
  versionFakes.known.mockReturnValue("v0.2.1-dev");
  apiFakes.create.mockReset();
  apiFakes.close.mockReset();
  apiFakes.close.mockResolvedValue(undefined);
  apiFakes.applicationCreate.mockReset();
  apiFakes.applicationClose.mockReset();
  apiFakes.applicationClose.mockResolvedValue(undefined);
  apiFakes.applicationCreate.mockImplementation(
    async (
      applicationId: string,
      input: { service: string; containerId: string; cols: number; rows: number },
    ) => {
      sessionSeq += 1;
      return {
        id: `session-app-${sessionSeq}`,
        server_id: "server-1",
        target: "container",
        cols: input.cols,
        rows: input.rows,
        expires_at: "2026-10-20T12:01:00Z",
        websocket_path: `/api/v1/applications/${applicationId}/terminal/sessions/session-app-${sessionSeq}/ws`,
        subprotocol: "gotham.terminal.v1",
        application_id: applicationId,
        service: input.service,
        container_id: input.containerId,
        container_name: "web-1",
        target_label: "web · web-1",
      };
    },
  );
  apiFakes.create.mockImplementation(
    async (
      serverId: string,
      input: { target?: string; root?: string; cwd?: string; cols: number; rows: number },
    ) => {
      sessionSeq += 1;
      return {
        id: `session-${sessionSeq}`,
        server_id: serverId,
        target: input.target ?? "host",
        root: input.root ?? "",
        cwd: input.cwd ?? "",
        cols: input.cols,
        rows: input.rows,
        expires_at: "2026-10-20T12:01:00Z",
        websocket_path: `/api/v1/servers/${serverId}/terminal/sessions/session-${sessionSeq}/ws`,
        subprotocol: "gotham.terminal.v1",
      };
    },
  );
});

afterEach(() => {
  useTerminalStore().reset();
  vi.useRealTimers();
});

describe("terminal store sessions", () => {
  it("reuses one page session per target and creates separate window shells", async () => {
    const terminal = useTerminalStore();
    const page = await terminal.openPageSession(options());
    const again = await terminal.openPageSession(options());
    expect(again.id).toBe(page.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(page.mode).toBe("page");
    expect(page.sessionId).toBe("session-1");
    expect(page.status).toBe("connected");

    const firstWindow = await terminal.openWindowSession(options("one"));
    const secondWindow = await terminal.openWindowSession(options("two"));
    expect(firstWindow.id).not.toBe(secondWindow.id);
    expect(firstWindow.sessionId).not.toBe(secondWindow.sessionId);
    expect(terminal.windowOpenCount).toBe(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(3);
    expect(runtimeFakes.instances).toHaveLength(3);
    expect(new Set(runtimeFakes.instances.map((runtime) => runtime.options.session.id)).size).toBe(3);
  });

  it("deduplicates a rapid double page open before the first resolve", async () => {
    const terminal = useTerminalStore();
    const first = terminal.openPageSession(options());
    const second = terminal.openPageSession(options());
    const [a, b] = await Promise.all([first, second]);
    expect(a.id).toBe(b.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
  });

  it("restores and raises the floating window for a new window session", async () => {
    const terminal = useTerminalStore();
    const windowStore = useTerminalWindowStore();
    windowStore.setMinimized(true);
    windowStore.lower();

    const tab = await terminal.openWindowSession(options("data"));

    expect(tab.mode).toBe("window");
    expect(tab.cwd).toBe("data");
    expect(windowStore.minimized).toBe(false);
    expect(windowStore.raised).toBe(true);
  });

  it("shares one in-flight window create for the same target, then allows a new one", async () => {
    vi.useFakeTimers();
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    apiFakes.create.mockImplementationOnce(
      async (
        serverId: string,
        input: { target?: string; root?: string; cwd?: string; cols: number; rows: number },
      ) => {
        await gate;
        sessionSeq += 1;
        return {
          id: `session-${sessionSeq}`,
          server_id: serverId,
          target: input.target ?? "host",
          root: input.root ?? "",
          cwd: input.cwd ?? "",
          cols: input.cols,
          rows: input.rows,
          expires_at: "2026-10-20T12:01:00Z",
          websocket_path: `/api/v1/servers/${serverId}/terminal/sessions/session-${sessionSeq}/ws`,
          subprotocol: "gotham.terminal.v1",
        };
      },
    );

    const terminal = useTerminalStore();
    const first = terminal.openWindowSession(options("data"));
    const second = terminal.openWindowSession(options("data"));
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    release();
    const [a, b] = await Promise.all([first, second]);
    await vi.advanceTimersByTimeAsync(0);
    expect(b.id).toBe(a.id);
    expect(terminal.windowOpenCount).toBe(1);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(terminal.isWindowOpening(options("data"))).toBe(true);

    const held = await terminal.openWindowSession(options("data"));
    expect(held.id).toBe(a.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(WINDOW_SINGLE_FLIGHT_HOLD_MS);
    expect(terminal.isWindowOpening(options("data"))).toBe(false);
    const later = await terminal.openWindowSession(options("data"));
    expect(later.id).not.toBe(a.id);
    expect(terminal.windowOpenCount).toBe(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
  });

  it("holds a fast window create for the settle window, then releases it", async () => {
    vi.useFakeTimers();
    const terminal = useTerminalStore();
    const first = await terminal.openWindowSession(options("data"));
    await vi.advanceTimersByTimeAsync(0);
    expect(terminal.isWindowOpening(options("data"))).toBe(true);

    const held = await terminal.openWindowSession(options("data"));
    expect(held.id).toBe(first.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    expect(terminal.isWindowOpening(options("other"))).toBe(false);

    await vi.advanceTimersByTimeAsync(WINDOW_SINGLE_FLIGHT_HOLD_MS - 1);
    expect(terminal.isWindowOpening(options("data"))).toBe(true);
    await vi.advanceTimersByTimeAsync(1);
    expect(terminal.isWindowOpening(options("data"))).toBe(false);

    const later = await terminal.openWindowSession(options("data"));
    expect(later.id).not.toBe(first.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);

    terminal.reset();
    expect(vi.getTimerCount()).toBe(0);
    expect(terminal.isWindowOpening(options("data"))).toBe(false);
  });

  it("aborts an in-flight create when the window closes and releases the key", async () => {
    vi.useFakeTimers();
    let signal: AbortSignal | undefined;
    apiFakes.create.mockImplementationOnce(
      (_serverId: string, _input: unknown, nextSignal?: AbortSignal) =>
        new Promise((_resolve, reject) => {
          signal = nextSignal;
          nextSignal?.addEventListener("abort", () => {
            reject(Object.assign(new Error("aborted"), { name: "AbortError" }));
          });
        }),
    );

    const terminal = useTerminalStore();
    const opening = terminal.openWindowSession(options("data"));
    expect(terminal.isWindowOpening(options("data"))).toBe(true);
    expect(signal).toBeDefined();

    terminal.closeAll();

    expect(signal!.aborted).toBe(true);
    expect(terminal.isWindowOpening(options("data"))).toBe(false);
    await expect(opening).resolves.toBeDefined();
    expect(apiFakes.close).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("aborts an in-flight create on logout/account switch", async () => {
    vi.useFakeTimers();
    let signal: AbortSignal | undefined;
    apiFakes.create.mockImplementationOnce(
      (_serverId: string, _input: unknown, nextSignal?: AbortSignal) =>
        new Promise((_resolve, reject) => {
          signal = nextSignal;
          nextSignal?.addEventListener("abort", () => {
            reject(Object.assign(new Error("aborted"), { name: "AbortError" }));
          });
        }),
    );

    const terminal = useTerminalStore();
    const opening = terminal.openWindowSession(options("data"));
    expect(signal).toBeDefined();

    terminal.reset();

    expect(signal!.aborted).toBe(true);
    expect(terminal.isWindowOpening(options("data"))).toBe(false);
    await expect(opening).resolves.toBeDefined();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("opens different targets concurrently instead of sharing one create", async () => {
    const terminal = useTerminalStore();
    const one = terminal.openWindowSession(options("one"));
    const two = terminal.openWindowSession(options("two"));
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    const [a, b] = await Promise.all([one, two]);
    expect(a.id).not.toBe(b.id);
    expect(terminal.windowOpenCount).toBe(2);
  });

  it("transfers page → window with the same session id and no create", async () => {
    const terminal = useTerminalStore();
    const tab = await terminal.openPageSession(options());
    const runtime = runtimeFakes.instances[0]!;
    useTerminalWindowStore().setMinimized(true);

    terminal.detachToWindow(tab.id);

    expect(tab.mode).toBe("window");
    expect(tab.sessionId).toBe("session-1");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(runtime.disposed).toBe(false);
    expect(useTerminalWindowStore().minimized).toBe(false);
    expect(terminal.windowOpenCount).toBe(1);
    const reopened = await terminal.openPageSession(options());
    expect(reopened.id).toBe(tab.id);
    expect(reopened.sessionId).toBe(tab.sessionId);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
  });

  it("tracks the page source and pane ownership for the paused-session dock", async () => {
    const terminal = useTerminalStore();
    const tab = await terminal.openPageSession({
      ...options(),
      sourcePath: "/terminal?server=server-1",
    });
    expect(tab.sourcePath).toBe("/terminal?server=server-1");
    expect(terminal.sessionCount).toBe(1);
    expect(terminal.isDisplayed(tab.id)).toBe(false);

    const container = document.createElement("div");
    document.body.append(container);
    terminal.attach(tab.id, container);
    expect(terminal.isDisplayed(tab.id)).toBe(true);

    terminal.detach(tab.id, document.createElement("div"));
    expect(terminal.isDisplayed(tab.id)).toBe(true);
    terminal.detach(tab.id, container);
    expect(terminal.isDisplayed(tab.id)).toBe(false);

    await terminal.openPageSession({
      ...options(),
      sourcePath: "/servers/server-1?tab=terminal",
    });
    expect(tab.sourcePath).toBe("/servers/server-1?tab=terminal");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    const windowTab = await terminal.openWindowSession(options("two"));
    expect(terminal.sessionCount).toBe(2);
    expect(terminal.windowOpenCount).toBe(1);
    terminal.closeTabs([tab.id, windowTab.id]);
    expect(terminal.sessionCount).toBe(0);
    expect(terminal.isDisplayed(tab.id)).toBe(false);
  });

  it("starts a fresh page shell only when explicitly asked", async () => {
    const terminal = useTerminalStore();
    const first = await terminal.openPageSession(options());
    terminal.detachToWindow(first.id);
    expect(first.mode).toBe("window");

    const reopened = await terminal.openPageSession(options());
    expect(reopened.id).toBe(first.id);

    const fresh = await terminal.openPageSession({ ...options(), fresh: true });
    expect(fresh.id).not.toBe(first.id);
    expect(fresh.mode).toBe("page");
    expect(fresh.sessionId).toBe("session-2");
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(terminal.windowTabs).toHaveLength(1);
    expect(terminal.pageTabs).toHaveLength(1);

    const reuse = await terminal.openPageSession(options());
    expect(reuse.id).toBe(fresh.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
  });

  it("deduplicates a rapid double fresh open before the first resolve", async () => {
    const terminal = useTerminalStore();
    const first = terminal.openPageSession({ ...options(), fresh: true });
    const second = terminal.openPageSession({ ...options(), fresh: true });
    const [a, b] = await Promise.all([first, second]);
    expect(a.id).toBe(b.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
  });

  it("queues a surface that mounted before the runtime existed", async () => {    const terminal = useTerminalStore();
    const pending = terminal.openPageSession(options());
    const tab = terminal.findTabForTarget(options());
    expect(tab).not.toBeNull();
    const container = document.createElement("div");
    document.body.append(container);
    terminal.attach(tab!.id, container);
    await pending;

    expect(runtimeFakes.instances).toHaveLength(1);
    expect(runtimeFakes.instances[0]!.attached).toBe(container);
    expect(container.querySelector(".term-host")).not.toBeNull();
  });

  it("retries only on request, with a brand-new session and no replay", async () => {
    const terminal = useTerminalStore();
    const tab = await terminal.openPageSession(options());
    const first = runtimeFakes.instances[0]!;
    first.emit({ status: "error", errorCode: "generic" });
    expect(tab.status).toBe("error");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    tab.status = "connected";
    await terminal.retry(tab.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    first.emit({ status: "error", errorCode: "generic" });
    await terminal.retry(tab.id);
    expect(first.disposed).toBe(true);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(tab.sessionId).toBe("session-2");
    expect(tab.status).toBe("connected");
    expect(apiFakes.close).toHaveBeenCalledWith("server-1", "session-1");
  });
});

describe("terminal appearance and host banner", () => {
  it("passes banner metadata only for host sessions", async () => {
    const terminal = useTerminalStore();
    await terminal.openWindowSession(options("one"));
    expect(runtimeFakes.instances[0]!.options.banner).toEqual({
      version: "v0.2.1-dev",
      serverName: "prod-01",
      serverId: "server-1",
      target: "host",
      serverLabel: "server",
    });

    await terminal.openWindowSession({
      serverId: "server-1",
      serverName: "prod-01",
      target: "container:web-1",
      cwd: "app",
    });
    expect(runtimeFakes.instances[1]!.options.banner).toBeUndefined();
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
  });

  it("starts the shell without waiting for a hung /version lookup", async () => {
    versionFakes.running.mockReturnValue(new Promise(() => {}));
    versionFakes.known.mockReturnValue(null);
    const terminal = useTerminalStore();
    const tab = await terminal.openWindowSession(options("hung"));

    expect(tab.status).toBe("connected");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const runtime = runtimeFakes.instances[0]!;
    expect(runtime.connected).toBe(true);
    expect(runtime.options.banner).toMatchObject({
      version: null,
      serverLabel: "server",
    });
    expect(runtime.options.bannerVersion).toBeInstanceOf(Promise);
  });

  it("themes every live renderer without recreating sessions", async () => {
    localStorage.setItem(
      "gotham.auth.session",
      JSON.stringify({
        user: { id: "u-1" },
        accessToken: "a",
        refreshToken: "r",
      }),
    );
    const terminal = useTerminalStore();
    const first = await terminal.openWindowSession(options("one"));
    const second = await terminal.openWindowSession(options("two"));
    const runtimes = [runtimeFakes.instances[0]!, runtimeFakes.instances[1]!];
    expect(terminal.theme).toBe("gotham-dark");
    expect(runtimes.every((runtime) => runtime.theme === "gotham-dark")).toBe(
      true,
    );

    terminal.setTheme("gotham-light");

    expect(terminal.theme).toBe("gotham-light");
    expect(runtimes.every((runtime) => runtime.theme === "gotham-light")).toBe(
      true,
    );
    expect(runtimes.some((runtime) => runtime.disposed)).toBe(false);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(first.sessionId).toBe("session-1");
    expect(second.sessionId).toBe("session-2");
    expect(
      localStorage.getItem("gotham.terminal.appearance.v1.u-1"),
    ).toContain('"theme":"gotham-light"');

    await terminal.openWindowSession(options("three"));
    expect(runtimeFakes.instances[2]!.options.theme).toBe("gotham-light");
  });
});

describe("terminal store close semantics", () => {
  it("confirms before closing and keeps everything on cancel", async () => {
    const terminal = useTerminalStore();
    const first = await terminal.openWindowSession(options("one"));
    const second = await terminal.openWindowSession(options("two"));

    terminal.requestClose([first.id, second.id]);
    expect(terminal.closeRequest).toEqual({
      ids: [first.id, second.id],
      scope: "window",
    });
    terminal.cancelClose();
    expect(terminal.closeRequest).toBeNull();
    expect(terminal.tabs).toHaveLength(2);
    expect(runtimeFakes.instances.some((runtime) => runtime.disposed)).toBe(false);

    terminal.requestClose([first.id, second.id]);
    terminal.confirmClose();
    expect(terminal.tabs).toHaveLength(0);
    expect(terminal.closeRequest).toBeNull();
    expect(runtimeFakes.instances.every((runtime) => runtime.disposed)).toBe(true);
    expect(apiFakes.close).toHaveBeenCalledTimes(2);
  });

  it("closes only the confirmed tab and keeps the other session live", async () => {
    const terminal = useTerminalStore();
    const first = await terminal.openWindowSession(options("one"));
    const second = await terminal.openWindowSession(options("two"));
    const firstRuntime = runtimeFakes.instances[0]!;
    const secondRuntime = runtimeFakes.instances[1]!;

    terminal.requestClose([first.id]);
    expect(terminal.closeRequest?.scope).toBe("tab");
    terminal.confirmClose();

    expect(terminal.tabs.map((tab) => tab.id)).toEqual([second.id]);
    expect(firstRuntime.disposed).toBe(true);
    expect(secondRuntime.disposed).toBe(false);
    expect(apiFakes.close).toHaveBeenCalledWith("server-1", "session-1");
    expect(apiFakes.close).not.toHaveBeenCalledWith("server-1", "session-2");
  });

  it("awaits the shared confirmation for a session-terminating flow", async () => {
    const terminal = useTerminalStore();
    await terminal.openWindowSession(options("one"));
    await terminal.openWindowSession(options("two"));

    const pending = terminal.confirmCloseAll();
    expect(terminal.closeRequest?.scope).toBe("window");
    terminal.cancelClose();
    await expect(pending).resolves.toBe(false);
    expect(terminal.tabs).toHaveLength(2);

    const confirmed = terminal.confirmCloseAll();
    terminal.confirmClose();
    await expect(confirmed).resolves.toBe(true);
    expect(terminal.tabs).toHaveLength(0);
  });

  it("disposes every renderer on logout without issuing DELETE", async () => {
    const terminal = useTerminalStore();
    await terminal.openWindowSession(options("one"));
    await terminal.openWindowSession(options("two"));
    terminal.reset();
    expect(terminal.tabs).toHaveLength(0);
    expect(terminal.activeTabId).toBe("");
    expect(runtimeFakes.instances.every((runtime) => runtime.disposed)).toBe(true);
    expect(apiFakes.close).not.toHaveBeenCalled();
  });
});

describe("terminal pane transfer", () => {
  it("moves the same host and runtime from the page pane into the window", async () => {
    const terminal = useTerminalStore();
    const tab = await terminal.openPageSession(options());
    const runtime = runtimeFakes.instances[0]!;

    const pane = mount(TerminalPane, {
      props: { tabId: tab.id },
      attachTo: document.body,
      global: { plugins: [i18n] },
    });
    await flushPromises();
    expect(pane.find(".term-surface").exists()).toBe(true);
    expect(runtime.attached).not.toBeNull();
    expect(runtime.attached?.querySelector(".term-host")).not.toBeNull();
    const host = runtime.host;

    await pane.find(".term-open-in-window").trigger("click");
    await flushPromises();
    expect(tab.mode).toBe("window");
    expect(pane.find(".term-detached").exists()).toBe(true);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(runtime.disposed).toBe(false);
    pane.unmount();

    const window = mount(TerminalWindow, {
      attachTo: document.body,
      global: { plugins: [i18n] },
    });
    await flushPromises();
    expect(window.find(".term-window").exists()).toBe(true);
    const moved = window.find(".term-screen .term-host");
    expect(moved.exists()).toBe(true);
    expect(moved.element).toBe(host);
    expect(terminal.findTab(tab.id)?.sessionId).toBe("session-1");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    const second = await terminal.openWindowSession(options("other"));
    await flushPromises();
    expect(terminal.windowTabs).toHaveLength(2);
    expect(terminal.findTab(tab.id)?.sessionId).not.toBe(second.sessionId);
    expect(window.findAll(".term-window-body .term-surface")).toHaveLength(2);
    window.unmount();
  });
});

describe("page tabs (JUS-265)", () => {
  it("keeps the newest page session selected and switches without creating", async () => {
    const terminal = useTerminalStore();
    const first = await terminal.openPageSession(options("one"));
    expect(terminal.activePageTabId).toBe(first.id);
    const second = await terminal.openPageSession({
      ...options("two"),
      fresh: true,
    });
    expect(second.id).not.toBe(first.id);
    expect(terminal.pageTabs).toHaveLength(2);
    expect(terminal.activePageTabId).toBe(second.id);
    expect(terminal.pageActiveTab?.id).toBe(second.id);

    terminal.activatePage(first.id);
    expect(terminal.pageActiveTab?.id).toBe(first.id);
    await terminal.openWindowSession(options("window"));
    expect(terminal.pageActiveTab?.id).toBe(first.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(3);
  });

  it("falls back to the remaining page tab when the active one moves to the window", async () => {
    const terminal = useTerminalStore();
    const first = await terminal.openPageSession(options("one"));
    const second = await terminal.openPageSession({
      ...options("two"),
      fresh: true,
    });
    expect(terminal.activePageTabId).toBe(second.id);

    terminal.detachToWindow(second.id);
    expect(terminal.pageTabs.map((tab) => tab.id)).toEqual([first.id]);
    expect(terminal.activePageTabId).toBe(first.id);
    expect(terminal.pageActiveTab?.id).toBe(first.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
  });

  it("clears the page selection when the last page tab closes", async () => {
    const terminal = useTerminalStore();
    const only = await terminal.openPageSession(options());
    terminal.closeTabs([only.id]);
    expect(terminal.pageTabs).toHaveLength(0);
    expect(terminal.activePageTabId).toBe("");
    expect(terminal.pageActiveTab).toBeNull();
    expect(apiFakes.close).toHaveBeenCalledTimes(1);
  });
});

describe("page single-flight settle window (JUS-265 fix)", () => {
  it("joins a second fresh page open inside the hold and opens a new tab after it", async () => {
    vi.useFakeTimers();
    const terminal = useTerminalStore();
    const first = await terminal.openPageSession({
      ...options("one"),
      fresh: true,
    });
    const second = await terminal.openPageSession({
      ...options("one"),
      fresh: true,
    });
    expect(second.id).toBe(first.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(terminal.pageTabs).toHaveLength(1);

    vi.advanceTimersByTime(PAGE_SINGLE_FLIGHT_HOLD_MS + 1);
    const third = await terminal.openPageSession({
      ...options("one"),
      fresh: true,
    });
    expect(third.id).not.toBe(first.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(terminal.pageTabs).toHaveLength(2);
  });
});

describe("terminal banner capability (JUS-265 fix)", () => {
  it("gives each host session a banner and no container session one", async () => {
    const terminal = useTerminalStore();
    await terminal.openWindowSession(options("one"));
    await terminal.openWindowSession(options("two"));
    await terminal.openPageSession({
      serverId: "server-1",
      target: "container",
      applicationId: "app-1",
      applicationName: "shop",
      service: "web",
      containerId: "cid-1",
      containerName: "web-1",
      targetLabel: "web · web-1",
    });
    const [hostOne, hostTwo, container] = runtimeFakes.instances;
    expect(hostOne?.options.banner).toBeDefined();
    expect(hostTwo?.options.banner).toBeDefined();
    expect(container?.options.banner).toBeUndefined();
    expect(apiFakes.applicationCreate).toHaveBeenCalledTimes(1);
  });
});
