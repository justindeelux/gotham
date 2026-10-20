// JUS-245: the terminal runtime owns one xterm renderer, one WebSocket and the
// heartbeat. These tests use a faithful fake socket (open/message/close/error
// are all controllable), so lifecycle bugs — an early "connected", a reconnect,
// input before open, a leaked timer — fail here instead of hiding behind a stub.
/* global document:readonly, HTMLElement:readonly, TextDecoder:readonly, TextEncoder:readonly, URL:readonly, window:readonly */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  CLIENT_CLOSE,
  CLIENT_HEARTBEAT,
  CLIENT_INPUT,
  CLIENT_RESIZE,
} from "@/features/terminal/transport/frames";
import {
  BANNER_VERSION_WAIT_MS,
  TERMINAL_HEARTBEAT_MS,
  TerminalRuntime,
} from "@/features/terminal/transport/terminalRuntime";
import type {
  TerminalModules,
  TerminalRuntimeOptions,
  TerminalSocket,
  TerminalStatePatch,
} from "@/features/terminal/transport/terminalRuntime";
import {
  TERMINAL_LOGO_DISPLAY,
  buildTerminalBanner,
} from "@/features/terminal/utils/terminalBanner";

/** serverFrame wraps an opcode + payload into one binary frame. */
function serverFrame(opcode: number, payload: Uint8Array = new Uint8Array()): ArrayBuffer {
  const bytes = new Uint8Array(payload.length + 1);
  bytes[0] = opcode;
  bytes.set(payload, 1);
  return bytes.buffer;
}

/** jsonFrame encodes a JSON control payload. */
function jsonFrame(opcode: number, value: unknown): ArrayBuffer {
  return serverFrame(opcode, new TextEncoder().encode(JSON.stringify(value)));
}

/** openFrame is the server's PTY confirmation with its create-time size. */
function openFrame(): ArrayBuffer {
  return jsonFrame(0x04, {
    shell: "/bin/bash",
    cols: 120,
    rows: 32,
    service_user: "gotham-agent",
  });
}

/** FakeSocket is a fully controllable WebSocket stand-in. */
class FakeSocket implements TerminalSocket {
  binaryType = "arraybuffer";
  readyState = 0;
  sent: Uint8Array[] = [];
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((_event: { data: unknown }) => void) | null = null;
  onclose: ((_event: { code: number; reason: string }) => void) | null = null;
  onerror: ((_event: unknown) => void) | null = null;

  open(): void {
    this.readyState = 1;
    this.onopen?.();
  }

  message(data: ArrayBuffer): void {
    this.onmessage?.({ data });
  }

  serverClose(code = 1006): void {
    this.readyState = 3;
    this.onclose?.({ code, reason: "" });
  }

  send(data: ArrayBuffer | Uint8Array): void {
    this.sent.push(data instanceof Uint8Array ? data : new Uint8Array(data));
  }

  close(): void {
    this.closed = true;
    this.readyState = 3;
    this.onclose?.({ code: 1000, reason: "" });
  }

  /** opcodes returns the first byte of every frame the client sent. */
  opcodes(): number[] {
    return this.sent.map((frame) => frame[0]!);
  }
}

/** FakeTerminal records writes and lets the test drive user input/resize. */
class FakeTerminal {
  static last: FakeTerminal | null = null;

  cols: number;
  rows: number;
  options: Record<string, unknown>;
  written: Uint8Array[] = [];
  disposed = false;
  focused = false;
  host: HTMLElement | null = null;
  private dataListeners: Array<(_data: string) => void> = [];
  private resizeListeners: Array<(_size: { cols: number; rows: number }) => void> = [];

  constructor(options: { cols: number; rows: number } & Record<string, unknown>) {
    this.cols = options.cols;
    this.rows = options.rows;
    this.options = options;
    FakeTerminal.last = this;
  }

  open(host: HTMLElement): void {
    this.host = host;
  }

  loadAddon(): void {
    // The FitAddon is exercised separately; the runtime only needs it loaded.
  }

  onData(listener: (_data: string) => void): { dispose: () => void } {
    this.dataListeners.push(listener);
    return { dispose: () => this.dataListeners.splice(this.dataListeners.indexOf(listener), 1) };
  }

  onResize(
    listener: (_size: { cols: number; rows: number }) => void,
  ): { dispose: () => void } {
    this.resizeListeners.push(listener);
    return {
      dispose: () => this.resizeListeners.splice(this.resizeListeners.indexOf(listener), 1),
    };
  }

