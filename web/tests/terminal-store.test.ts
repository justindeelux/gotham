// JUS-245: tab/session ownership. The API and the runtime are mocked with
// faithful fakes that record creates, attaches and disposes, so the tests can
// prove a page→window transfer issues no new session and that closing releases
// exactly the confirmed tabs.
/* global document:readonly, HTMLElement:readonly, window:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

/** runtimeFakes holds the instances the store created. */
const runtimeFakes = vi.hoisted(() => {
  class FakeRuntime {
    options: {
      onState: (_patch: Record<string, unknown>) => void;
      session: { id: string };
    };
    host: HTMLElement;
    attached: HTMLElement | null = null;
    disposed = false;
    focused = false;
    fitted = 0;

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

    focus(): void {
      this.focused = true;
    }

    fit(): void {
      this.fitted += 1;
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

/** apiFakes holds the HTTP seam used by the store. */
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

const { useTerminalStore } = await import("@/features/terminal/stores/terminal");
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

/** options builds one open request. */
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
  apiFakes.create.mockReset();
  apiFakes.close.mockReset();
  apiFakes.close.mockResolvedValue(undefined);
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
    // Every tab owns its own runtime and socket wiring.
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
    // The entry point returning to the page gets the same detached tab, never
    // a new shell.
    const reopened = await terminal.openPageSession(options());
    expect(reopened.id).toBe(tab.id);
    expect(reopened.sessionId).toBe(tab.sessionId);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
  });

  it("queues a surface that mounted before the runtime existed", async () => {
    const terminal = useTerminalStore();
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
    first.emit({ status: "error", errorMessage: "stream failed" });
    expect(tab.status).toBe("error");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    // While not failed, retry is a no-op.
    tab.status = "connected";
    await terminal.retry(tab.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    first.emit({ status: "error", errorMessage: "stream failed" });
    await terminal.retry(tab.id);
    expect(first.disposed).toBe(true);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(tab.sessionId).toBe("session-2");
    expect(tab.status).toBe("connected");
    expect(apiFakes.close).toHaveBeenCalledWith("server-1", "session-1");
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

    await pane.find(".term-meta button").trigger("click");
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

    // Two tabs stay isolated: switching panes does not swap sessions.
    const second = await terminal.openWindowSession(options("other"));
    await flushPromises();
    expect(terminal.windowTabs).toHaveLength(2);
    expect(terminal.findTab(tab.id)?.sessionId).not.toBe(second.sessionId);
    expect(window.findAll(".term-window-body .term-surface")).toHaveLength(2);
    window.unmount();
  });
});
