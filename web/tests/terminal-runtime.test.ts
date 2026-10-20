// JUS-245: the terminal runtime owns one xterm renderer, one WebSocket and the
// heartbeat. These tests use a faithful fake socket (open/message/close/error
// are all controllable), so lifecycle bugs — an early "connected", a reconnect,
// input before open, a leaked timer — fail here instead of hiding behind a stub.
/* global document:readonly, HTMLElement:readonly, TextEncoder:readonly, URL:readonly, window:readonly */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  CLIENT_CLOSE,
  CLIENT_HEARTBEAT,
  CLIENT_INPUT,
  CLIENT_RESIZE,
} from "@/features/terminal/transport/frames";
import {
  TERMINAL_HEARTBEAT_MS,
  TerminalRuntime,
} from "@/features/terminal/transport/terminalRuntime";
import type {
  TerminalModules,
  TerminalSocket,
  TerminalStatePatch,
} from "@/features/terminal/transport/terminalRuntime";

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
  written: Uint8Array[] = [];
  disposed = false;
  focused = false;
  host: HTMLElement | null = null;
  private dataListeners: Array<(_data: string) => void> = [];
  private resizeListeners: Array<(_size: { cols: number; rows: number }) => void> = [];

  constructor(options: { cols: number; rows: number }) {
    this.cols = options.cols;
    this.rows = options.rows;
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

/** FakeFit records fits; the runtime only calls it for a measurable host. */
class FakeFit {
  static fits = 0;

  fit(): void {
    FakeFit.fits += 1;
  }

  dispose(): void {
    // no-op
  }
}

/** setup builds one runtime with a captured socket and state log. */
function setup() {
  FakeTerminal.last = null;
  FakeFit.fits = 0;
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
    deps: {
      createSocket: (nextUrl, nextProtocols) => {
        url = nextUrl;
        protocols = nextProtocols;
        socket = new FakeSocket();
        return socket;
      },
      loadTerminal: async () =>
        ({ Terminal: FakeTerminal, FitAddon: FakeFit }) as unknown as TerminalModules,
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
      cols: 100,
      rows: 30,
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

  it("surfaces a server error frame and stops the heartbeat", async () => {
    const context = await connectedRuntime();
    context.socket.message(serverFrame(0x03, new TextEncoder().encode("agent offline")));
    expect(context.runtime.getStatus()).toBe("error");
    expect(context.states.at(-1)).toEqual({
      status: "error",
      errorMessage: "agent offline",
    });
    context.socket.sent = [];
    vi.advanceTimersByTime(TERMINAL_HEARTBEAT_MS * 2);
    expect(context.socket.sent).toEqual([]);
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