  write(data: string | Uint8Array): void {
    this.written.push(
      typeof data === "string" ? new TextEncoder().encode(data) : data,
    );
  }

  focus(): void {
    this.focused = true;
  }

  dispose(): void {
    this.disposed = true;
  }

  type(data: string): void {
    for (const listener of this.dataListeners) {
      listener(data);
    }
  }

  resize(cols: number, rows: number): void {
    this.cols = cols;
    this.rows = rows;
    for (const listener of this.resizeListeners) {
      listener({ cols, rows });
    }
  }
}

/** FakeFit records fits and reports the fitted box through the terminal. */
class FakeFit {
  static fits = 0;
  static cols = 100;
  static rows = 30;

  fit(): void {
    FakeFit.fits += 1;
    // A real FitAddon calls terminal.resize(), which fires onResize with the
    // measured columns/rows; the fake does the same so ordering is exercised.
    FakeTerminal.last?.resize(FakeFit.cols, FakeFit.rows);
  }

  dispose(): void {
    // no-op
  }
}

/** setup builds one runtime with a captured socket and state log. */
function setup(extra: Partial<TerminalRuntimeOptions> = {}) {
  FakeTerminal.last = null;
  FakeFit.fits = 0;
  FakeFit.cols = 100;
  FakeFit.rows = 30;
  let socket: FakeSocket | null = null;
  let url = "";
  let protocols: string[] = [];
  const states: TerminalStatePatch[] = [];
  const runtime = new TerminalRuntime({
    session: {
      id: "session-1",
      websocket_path: "/api/v1/servers/s1/terminal/sessions/session-1/ws",
      cols: 120,
      rows: 32,
    },
    getToken: () => "jwt-token",
    onState: (patch) => states.push(patch),
    ...extra,
    deps: {
      createSocket: (nextUrl, nextProtocols) => {
        url = nextUrl;
        protocols = nextProtocols;
        socket = new FakeSocket();
        return socket;
      },
      loadTerminal: async () =>
        ({ Terminal: FakeTerminal, FitAddon: FakeFit }) as unknown as TerminalModules,
      ...extra.deps,
    },
  });
  return {
    runtime,
    states,
    get socket() {
      if (!socket) {
        throw new Error("socket not created");
      }
      return socket;
    },
    get url() {
      return url;
    },
    get protocols() {
      return protocols;
    },
    terminal() {
      if (!FakeTerminal.last) {
        throw new Error("terminal not created");
      }
      return FakeTerminal.last;
    },
  };
}

/** connectedRuntime returns a runtime whose server has confirmed `opened`. */
async function connectedRuntime() {
  const context = setup();
  const connecting = context.runtime.connect();
  await connecting;
  context.socket.open();
  context.socket.message(
    jsonFrame(0x04, {
      shell: "/bin/bash",
      cols: 120,
      rows: 32,
      service_user: "gotham-agent",
    }),
  );
  return context;
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  document.body.innerHTML = "";
});

