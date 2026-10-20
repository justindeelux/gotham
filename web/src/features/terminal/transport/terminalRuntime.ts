import type { ITheme } from "@xterm/xterm";

import {
  decodeServerFrame,
  encodeClose,
  encodeHeartbeat,
  encodeInput,
  encodeResize,
} from "@/features/terminal/transport/frames";
import type { TerminalSessionInfo } from "@/features/terminal/schemas/terminal";
import { terminalWebSocketUrl } from "@/features/terminal/api/terminal";

/**
 * One live terminal session (JUS-245): the xterm renderer, the WebSocket
 * transport, the heartbeat and the fitted size, owned OUTSIDE Vue so route or
 * pane mounts can come and go without touching the PTY.
 *
 * xterm is loaded lazily (its own chunk) and the runtime is created only when
 * a tab actually opens. Input is sent as it is typed and is never queued for
 * a reconnect: a broken transport ends in `disconnected` and only an explicit
 * user retry creates a new session.
 */

/** TerminalStatus is the user-visible lifecycle of one tab. */
export type TerminalStatus =
  | "connecting"
  | "connected"
  | "exited"
  | "disconnected"
  | "error";

/** TerminalStatePatch is emitted whenever the lifecycle changes. */
export interface TerminalStatePatch {
  status?: TerminalStatus;
  exitCode?: number;
  exitSignal?: string;
  errorMessage?: string;
  shell?: string;
  serviceUser?: string;
  cols?: number;
  rows?: number;
}

/** SOCKET_OPEN mirrors the WebSocket.OPEN readyState. */
const SOCKET_OPEN = 1;

/** TERMINAL_HEARTBEAT_MS matches the server's own 25s ping cadence. */
export const TERMINAL_HEARTBEAT_MS = 25_000;

/** TerminalSocket is the minimal WebSocket surface the runtime needs. */
export interface TerminalSocket {
  binaryType: string;
  readyState: number;
  send(_data: ArrayBuffer | Uint8Array): void;
  close(_code?: number, _reason?: string): void;
  onopen: (() => void) | null;
  onmessage: ((_event: { data: unknown }) => void) | null;
  onclose: ((_event: { code: number; reason: string }) => void) | null;
  onerror: ((_event: unknown) => void) | null;
}

/** TerminalModules is the lazily imported xterm pair. */
export interface TerminalModules {
  Terminal: typeof import("@xterm/xterm").Terminal;
  FitAddon: typeof import("@xterm/addon-fit").FitAddon;
}

/** TerminalRuntimeDeps are the injectable edges (real browser by default). */
export interface TerminalRuntimeDeps {
  createSocket: (_url: string, _protocols: string[]) => TerminalSocket;
  loadTerminal: () => Promise<TerminalModules>;
}

/** TerminalRuntimeOptions is everything the runtime needs to start. */
export interface TerminalRuntimeOptions {
  session: Pick<TerminalSessionInfo, "id" | "websocket_path" | "cols" | "rows">;
  getToken: () => string | null;
  onState: (_patch: TerminalStatePatch) => void;
  deps?: Partial<TerminalRuntimeDeps>;
}

/** defaultCreateSocket wraps the browser WebSocket behind the minimal shape. */
function defaultCreateSocket(url: string, protocols: string[]): TerminalSocket {
  const socket = new WebSocket(url, protocols);
  socket.binaryType = "arraybuffer";
  return socket as unknown as TerminalSocket;
}

/**
 * defaultLoadTerminal imports xterm and the fit addon on demand. The CSS is
 * imported in the same dynamic chunk, so the base app bundle stays unchanged
 * until a terminal opens.
 */
async function defaultLoadTerminal(): Promise<TerminalModules> {
  const [xterm, fit] = await Promise.all([
    import("@xterm/xterm"),
    import("@xterm/addon-fit"),
    import("@xterm/xterm/css/xterm.css"),
  ]);
  return { Terminal: xterm.Terminal, FitAddon: fit.FitAddon };
}

/** readToken returns one resolved design token, or undefined when absent. */
function readToken(name: string): string | undefined {
  if (typeof document === "undefined") {
    return undefined;
  }
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  return value === "" ? undefined : value;
}

/** terminalTheme maps the design tokens onto xterm's theme (no literals). */
function terminalTheme(): ITheme {
  const background = readToken("--surface-warm");
  const foreground = readToken("--fg");
  const cursor = readToken("--accent");
  const selectionBackground = readToken("--border-soft");
  return {
    ...(background ? { background } : {}),
    ...(foreground ? { foreground } : {}),
    ...(cursor ? { cursor } : {}),
    ...(selectionBackground ? { selectionBackground } : {}),
  };
}

/** terminalFontFamily returns the mono stack, or undefined for xterm's own. */
function terminalFontFamily(): string | undefined {
  return readToken("--font-mono");
}

