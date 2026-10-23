import {
  clampDimension,
  decodeServerFrame,
  encodeClose,
  encodeHeartbeat,
  encodeInput,
  encodeResize,
} from "@/features/terminal/transport/frames";
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";
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
import {
  PromptScanner,
  UNKNOWN_PROMPT_STATE,
} from "@/features/terminal/transport/promptScanner";
import type { PromptState } from "@/features/terminal/transport/promptScanner";
import { QuickCommandEngine } from "@/features/terminal/utils/quickCommandEngine";
import type {
  QuickCommandSuggestionState,
} from "@/features/terminal/utils/quickCommandEngine";
import {
  prepareAcceptance,
  prepareInsertion,
} from "@/features/terminal/utils/quickCommandInsertion";
import type { InsertionFailure } from "@/features/terminal/utils/quickCommandInsertion";

export type TerminalStatus =
  | "connecting"
  | "connected"
  | "exited"
  | "disconnected"
  | "error";

export interface TerminalStatePatch {
  status?: TerminalStatus;
  exitCode?: number;
  exitSignal?: string;
  errorCode?: TerminalErrorCode | null;
  shell?: string;
  serviceUser?: string;
  cols?: number;
  rows?: number;
}

const SOCKET_OPEN = 1;

export const TERMINAL_HEARTBEAT_MS = 25_000;

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

export interface TerminalModules {
  Terminal: typeof import("@xterm/xterm").Terminal;
  FitAddon: typeof import("@xterm/addon-fit").FitAddon;
}

export interface TerminalRuntimeDeps {
  createSocket: (_url: string, _protocols: string[]) => TerminalSocket;
  loadTerminal: () => Promise<TerminalModules>;
}

export interface TerminalRuntimeOptions {
  session: Pick<TerminalSessionInfo, "id" | "websocket_path" | "cols" | "rows">;
  getToken: () => string | null;
  onState: (_patch: TerminalStatePatch) => void;
  theme?: TerminalThemeName;
  banner?: TerminalBannerInfo;
  bannerVersion?: Promise<string | null>;
  quickCommands?: () => QuickCommand[];
  onSuggestion?: (_state: QuickCommandSuggestionState) => void;
  deps?: Partial<TerminalRuntimeDeps>;
}

export const BANNER_VERSION_WAIT_MS = 750;

export type TerminalCommandFailure = InsertionFailure | "unavailable" | "no-suggestion";

export interface TerminalCommandResult {
  ok: boolean;
  failure?: TerminalCommandFailure;
}

function defaultCreateSocket(url: string, protocols: string[]): TerminalSocket {
  const socket = new WebSocket(url, protocols);
  socket.binaryType = "arraybuffer";
  return socket as unknown as TerminalSocket;
}

async function defaultLoadTerminal(): Promise<TerminalModules> {
  const [xterm, fit] = await Promise.all([
    import("@xterm/xterm"),
    import("@xterm/addon-fit"),
    import("@xterm/xterm/css/xterm.css"),
  ]);
  return { Terminal: xterm.Terminal, FitAddon: fit.FitAddon };
}