describe("TerminalRuntime handshake", () => {
  it("offers both subprotocols and stays connecting until opened", async () => {
    const context = setup();
    await context.runtime.connect();
    const expected = new URL(
      "/api/v1/servers/s1/terminal/sessions/session-1/ws",
      window.location.href,
    );
    expected.protocol = expected.protocol === "https:" ? "wss:" : "ws:";
    expect(context.url).toBe(expected.toString());
    expect(context.protocols).toEqual(["gotham.terminal.v1", "gotham.token.jwt-token"]);
    expect(context.runtime.getStatus()).toBe("connecting");
    expect(context.states).toEqual([]);

    context.socket.message(
      jsonFrame(0x04, {
        shell: "/bin/bash",
        cols: 100,
        rows: 30,
        service_user: "gotham-agent",
      }),
    );
    expect(context.runtime.getStatus()).toBe("connected");
    expect(context.states.at(-1)).toEqual({
      status: "connected",
      shell: "/bin/bash",
      serviceUser: "gotham-agent",
    });
  });

  it("writes output verbatim and never sends input before connected", async () => {
    const context = setup();
    await context.runtime.connect();
    const terminal = context.terminal();
    terminal.type("pwd");
    expect(context.socket.opcodes()).not.toContain(CLIENT_INPUT);

    // The transport is open but the server has not confirmed the PTY yet:
    // input is still dropped rather than queued for replay.
    context.socket.open();
    terminal.type("ls");
    expect(context.socket.opcodes()).not.toContain(CLIENT_INPUT);

    context.socket.message(
      jsonFrame(0x04, {
        shell: "/bin/bash",
        cols: 120,
        rows: 32,
        service_user: "gotham-agent",
      }),
    );
    terminal.type("ls");
    expect(context.socket.opcodes()).toContain(CLIENT_INPUT);
    const input = context.socket.sent.find((frame) => frame[0] === CLIENT_INPUT);
    expect([...(input ?? new Uint8Array())].slice(1)).toEqual([0x6c, 0x73]);

    context.socket.message(serverFrame(0x01, new Uint8Array([0x68, 0x69])));
    expect([...terminal.written[0]!]).toEqual([0x68, 0x69]);
  });

  it("sends a resize frame when the renderer changes size after opening", async () => {
    const context = await connectedRuntime();
    const terminal = context.terminal();
    context.socket.sent = [];
    terminal.resize(90, 24);
    const resize = context.socket.sent.find((frame) => frame[0] === CLIENT_RESIZE);
    expect(resize).toBeDefined();
    expect([...(resize ?? new Uint8Array())]).toEqual([CLIENT_RESIZE, 0, 90, 0, 24]);
    expect(context.states.at(-1)).toEqual({ cols: 90, rows: 24 });
  });
});

describe("TerminalRuntime failure copy", () => {
  it("reports a missing token as a translatable auth error", async () => {
    const context = setup();
    const runtime = new TerminalRuntime({
      session: {
        id: "session-1",
        websocket_path: "/api/v1/servers/s1/terminal/sessions/session-1/ws",
        cols: 120,
        rows: 32,
      },
      getToken: () => null,
      onState: (patch) => context.states.push(patch),
      deps: {
        createSocket: () => {
          throw new Error("must not connect without a token");
        },
        loadTerminal: async () =>
          ({ Terminal: FakeTerminal, FitAddon: FakeFit }) as unknown as TerminalModules,
      },
    });
    await runtime.connect();
    expect(runtime.getStatus()).toBe("error");
    // No English runtime string reaches the UI: the catalog owns the copy.
    expect(context.states.at(-1)).toEqual({
      status: "error",
      errorCode: "auth",
    });
  });

  it("reports an unsupported frame as a generic translatable error", async () => {
    const context = await connectedRuntime();
    context.socket.message(serverFrame(0x09));
    expect(context.states.at(-1)).toEqual({
      status: "error",
      errorCode: "generic",
    });
  });
});

describe("TerminalRuntime initial size", () => {
  /** measure makes the detached xterm host look laid out to FitAddon. */
  function measure(runtime: TerminalRuntime): void {
    const host = runtime.getHost();
    if (!host) {
      throw new Error("host missing");
    }
    Object.defineProperty(host, "clientWidth", {
      configurable: true,
      value: 900,
    });
    Object.defineProperty(host, "clientHeight", {
      configurable: true,
      value: 500,
    });
  }

  it("queues a fit that happens BEFORE opened and flushes it once", async () => {
    const context = setup();
    await context.runtime.connect();
    context.socket.open();
    const terminal = context.terminal();
    // The surface attaches and ResizeObserver fits while the PTY is still
    // pending; the frame must wait for `opened` and then use the fitted box.
    measure(context.runtime);
    FakeFit.cols = 104;
    FakeFit.rows = 28;
    context.runtime.fit();
    expect(context.socket.opcodes()).not.toContain(CLIENT_RESIZE);
    expect(terminal.cols).toBe(104);

    context.socket.message(openFrame());
    const resizes = context.socket.sent.filter(
      (frame) => frame[0] === CLIENT_RESIZE,
    );
    expect(resizes).toHaveLength(1);
    expect([...resizes[0]!]).toEqual([CLIENT_RESIZE, 0, 104, 0, 28]);
    // The server's create-time size never overwrites the fitted one.
    const connected = context.states.find(
      (patch) => patch.status === "connected",
    );
    expect(connected).toMatchObject({
      status: "connected",
      shell: "/bin/bash",
    });
    expect(connected).not.toHaveProperty("cols");
    expect(context.states.at(-1)).toEqual({ cols: 104, rows: 28 });
  });

  it("sends the fitted size when the fit happens AFTER opened", async () => {
    const context = await connectedRuntime();
    measure(context.runtime);
    FakeFit.cols = 96;
    FakeFit.rows = 24;
    context.socket.sent = [];
    context.runtime.fit();
    const resizes = context.socket.sent.filter(
      (frame) => frame[0] === CLIENT_RESIZE,
    );
    expect(resizes).toHaveLength(1);
    expect([...resizes[0]!]).toEqual([CLIENT_RESIZE, 0, 96, 0, 24]);
    // A repeated identical fit does not spam the PTY.
    context.runtime.fit();
    expect(
      context.socket.sent.filter((frame) => frame[0] === CLIENT_RESIZE),
    ).toHaveLength(1);
  });
});