export class TerminalRuntime {
  private readonly options: TerminalRuntimeOptions;
  private readonly deps: TerminalRuntimeDeps;

  private terminal: import("@xterm/xterm").Terminal | null = null;
  private fitAddon: import("@xterm/addon-fit").FitAddon | null = null;
  private socket: TerminalSocket | null = null;
  private host: HTMLDivElement | null = null;
  private container: HTMLElement | null = null;
  private observer: ResizeObserver | null = null;
  private heartbeat: ReturnType<typeof setInterval> | null = null;
  private inputSubscription: { dispose: () => void } | null = null;
  private resizeSubscription: { dispose: () => void } | null = null;

  private status: TerminalStatus = "connecting";
  private disposed = false;
  private ended = false;

  constructor(options: TerminalRuntimeOptions) {
    this.options = options;
    this.deps = {
      createSocket: options.deps?.createSocket ?? defaultCreateSocket,
      loadTerminal: options.deps?.loadTerminal ?? defaultLoadTerminal,
    };
  }

  /** connect loads the renderer, opens the socket and waits for `opened`. */
  async connect(): Promise<void> {
    if (this.disposed) {
      return;
    }
    try {
      const modules = await this.deps.loadTerminal();
      if (this.disposed) {
        return;
      }
      this.mount(modules);
      this.openSocket();
    } catch (error) {
      this.fail(
        error instanceof Error ? error.message : "The terminal could not start.",
      );
    }
  }

  /**
   * attach moves the live xterm host into a surface element. The same host
   * element is re-parented on a page→window transfer, so the buffer and the
   * PTY stream continue without a reconnect.
   */
  attach(container: HTMLElement): void {
    if (this.disposed) {
      return;
    }
    this.container = container;
    if (this.host) {
      container.appendChild(this.host);
      this.fit();
    }
  }

  /** focus focuses the live renderer, when it exists. */
  focus(): void {
    this.terminal?.focus();
  }

  /** getHost exposes the renderer host for tests and diagnostics. */
  getHost(): HTMLDivElement | null {
    return this.host;
  }

  /** getStatus exposes the local lifecycle copy. */
  getStatus(): TerminalStatus {
    return this.status;
  }

  /** sendInput sends raw keystrokes; nothing is queued while disconnected. */
  sendInput(data: string): void {
    this.send(encodeInput(data));
  }

  /** fit re-fits the renderer to its current host box and reports columns. */
  fit(): void {
    if (!this.fitAddon || !this.host || this.disposed) {
      return;
    }
    // A hidden (minimized) host measures 0x0; fitting then would collapse the
    // renderer to one column. Fitting happens again on restore.
    if (this.host.clientWidth <= 0 || this.host.clientHeight <= 0) {
      return;
    }
    try {
      this.fitAddon.fit();
    } catch {
      // xterm throws when the host detaches mid-measure; the next attach fits.
    }
  }