function terminalFontFamily(): string | undefined {
  return readDesignToken("--font-mono");
}

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
  private readonly scanner: PromptScanner;
  private readonly engine: QuickCommandEngine;
  private promptState: PromptState = { ...UNKNOWN_PROMPT_STATE };

  private status: TerminalStatus = "connecting";
  private disposed = false;
  private ended = false;
  private upgraded = false;
  private pendingSize: { cols: number; rows: number } | null = null;
  private lastResize = "";
  private themeName: TerminalThemeName;
  private bannerWritten = false;
  private bannerVersion: string | null;
  private bannerTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(options: TerminalRuntimeOptions) {
    this.options = options;
    this.themeName = options.theme ?? DEFAULT_TERMINAL_THEME;
    this.bannerVersion = options.banner?.version ?? null;
    this.deps = {
      createSocket: options.deps?.createSocket ?? defaultCreateSocket,
      loadTerminal: options.deps?.loadTerminal ?? defaultLoadTerminal,
    };
    this.engine = new QuickCommandEngine({
      getCommands: options.quickCommands ?? (() => []),
      onChange: (state) => options.onSuggestion?.(state),
    });
    this.scanner = new PromptScanner((state) => {
      this.promptState = state;
      this.engine.setPrompt(state);
    });
  }

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
      this.fail("generic");
    }
  }

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

  focus(): void {
    this.terminal?.focus();
  }

  getHost(): HTMLDivElement | null {
    return this.host;
  }

  getCursorRow(): { top: number; height: number } | null {
    const terminal = this.terminal;
    const host = this.host;
    if (!terminal || !host) {
      return null;
    }
    const rows = host.querySelector<HTMLElement>(".xterm-rows");
    const cell = rows?.firstElementChild;
    if (!rows || !cell) {
      return null;
    }
    const height = cell.getBoundingClientRect().height;
    if (height <= 0) {
      return null;
    }
    const cursorY = terminal.buffer.active.cursorY;
    return { top: rows.getBoundingClientRect().top + cursorY * height, height };
  }

  watchCursor(listener: () => void): () => void {
    const subscriptions = [
      this.terminal?.onCursorMove(listener),
      this.terminal?.onScroll(listener),
    ];
    return () => {
      for (const subscription of subscriptions) {
        subscription?.dispose();
      }
    };
  }

  getStatus(): TerminalStatus {
    return this.status;
  }

  setTheme(theme: TerminalThemeName): void {
    this.themeName = theme;
    if (this.terminal) {
      this.terminal.options.theme = terminalTheme(theme);
    }
  }

  getPromptState(): PromptState {
    return { ...this.promptState };
  }

  getSuggestionState(): QuickCommandSuggestionState {
    return this.engine.getState();
  }

  dismissSuggestion(): void {
    this.engine.dismiss();
  }

  setComposing(composing: boolean): void {
    this.engine.setComposing(composing);
  }

  acceptSuggestion(commandId?: string): TerminalCommandResult {
    if (this.status !== "connected") {
      return { ok: false, failure: "unavailable" };
    }
    const acceptance = this.engine.accept(commandId);
    if (!acceptance) {
      return { ok: false, failure: "no-suggestion" };
    }
    const plan = prepareAcceptance(
      acceptance.erase,
      acceptance.text,
      this.bracketedPasteEnabled(),
    );
    if (!plan.ok) {
      return { ok: false, failure: plan.failure ?? "control" };
    }
    this.sendInput(plan.sequence);
    this.engine.clearTracking();
    return { ok: true };
  }

  insertCommand(text: string): TerminalCommandResult {
    if (this.status !== "connected") {
      return { ok: false, failure: "unavailable" };
    }
    const plan = prepareInsertion(text, this.bracketedPasteEnabled());
    if (!plan.ok) {
      return { ok: false, failure: plan.failure ?? "control" };
    }
    this.sendInput(plan.sequence);
    this.engine.clearTracking();
    return { ok: true };
  }

  private bracketedPasteEnabled(): boolean {
    return this.terminal?.modes.bracketedPasteMode === true;
  }

  sendInput(data: string): void {
    if (this.status === "connected") {
      this.writeBanner();
    }
    this.send(encodeInput(data));
  }

  fit(): void {
    if (!this.fitAddon || !this.host || this.disposed) {
      return;
    }
    if (this.host.clientWidth <= 0 || this.host.clientHeight <= 0) {
      return;
    }
    try {
      this.fitAddon.fit();
    } catch {
    }
  }

  dispose(sendClose = true): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    this.stopHeartbeat();
    this.clearBannerTimer();
    this.scanner.reset();
    this.engine.reset();
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
        }
      }
      try {
        socket.close(1000);
      } catch {
      }
    }
    this.terminal?.dispose();
    this.terminal = null;
    this.fitAddon = null;
    this.host?.remove();
    this.host = null;
    this.container = null;
  }

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
      allowProposedApi: false,
      macOptionIsMeta: true,
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
      if (this.status === "connected") {
        const forwarded = this.engine.handleInput(data);
        if (forwarded !== null) {
          this.sendInput(forwarded);
        }
      }
    });
    this.resizeSubscription = terminal.onResize(({ cols, rows }) => {
      this.options.onState({ cols, rows });
      if (this.status === "connected") {
        this.sendResize(cols, rows);
      } else {
        this.pendingSize = { cols, rows };
      }
    });

    if (typeof ResizeObserver !== "undefined") {
      this.observer = new ResizeObserver(() => this.fit());
      this.observer.observe(host);
    }
    this.fit();
  }

  private openSocket(): void {
    const token = this.options.getToken();
    if (!token) {
      this.fail("auth");
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
      this.fail("generic");
      return;
    }
    socket.binaryType = "arraybuffer";
    this.socket = socket;

    socket.onopen = () => {
      if (this.disposed) {
        return;
      }
      this.upgraded = true;
      this.startHeartbeat();
    };
    socket.onmessage = (event) => {
      this.handleMessage(event.data);
    };
    socket.onclose = () => {
      this.stopHeartbeat();
      if (this.disposed || this.ended) {
        return;
      }
      if (this.status !== "connected") {
        this.fail(this.upgraded ? "sessionExpired" : "originRefused");
        return;
      }
      this.setStatus("disconnected");
    };
    socket.onerror = () => {
    };
  }

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
        this.writeBanner();
        this.scanner.push(frame.bytes);
        this.terminal?.write(frame.bytes);
        return;
      case "opened": {
        this.ended = false;
        this.status = "connected";
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
        this.setStatus("error", {
          errorCode: terminalStreamFailureCode(frame.message) ?? "generic",
        });
        return;
      case "ping":
        return;
      case "invalid":
        this.ended = true;
        this.stopHeartbeat();
        this.clearBannerTimer();
        this.setStatus("error", { errorCode: "generic" });
        return;
    }
  }

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

  private clearBannerTimer(): void {
    if (this.bannerTimer !== null) {
      clearTimeout(this.bannerTimer);
      this.bannerTimer = null;
    }
  }

  private sendResize(cols: number, rows: number): void {
    const key = `${clampDimension(cols)}x${clampDimension(rows)}`;
    if (key === this.lastResize) {
      return;
    }
    this.lastResize = key;
    this.send(encodeResize(cols, rows));
  }

  private send(frame: Uint8Array): void {
    const socket = this.socket;
    if (!socket || socket.readyState !== SOCKET_OPEN) {
      return;
    }
    try {
      socket.send(frame);
    } catch {
    }
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeat = setInterval(() => {
      if (this.status !== "exited" && this.status !== "error") {
        this.send(encodeHeartbeat());
      }
    }, TERMINAL_HEARTBEAT_MS);
  }

  private stopHeartbeat(): void {
    if (this.heartbeat !== null) {
      clearInterval(this.heartbeat);
      this.heartbeat = null;
    }
  }

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

  private fail(code: TerminalErrorCode): void {
    if (this.disposed || this.ended) {
      return;
    }
    this.ended = true;
    this.stopHeartbeat();
    this.setStatus("error", { errorCode: code });
  }
}