describe("TerminalRuntime lifecycle", () => {
  it("heartbeats every 25s after open and stops on dispose", async () => {
    const context = setup();
    await context.runtime.connect();
    context.socket.open();
    context.socket.sent = [];
    vi.advanceTimersByTime(TERMINAL_HEARTBEAT_MS);
    expect(context.socket.opcodes()).toEqual([CLIENT_HEARTBEAT]);
    vi.advanceTimersByTime(TERMINAL_HEARTBEAT_MS);
    expect(context.socket.opcodes()).toEqual([CLIENT_HEARTBEAT, CLIENT_HEARTBEAT]);

    context.runtime.dispose(true);
    expect(context.socket.opcodes().at(-1)).toBe(CLIENT_CLOSE);
    expect(context.socket.closed).toBe(true);
    context.socket.sent = [];
    vi.advanceTimersByTime(TERMINAL_HEARTBEAT_MS * 4);
    expect(context.socket.sent).toEqual([]);
  });

  it("reports a close before opened as disconnected and never reconnects", async () => {
    const context = setup();
    await context.runtime.connect();
    context.socket.serverClose(1006);
    expect(context.runtime.getStatus()).toBe("disconnected");
    expect(context.states.at(-1)).toMatchObject({ status: "disconnected" });
    vi.advanceTimersByTime(TERMINAL_HEARTBEAT_MS * 4);
    // One socket was created for the whole lifetime: retry is the only path.
    expect(context.states.filter((state) => state.status === "disconnected")).toHaveLength(1);
  });

  it("surfaces a server error frame and keeps only the bounded code", async () => {
    const context = await connectedRuntime();
    context.socket.message(serverFrame(0x03, new TextEncoder().encode("agent offline")));
    expect(context.runtime.getStatus()).toBe("error");
    // Unknown relay text is never retained: it can carry host paths.
    expect(context.states.at(-1)).toEqual({
      status: "error",
      errorCode: "generic",
    });
    context.socket.sent = [];
    vi.advanceTimersByTime(TERMINAL_HEARTBEAT_MS * 2);
    expect(context.socket.sent).toEqual([]);
  });

  it("classifies working-directory and agent error frames (JUS-248)", async () => {
    const cases: Array<[string, string]> = [
      ["invalid working directory", "cwdInvalid"],
      ["the working directory no longer exists", "cwdMissing"],
      ["the working directory is not readable", "cwdUnreadable"],
      [
        "the node agent does not support terminal sessions",
        "unsupported",
      ],
      ["the node is running the maximum number of terminal sessions", "quota"],
      ["the node agent is unavailable", "unavailable"],
    ];
    for (const [message, code] of cases) {
      const context = await connectedRuntime();
      context.socket.message(serverFrame(0x03, new TextEncoder().encode(message)));
      expect(context.runtime.getStatus()).toBe("error");
      expect(context.states.at(-1)).toEqual({
        status: "error",
        errorCode: code,
      });
      context.runtime.dispose(false);
    }
  });

  it("records the exit code when the shell ends", async () => {
    const context = await connectedRuntime();
    context.socket.message(jsonFrame(0x02, { exit_code: 130, signal: "" }));
    expect(context.runtime.getStatus()).toBe("exited");
    expect(context.states.at(-1)).toEqual({
      status: "exited",
      exitCode: 130,
      exitSignal: "",
    });
  });

  it("disposes the renderer and releases the socket exactly once", async () => {
    const context = await connectedRuntime();
    const terminal = context.terminal();
    context.runtime.dispose(true);
    expect(terminal.disposed).toBe(true);
    expect(context.runtime.getHost()).toBeNull();
    expect(context.socket.closed).toBe(true);
    // A second dispose is a no-op, not another close frame.
    context.socket.sent = [];
    context.runtime.dispose(true);
    expect(context.socket.sent).toEqual([]);
  });
});

