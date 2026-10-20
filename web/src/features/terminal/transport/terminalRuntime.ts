import {
  clampDimension,
  decodeServerFrame,
  encodeClose,
  encodeHeartbeat,
  encodeInput,
  encodeResize,
} from "@/features/terminal/transport/frames";
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import type { TerminalSessionInfo } from "@/features/terminal/schemas/terminal";
import {
  terminalStreamFailureCode,
  terminalWebSocketUrl,
} from "@/features/terminal/api/terminal";
import {
  DEFAULT_TERMINAL_THEME,
  type TerminalThemeName,
} from "@/features/terminal/schemas/terminalAppearance";
import {
  buildTerminalBanner,
  isHostTerminalTarget,
  type TerminalBannerInfo,
} from "@/features/terminal/utils/terminalBanner";
import {
  readDesignToken,
  terminalTheme,
} from "@/features/terminal/utils/terminalTheme";

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
  errorCode?: TerminalErrorCode | null;
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
  /** theme is the named renderer palette; it can change without a reconnect. */
  theme?: TerminalThemeName;
  /** banner is display-only metadata written once after `opened` (host only). */
  banner?: TerminalBannerInfo;
  /**
   * bannerVersion is the version lookup started in parallel with the session
   * create. The banner never waits on it for session start; it is used if it
   * resolves before the first output/input, otherwise the banner omits it.
   */
  bannerVersion?: Promise<string | null>;
  deps?: Partial<TerminalRuntimeDeps>;
}

/** BANNER_VERSION_WAIT_MS bounds how long the display banner waits for metadata. */
export const BANNER_VERSION_WAIT_MS = 750;

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

/** terminalFontFamily returns the mono stack, or undefined for xterm's own. */
function terminalFontFamily(): string | undefined {
  return readDesignToken("--font-mono");
}