  /**
   * dispose releases the socket, timers, observer and renderer. `sendClose`
   * asks the server to cancel the session first; a transport that is already
   * gone is simply closed.
   */
  dispose(sendClose = true): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    this.stopHeartbeat();
    this.inputSubscription?.dispose();
    this.resizeSubscription?.dispose();
    this.observer?.disconnect();
    this.observer = null;
    if (this.socket) {
      const socket = this.socket;
      this.socket = null;
      socket.onopen = null;
      socket.onmessage = null;
      socket.onclose = null;
      socket.onerror = null;
      if (sendClose && socket.readyState === SOCKET_OPEN) {
        try {
          socket.send(encodeClose());
        } catch {
          // The close frame is best-effort; socket.close() still ends it.
        }
      }
      try {
        socket.close(1000);
      } catch {
        // Already closed.
      }
    }
    this.terminal?.dispose();
    this.terminal = null;
    this.fitAddon = null;
    this.host?.remove();
    this.host = null;
    this.container = null;
  }

  /** mount creates the renderer inside its detached host and wires events. */
  private mount(modules: TerminalModules): void {
    const host = document.createElement("div");
    host.className = "term-host";
    host.dataset.testid = "terminal-host";
    this.host = host;

    const terminal = new modules.Terminal({
      cols: this.options.session.cols,
      rows: this.options.session.rows,
      cursorBlink: true,
      scrollback: 5000,
      // Remote output is untrusted: keep proposed APIs (and any future
      // extension surface) off, and never interpret PTY bytes as markup.
      allowProposedApi: false,
      // xterm's accessible DOM (live region + aria labels) is on by default
      // for keyboard and screen-reader operation.
      screenReaderMode: true,
      fontFamily: terminalFontFamily(),
      fontSize: 13,
      theme: terminalTheme(),
    });
    const fitAddon = new modules.FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(host);
    this.terminal = terminal;
    this.fitAddon = fitAddon;

    if (this.container) {
      this.container.appendChild(host);
    }

    this.inputSubscription = terminal.onData((data) => {
      // Never replay or queue: input typed after an end state is dropped.
      if (this.status === "connected") {
        this.sendInput(data);
      }
    });
    this.resizeSubscription = terminal.onResize(({ cols, rows }) => {
      if (this.status === "connected") {
        this.send(encodeResize(cols, rows));
      }
      this.options.onState({ cols, rows });
    });

    if (typeof ResizeObserver !== "undefined") {
      this.observer = new ResizeObserver(() => this.fit());
      this.observer.observe(host);
    }
    // Layout is readable synchronously after append; this also seeds the
    // resize frame the server needs before the first output.
    this.fit();
  }

  /** openSocket connects with the v1 + token subprotocols. */
  private openSocket(): void {
    const token = this.options.getToken();
    if (!token) {
      this.fail("Your session has ended. Sign in again to open a terminal.");
      return;
    }
    const url = terminalWebSocketUrl(this.options.session.websocket_path);
    let socket: TerminalSocket;
    try {
      socket = this.deps.createSocket(url, [
        "gotham.terminal.v1",
        `gotham.token.${token}`,
      ]);
    } catch (error) {
      this.fail(
        error instanceof Error
          ? error.message
          : "The terminal connection could not be opened.",
      );
      return;
    }
    socket.binaryType = "arraybuffer";
    this.socket = socket;

    socket.onopen = () => {
      if (this.disposed) {
        return;
      }
      this.startHeartbeat();
      // The server may have sized the PTY already; the fitted size is sent
      // once the renderer knows its box.
      const terminal = this.terminal;
      if (terminal && this.status === "connected") {
        this.send(encodeResize(terminal.cols, terminal.rows));
      }
    };
    socket.onmessage = (event) => {
      this.handleMessage(event.data);
    };
    socket.onclose = () => {
      this.stopHeartbeat();
      if (this.disposed || this.ended) {
        return;
      }
      this.setStatus("disconnected", {
        errorMessage: "The terminal connection closed.",
      });
    };
    socket.onerror = () => {
      // Browsers follow an error with close; the close handler owns the state
      // transition so one failure is reported once.
    };
  }

  /** handleMessage decodes one server frame and applies it. */
  private handleMessage(data: unknown): void {
    if (this.disposed) {
      return;
    }
    const bytes =
      data instanceof ArrayBuffer
        ? new Uint8Array(data)
        : data instanceof Uint8Array
          ? data
          : null;
    if (!bytes) {
      return;
    }
    const frame = decodeServerFrame(bytes);
    if (!frame) {
      return;
    }
    switch (frame.kind) {
      case "output":
        this.terminal?.write(frame.bytes);
        return;
      case "opened":
        this.ended = false;
        this.options.onState({
          status: "connected",
          shell: frame.info.shell,
          serviceUser: frame.info.service_user,
          cols: frame.info.cols,
          rows: frame.info.rows,
        });
        this.status = "connected";
        this.fit();
        return;
      case "exited":
        this.ended = true;
        this.stopHeartbeat();
        this.setStatus("exited", {
          exitCode: frame.info.exit_code,
          exitSignal: frame.info.signal,
        });
        return;
      case "error":
        this.ended = true;
        this.stopHeartbeat();
        this.setStatus("error", { errorMessage: frame.message });
        return;
      case "ping":
        return;
      case "invalid":
        this.ended = true;
        this.stopHeartbeat();
        this.setStatus("error", {
          errorMessage: "The terminal sent an unsupported frame.",
        });
        return;
    }
  }

  /** send writes one client frame when the socket is open. */
  private send(frame: Uint8Array): void {
    const socket = this.socket;
    if (!socket || socket.readyState !== SOCKET_OPEN) {
      return;
    }
    try {
      socket.send(frame);
    } catch {
      // A send racing a close is not a new failure; the close handler reports.
    }
  }

  /** startHeartbeat sends 0x04 every 25s while connected. */
  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeat = setInterval(() => {
      if (this.status !== "exited" && this.status !== "error") {
        this.send(encodeHeartbeat());
      }
    }, TERMINAL_HEARTBEAT_MS);
  }

  /** stopHeartbeat cancels the liveness interval. */
  private stopHeartbeat(): void {
    if (this.heartbeat !== null) {
      clearInterval(this.heartbeat);
      this.heartbeat = null;
    }
  }

  /** setStatus records a lifecycle change and emits it once. */
  private setStatus(
    status: TerminalStatus,
    patch: Omit<TerminalStatePatch, "status"> = {},
  ): void {
    if (this.status === status) {
      return;
    }
    this.status = status;
    this.options.onState({ status, ...patch });
  }

  /** fail ends a session that never reached `opened`. */
  private fail(message: string): void {
    if (this.disposed || this.ended) {
      return;
    }
    this.ended = true;
    this.stopHeartbeat();
    this.setStatus("error", { errorMessage: message });
  }
}