describe("TerminalRuntime attach", () => {
  it("re-parents the same host element instead of creating a second one", async () => {
    const context = await connectedRuntime();
    const first = document.createElement("div");
    const second = document.createElement("div");
    document.body.append(first, second);
    context.runtime.attach(first);
    const host = context.runtime.getHost();
    expect(host).not.toBeNull();
    expect(host?.parentElement).toBe(first);

    context.runtime.attach(second);
    expect(context.runtime.getHost()).toBe(host);
    expect(host?.parentElement).toBe(second);
  });

  it("moves the host after the session was attached before it existed", async () => {
    const context = setup();
    const container = document.createElement("div");
    document.body.append(container);
    // Attach before connect: the surface mounted in the same tick as the tab.
    context.runtime.attach(container);
    await context.runtime.connect();
    expect(context.runtime.getHost()?.parentElement).toBe(container);
  });
});

/** measure makes the detached xterm host look laid out to FitAddon. */
function measureHost(runtime: TerminalRuntime, width = 2200, height = 900): void {
  const host = runtime.getHost();
  if (!host) {
    throw new Error("host missing");
  }
  Object.defineProperty(host, "clientWidth", {
    configurable: true,
    value: width,
  });
  Object.defineProperty(host, "clientHeight", {
    configurable: true,
    value: height,
  });
}

/** BANNER_INFO is one host session's display metadata. */
const BANNER_INFO = {
  version: "v0.2.1-dev",
  serverName: "prod-01",
  serverId: "server-1",
  target: "host",
};

/** decodeWrites turns xterm's recorded byte writes back into text. */
function decodeWrites(terminal: FakeTerminal): string[] {
  return terminal.written.map((bytes) => new TextDecoder().decode(bytes));
}

describe("TerminalRuntime theme", () => {
  it("repaints in place without a reconnect or input frame", async () => {
    document.documentElement.style.setProperty("--term-dark-bg", "#101010");
    document.documentElement.style.setProperty("--term-light-bg", "#fafafa");
    const context = await connectedRuntime();
    const terminal = context.terminal();
    expect(terminal.options.theme).toMatchObject({ background: "#101010" });

    context.socket.sent = [];
    context.runtime.setTheme("gotham-light");

    // xterm's option is the live palette: the same renderer, socket and
    // session keep running, and no keystroke was fabricated.
    expect(terminal.options.theme).toMatchObject({ background: "#fafafa" });
    expect(terminal.disposed).toBe(false);
    expect(context.socket.closed).toBe(false);
    expect(context.socket.opcodes()).not.toContain(CLIENT_INPUT);
    expect(context.runtime.getStatus()).toBe("connected");
    document.documentElement.style.removeProperty("--term-dark-bg");
    document.documentElement.style.removeProperty("--term-light-bg");
  });
});