/** terminalFontSize reads the compact text token; xterm needs a number. */
function terminalFontSize(): number {
  const raw = readDesignToken("--text-sm");
  const parsed = raw === undefined ? Number.NaN : Number.parseFloat(raw);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 14;
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
  /**
   * pendingSize is the fitted size measured before the server confirmed the
   * PTY. `opened` flushes it, so the PTY never stays at the create-time
   * default while the renderer is already fitted to a different box.
   */
  private pendingSize: { cols: number; rows: number } | null = null;
  /** lastResize deduplicates identical 0x02 frames. */
  private lastResize = "";
  /** themeName is the live palette; setTheme swaps it without a reconnect. */
  private themeName: TerminalThemeName;
  /** bannerWritten ensures one display banner per runtime/session. */
  private bannerWritten = false;
  /** bannerVersion is the version known when the banner is finally written. */
  private bannerVersion: string | null;
  /** bannerTimer writes a version-less banner if metadata is slow. */
  private bannerTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(options: TerminalRuntimeOptions) {
    this.options = options;
    this.themeName = options.theme ?? DEFAULT_TERMINAL_THEME;
    this.bannerVersion = options.banner?.version ?? null;
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
    } catch {
      this.fail("", "generic");
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

  /**
   * setTheme swaps the renderer palette in place. xterm repaints from
   * `options.theme`; the PTY, buffer and socket are untouched, so page/window
   * mode and focus do not matter.
   */
  setTheme(theme: TerminalThemeName): void {
    this.themeName = theme;
    if (this.terminal) {
      this.terminal.options.theme = terminalTheme(theme);
    }
  }

  /** sendInput sends raw keystrokes; nothing is queued while disconnected. */
  sendInput(data: string): void {
    // A user keystroke ahead of a slow version lookup flushes the banner first
    // (without the tag), so the banner never lands after the user's echo.
    if (this.status === "connected") {
      this.writeBanner();
    }
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
    this.clearBannerTimer();
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
      fontSize: terminalFontSize(),
      theme: terminalTheme(this.themeName),
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
      this.options.onState({ cols, rows });
      if (this.status === "connected") {
        this.sendResize(cols, rows);
      } else {
        // The renderer fits as soon as its box exists, usually before the
        // server's `opened`. Keep the latest measurement and flush it there.
        this.pendingSize = { cols, rows };
      }
    });

    if (typeof ResizeObserver !== "undefined") {
      this.observer = new ResizeObserver(() => this.fit());
      this.observer.observe(host);
    }
    // Layout is readable synchronously after append. A fit here happens
    // before `opened`; onResize stores it as pendingSize and `opened` flushes
    // the exact frame the PTY needs before the first prompt.
    this.fit();
  }

  /** openSocket connects with the v1 + token subprotocols. */
  private openSocket(): void {
    const token = this.options.getToken();
    if (!token) {
      this.fail("", "auth");
      return;
    }
    const url = terminalWebSocketUrl(this.options.session.websocket_path);
    let socket: TerminalSocket;
    try {
      socket = this.deps.createSocket(url, [
        "gotham.terminal.v1",
        `gotham.token.${token}`,
      ]);
    } catch {
      this.fail("", "generic");
      return;
    }
    socket.binaryType = "arraybuffer";
    this.socket = socket;

    socket.onopen = () => {
      if (this.disposed) {
        return;
      }
      this.startHeartbeat();
      // The fitted size is flushed on `opened`, once the PTY exists.
    };
    socket.onmessage = (event) => {
      this.handleMessage(event.data);
    };
    socket.onclose = () => {
      this.stopHeartbeat();
      if (this.disposed || this.ended) {
        return;
      }
      // The catalog owns the disconnected copy; no English reaches the UI.
      this.setStatus("disconnected");
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
        // Flush the welcome first when metadata was still pending, so shell
        // output never appears above the banner.
        this.writeBanner();
        this.terminal?.write(frame.bytes);
        return;
      case "opened": {
        this.ended = false;
        this.status = "connected";
        // The renderer's fitted size is the truth shown in the footer and sent
        // to the PTY; the server's create defaults are not surfaced.
        this.options.onState({
          status: "connected",
          shell: frame.info.shell,
          serviceUser: frame.info.service_user,
        });
        const size =
          this.pendingSize ??
          {
            cols: this.terminal?.cols ?? this.options.session.cols,
            rows: this.terminal?.rows ?? this.options.session.rows,
          };
        this.pendingSize = null;
        this.sendResize(size.cols, size.rows);
        this.fit();
        // Display-only welcome: written to xterm, never to the socket. A slow
        // /version never delays this confirmation or the PTY stream.
        this.scheduleBanner();
        return;
      }
      case "exited":
        this.ended = true;
        this.stopHeartbeat();
        this.clearBannerTimer();
        this.setStatus("exited", {
          exitCode: frame.info.exit_code,
          exitSignal: frame.info.signal,
        });
        return;
      case "error":
        this.ended = true;
        this.stopHeartbeat();
        this.clearBannerTimer();
        // The server's message is already a bounded, localized-agnostic
        // diagnostic; a known working-directory/agent phrase becomes a
        // translatable class, and anything else stays verbatim (JUS-248).
        {
          const code = terminalStreamFailureCode(frame.message);
          this.setStatus(
            "error",
            code === null
              ? { errorMessage: frame.message }
              : { errorCode: code, errorMessage: frame.message },
          );
        }
        return;
      case "ping":
        return;
      case "invalid":
        this.ended = true;
        this.stopHeartbeat();
        this.clearBannerTimer();
        this.setStatus("error", { errorCode: "generic", errorMessage: "" });
        return;
    }
  }

  /**
   * scheduleBanner writes the welcome without delaying the session. A warm
   * version renders immediately; a pending lookup gets a short display-only
   * wait, and the first output/input flushes the banner early (without a tag)
   * so ordering stays banner → shell output.
   */
  private scheduleBanner(): void {
    const banner = this.options.banner;
    if (
      this.bannerWritten ||
      !banner ||
      !isHostTerminalTarget(banner.target)
    ) {
      return;
    }
    if (this.bannerVersion !== null || !this.options.bannerVersion) {
      this.writeBanner();
      return;
    }
    void this.options.bannerVersion
      .then((version) => {
        if (version !== null) {
          this.bannerVersion = version;
        }
        this.writeBanner();
      })
      .catch(() => this.writeBanner());
    this.bannerTimer ??= setTimeout(() => {
      this.bannerTimer = null;
      this.writeBanner();
    }, BANNER_VERSION_WAIT_MS);
  }

  /**
   * writeBanner prints the Gotham welcome once, on the `opened` frame of this
   * runtime (or at the first output/input when metadata was slow). A tab
   * switch, fit, minimize/restore, theme change or page→window detach
   * re-parents the same renderer and never re-runs this. Non-host targets
   * (containers) are filtered before anything reaches the screen.
   */
  private writeBanner(): void {
    const banner = this.options.banner;
    if (
      this.bannerWritten ||
      !banner ||
      !isHostTerminalTarget(banner.target)
    ) {
      return;
    }
    this.bannerWritten = true;
    this.clearBannerTimer();
    const columns = this.terminal?.cols ?? this.options.session.cols;
    const rows = this.terminal?.rows ?? this.options.session.rows;
    this.terminal?.write(
      buildTerminalBanner(
        { ...banner, version: this.bannerVersion },
        columns,
        rows,
      ),
    );
  }

  /** clearBannerTimer cancels the display-only metadata wait. */
  private clearBannerTimer(): void {
    if (this.bannerTimer !== null) {
      clearTimeout(this.bannerTimer);
      this.bannerTimer = null;
    }
  }

  /**
   * sendResize sends one 0x02 frame, deduplicated against the last size. It is
   * only called once the PTY is confirmed, so the server never keeps the
   * create-time default while xterm is fitted to another box.
   */
  private sendResize(cols: number, rows: number): void {
    const key = `${clampDimension(cols)}x${clampDimension(rows)}`;
    if (key === this.lastResize) {
      return;
    }
    this.lastResize = key;
    this.send(encodeResize(cols, rows));
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
  private fail(message: string, code: TerminalErrorCode): void {
    if (this.disposed || this.ended) {
      return;
    }
    this.ended = true;
    this.stopHeartbeat();
    this.setStatus("error", { errorCode: code, errorMessage: message });
  }
}