describe("TerminalRuntime welcome banner", () => {
  it("writes the exact wide banner once and never through the PTY", async () => {
    const context = setup({ banner: BANNER_INFO });
    await context.runtime.connect();
    context.socket.open();
    measureHost(context.runtime);
    FakeFit.cols = 220;
    FakeFit.rows = 40;
    const opened = openFrame();
    context.socket.message(opened);
    // A duplicate confirmation still yields exactly one banner.
    context.socket.message(opened);
    const terminal = context.terminal();
    const banners = decodeWrites(terminal).filter((text) =>
      text.includes("Gotham"),
    );
    expect(banners).toHaveLength(1);
    expect(banners[0]).toBe(buildTerminalBanner(BANNER_INFO, 220));
    expect(banners[0]!.startsWith(TERMINAL_LOGO_DISPLAY)).toBe(true);
    expect(banners[0]).toContain("server: prod-01");
    expect(context.socket.opcodes()).not.toContain(CLIENT_INPUT);
    expect(context.socket.opcodes()).toContain(CLIENT_RESIZE);

    // Attach, fit, theme change and detach are not new sessions.
    const first = document.createElement("div");
    const second = document.createElement("div");
    document.body.append(first, second);
    context.runtime.attach(first);
    context.runtime.fit();
    context.runtime.setTheme("gotham-light");
    context.runtime.attach(second);
    expect(
      decodeWrites(terminal).filter((text) => text.includes("Gotham")),
    ).toHaveLength(1);
    expect(context.socket.opcodes()).not.toContain(CLIENT_INPUT);
  });

  it("uses the compact banner on a narrow renderer", async () => {
    const context = setup({ banner: BANNER_INFO });
    await context.runtime.connect();
    context.socket.open();
    measureHost(context.runtime);
    FakeFit.cols = 52;
    FakeFit.rows = 20;
    context.socket.message(openFrame());
    const banner = decodeWrites(context.terminal())[0]!;
    expect(banner).not.toContain("▓");
    expect(banner).toBe("Gotham v0.2.1-dev\r\nserver: prod-01\r\n\r\n");
    for (const line of banner.split("\r\n")) {
      expect(line.length).toBeLessThanOrEqual(52);
    }
  });

  it("never shows a host banner for a container target", async () => {
    const context = setup({
      banner: { ...BANNER_INFO, target: "container:web-1" },
    });
    await context.runtime.connect();
    context.socket.open();
    context.socket.message(openFrame());
    expect(context.terminal().written).toHaveLength(0);
    expect(context.runtime.getStatus()).toBe("connected");
  });

  it("never delays output or input for a hung version lookup", async () => {
    // A /version request that never settles: the session must still connect
    // and the first server bytes must flush a version-less banner first.
    const hung = new Promise<string | null>(() => {
      // Intentionally never resolves.
    });
    const context = setup({
      banner: { ...BANNER_INFO, version: null },
      bannerVersion: hung,
    });
    await context.runtime.connect();
    context.socket.open();
    context.socket.message(openFrame());
    expect(context.runtime.getStatus()).toBe("connected");
    expect(context.terminal().written).toHaveLength(0);

    context.socket.message(serverFrame(0x01, new TextEncoder().encode("hello")));
    const writes = decodeWrites(context.terminal());
    expect(writes).toHaveLength(2);
    expect(writes[0]).toBe(
      buildTerminalBanner({ ...BANNER_INFO, version: null }, 120, 32),
    );
    expect(writes[0]).not.toContain("v0.2.1-dev");
    expect(writes[1]).toBe("hello");
  });

  it("falls back to a version-less banner after the display-only cap", async () => {
    const hung = new Promise<string | null>(() => {
      // Intentionally never resolves.
    });
    const context = setup({
      banner: { ...BANNER_INFO, version: null },
      bannerVersion: hung,
    });
    await context.runtime.connect();
    context.socket.open();
    context.socket.message(openFrame());
    expect(context.terminal().written).toHaveLength(0);

    vi.advanceTimersByTime(BANNER_VERSION_WAIT_MS);
    const writes = decodeWrites(context.terminal());
    expect(writes).toHaveLength(1);
    expect(writes[0]).toContain("Gotham ·");
    expect(writes[0]).not.toContain("v0.2.1-dev");
  });

  it("uses a version that resolves before the first server bytes", async () => {
    let resolveVersion: (_value: string | null) => void = () => {};
    const pending = new Promise<string | null>((resolve) => {
      resolveVersion = resolve;
    });
    const context = setup({
      banner: { ...BANNER_INFO, version: null },
      bannerVersion: pending,
    });
    await context.runtime.connect();
    context.socket.open();
    context.socket.message(openFrame());
    expect(context.terminal().written).toHaveLength(0);

    resolveVersion("v0.9.0");
    await Promise.resolve();
    await Promise.resolve();
    expect(decodeWrites(context.terminal())[0]).toContain("Gotham v0.9.0");

    context.socket.message(serverFrame(0x01, new TextEncoder().encode("hello")));
    const writes = decodeWrites(context.terminal());
    expect(writes).toHaveLength(2);
    expect(writes[1]).toBe("hello");
  });

  it("flushes the banner before the first keystroke when metadata is slow", async () => {
    const hung = new Promise<string | null>(() => {
      // Intentionally never resolves.
    });
    const context = setup({
      banner: { ...BANNER_INFO, version: null },
      bannerVersion: hung,
    });
    await context.runtime.connect();
    context.socket.open();
    context.socket.message(openFrame());

    context.terminal().type("ls");
    const writes = decodeWrites(context.terminal());
    expect(writes).toHaveLength(1);
    expect(writes[0]).not.toContain("v0.2.1-dev");
    const input = context.socket.sent.find((frame) => frame[0] === CLIENT_INPUT);
    expect([...(input ?? new Uint8Array())].slice(1)).toEqual([0x6c, 0x73]);
  });
});
