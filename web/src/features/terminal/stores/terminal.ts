import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  closeTerminalSession,
  createTerminalSession,
  currentTerminalToken,
  terminalFailure,
} from "@/features/terminal/api/terminal";
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import type { TerminalThemeName } from "@/features/terminal/schemas/terminalAppearance";
import { useTerminalAppearanceStore } from "@/features/terminal/stores/terminalAppearance";
import { useTerminalWindowStore } from "@/features/terminal/stores/terminalWindow";
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import { TerminalRuntime } from "@/features/terminal/transport/terminalRuntime";
import type {
  TerminalCommandResult,
  TerminalStatePatch,
  TerminalStatus,
} from "@/features/terminal/transport/terminalRuntime";
import {
  isHostTerminalTarget,
  knownGothamVersion,
  runningGothamVersion,
} from "@/features/terminal/utils/terminalBanner";
import { i18n } from "@/shared/i18n";
import type { PromptPhase } from "@/features/terminal/transport/promptScanner";
import type {
  QuickCommandSuggestion,
  QuickCommandSuggestionState,
} from "@/features/terminal/utils/quickCommandEngine";

/**
 * Terminal tabs and live sessions (JUS-245).
 *
 * The reactive tabs are the window's data; the renderer/transport lives in a
 * non-reactive map so a pane or route unmount never tears down a shell. The
 * public methods below are the entry points later work packages call:
 *
 *   - openPageSession({ serverId, root, cwd })  page mode, idempotent per target
 *   - openWindowSession({ serverId, root, cwd }) floating window, one new tab
 *                                               per deliberate call; a same-target
 *                                               create already in flight is shared
 *   - detachToWindow(tabId)                     one-way page → window transfer
 *   - requestClose([tabId])                     same confirmation path as the UI
 *   - isDisplayed(tabId)                        a page pane owns the live host
 *
 * A transfer reuses the SAME session id, renderer and socket: it only moves
 * the host element. There is deliberately no window → page action.
 *
 * A page session outlives its route: the runtime keeps the renderer when the
 * pane unmounts (`displayed` drops), and the app-level page dock shows the
 * paused session so it stays reachable and closable from every route.
 */

/** TerminalMode is where one live session is shown. */
export type TerminalMode = "page" | "window";

/** TerminalTab is the reactive metadata of one live terminal. */
export interface TerminalTab {
  id: string;
  serverId: string;
  serverName: string;
  target: string;
  root: string;
  cwd: string;
  title: string;
  mode: TerminalMode;
  sessionId: string;
  status: TerminalStatus;
  cols: number;
  rows: number;
  shell: string;
  serviceUser: string;
  exitCode: number | null;
  exitSignal: string;
  errorCode: TerminalErrorCode | null;
  createdAt: number;
  /**
   * sourcePath is the route that owns this page session, used by the app
   * dock to resume a paused page tab after a route change. It is display
   * metadata only and never part of the idempotency key.
   */
  sourcePath: string;
  /** promptPhase is the shell's OSC 133/633 state (JUS-249). */
  promptPhase: PromptPhase;
  /** integrationDetected is true once this shell emitted a prompt mark. */
  integrationDetected: boolean;
  /** alternateScreen is true while a fullscreen TUI owns the screen. */
  alternateScreen: boolean;
  /** suggestion is the current abbreviation match, if any. */
  suggestion: QuickCommandSuggestion | null;
}

/** TerminalOpenOptions is the public entry-point input. */
export interface TerminalOpenOptions {
  serverId: string;
  serverName?: string;
  target?: string;
  root?: string;
  cwd?: string;
  cols?: number;
  rows?: number;
  title?: string;
  /** sourcePath is the fullPath of the page/tab that opened the session. */
  sourcePath?: string;
  /**
   * fresh asks for a new page tab even when a detached tab for the same
   * target exists. Hosts use it only for an explicit "start a new session"
   * action (for example after the page session moved into the window);
   * ordinary opens still reuse the live shell.
   */
  fresh?: boolean;
}

/** TerminalCloseRequest is the pending confirmation (always one at a time). */
export interface TerminalCloseRequest {
  ids: string[];
  scope: "tab" | "window";
}

/** DEFAULT_TERMINAL_COLS/ROWS seed the create call before the renderer fits. */
const DEFAULT_TERMINAL_COLS = 120;
const DEFAULT_TERMINAL_ROWS = 32;

/**
 * WINDOW_SINGLE_FLIGHT_HOLD_MS keeps a window target's single-flight key (and
 * its busy state) after a create settles, so a human-speed double-click whose
 * second click lands after a fast create still joins the first gesture
 * (JUS-248 fix round 2). A deliberate activation after this window opens a new
 * independent tab, as documented.
 */
export const WINDOW_SINGLE_FLIGHT_HOLD_MS = 600;

/** tabId returns a stable local id without requiring a secure context. */
function tabId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `term-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

/** targetKey identifies a page session request for idempotent opens. */
function targetKey(options: TerminalOpenOptions): string {
  return [
    options.serverId,
    options.target ?? "host",
    options.root ?? "",
    options.cwd ?? "",
  ].join("\u0000");
}

/** terminalTitle names one tab in the window strip. */
function terminalTitle(options: TerminalOpenOptions): string {
  if (options.title) {
    return options.title;
  }
  const name = options.serverName || options.serverId;
  const where = options.cwd && options.cwd !== "." ? options.cwd : options.root;
  return where ? `${name} · ${where}` : name;
}

export const useTerminalStore = defineStore("terminal", () => {
  const tabs = ref<TerminalTab[]>([]);
  const activeTabId = ref<string>("");
  const closeRequest = ref<TerminalCloseRequest | null>(null);
  const appearance = useTerminalAppearanceStore();

  /** runtimes holds one live renderer/transport per tab (non-reactive). */
  const runtimes = new Map<string, TerminalRuntime>();
  /**
   * surfaces remembers the element each tab is displayed in. It queues a
   * surface that mounted before the socket, and lets an explicit retry put the
   * new renderer into the still-mounted pane.
   */
  const surfaces = new Map<string, HTMLElement>();
  /**
   * displayed is the reactive companion of `surfaces`: the page dock shows a
   * paused card for every page tab without a live pane, while window tabs are
   * surfaced by the floating window and the rail.
   */
  const displayed = ref<Set<string>>(new Set());
  /** inflight deduplicates rapid page opens for the same target. */
  const inflight = new Map<string, Promise<TerminalTab>>();
  /**
   * windowInflight keeps one window create per target. The entry survives for
   * WINDOW_SINGLE_FLIGHT_HOLD_MS after the create settles so an accidental
   * second click from the same gesture cannot start another shell even when
   * the create was fast. A deliberate later activation opens a new tab.
   */
  const windowInflight = new Map<string, Promise<TerminalTab>>();
  /** windowHoldTimers owns the settle windows so teardown can clear them. */
  const windowHoldTimers = new Map<string, ReturnType<typeof setTimeout>>();
  /** windowBusyKeys is the reactive busy state the toolbar reads per target. */
  const windowBusyKeys = ref<Set<string>>(new Set());
  /** controllers aborts an in-flight create when its tab/window closes. */
  const controllers = new Map<string, AbortController>();
  /** createTargets maps each live tab to its single-flight key and mode. */
  const createTargets = new Map<string, { key: string; mode: TerminalMode }>();
  /** retrying guards a second explicit retry while the first is running. */
  const retrying = new Set<string>();
  /** closeResolve settles an awaited close confirmation (logout path). */
  let closeResolve: ((_confirmed: boolean) => void) | null = null;
  /** closePromise is shared by overlapping awaited close requests. */
  let closePromise: Promise<boolean> | null = null;

  const activeTab = computed<TerminalTab | null>(
    () => tabs.value.find((tab) => tab.id === activeTabId.value) ?? null,
  );
  const pageTabs = computed<TerminalTab[]>(() =>
    tabs.value.filter((tab) => tab.mode === "page"),
  );
  const windowTabs = computed<TerminalTab[]>(() =>
    tabs.value.filter((tab) => tab.mode === "window"),
  );
  const windowOpenCount = computed<number>(() => windowTabs.value.length);
  /**
   * sessionCount is the total number of live tabs: page sessions count too,
   * so the rail badge never hides a page-only shell from the operator.
   */
  const sessionCount = computed<number>(() => tabs.value.length);
  /**
   * hasLiveSessions is the beforeunload guard: a browser unload terminates an
   * attached PTY just like a tab close, so it must warn the same way.
   */
  const hasLiveSessions = computed<boolean>(() =>
    tabs.value.some(
      (tab) => tab.status === "connecting" || tab.status === "connected",
    ),
  );

  /** findTab resolves one tab by local id. */
  function findTab(id: string): TerminalTab | null {
    return tabs.value.find((tab) => tab.id === id) ?? null;
  }

  /**
   * findTabForTarget returns an existing session for one page target. A live
   * page tab wins over a detached copy of the same target: after an explicit
   * "start a new page session", later opens must reuse the page tab instead of
   * jumping back to the old window tab.
   */
  function findTabForTarget(options: TerminalOpenOptions): TerminalTab | null {
    const key = targetKey(options);
    const matches = tabs.value.filter((tab) => targetKey(tab) === key);
    return (
      matches.find((tab) => tab.mode === "page") ?? matches[0] ?? null
    );
  }

  /** releaseWindowKey drops one target's hold, timer and busy flag at once. */
  function releaseWindowKey(key: string): void {
    const timer = windowHoldTimers.get(key);
    if (timer !== undefined) {
      clearTimeout(timer);
      windowHoldTimers.delete(key);
    }
    windowInflight.delete(key);
    windowBusyKeys.value.delete(key);
  }

  /** holdWindowKey starts the short settle window once a create resolves. */
  function holdWindowKey(
    key: string,
    created: Promise<TerminalTab>,
  ): void {
    // An abort/close released the key while the create was settling.
    if (windowInflight.get(key) !== created) {
      return;
    }
    const timer = setTimeout(() => {
      windowHoldTimers.delete(key);
      if (windowInflight.get(key) === created) {
        windowInflight.delete(key);
        windowBusyKeys.value.delete(key);
      }
    }, WINDOW_SINGLE_FLIGHT_HOLD_MS);
    windowHoldTimers.set(key, timer);
  }

  /**
   * isWindowOpening reports whether one window target is creating or inside
   * its settle window. The toolbar uses it to stay busy/disabled for the same
   * target only; a different target is unaffected (JUS-248 r2).
   */
  function isWindowOpening(options: TerminalOpenOptions): boolean {
    return windowBusyKeys.value.has(targetKey(options));
  }

  /** applyPatch merges a runtime lifecycle patch into one tab. */
  function applyPatch(tab: TerminalTab, patch: TerminalStatePatch): void {
    if (!findTab(tab.id)) {
      return;
    }
    if (patch.status !== undefined) {
      tab.status = patch.status;
    }
    if (patch.exitCode !== undefined) {
      tab.exitCode = patch.exitCode;
    }
    if (patch.exitSignal !== undefined) {
      tab.exitSignal = patch.exitSignal;
    }
    if (patch.errorCode !== undefined) {
      tab.errorCode = patch.errorCode;
    }
    if (patch.shell !== undefined) {
      tab.shell = patch.shell;
    }
    if (patch.serviceUser !== undefined) {
      tab.serviceUser = patch.serviceUser;
    }
    if (patch.cols !== undefined) {
      tab.cols = patch.cols;
    }
    if (patch.rows !== undefined) {
      tab.rows = patch.rows;
    }
  }

  /** applySuggestion mirrors the runtime's prompt/suggestion snapshot. */
  function applySuggestion(
    tab: TerminalTab,
    state: QuickCommandSuggestionState,
  ): void {
    if (!findTab(tab.id)) {
      return;
    }
    tab.promptPhase = state.phase;
    tab.integrationDetected = state.detected;
    tab.alternateScreen = state.alternateScreen;
    tab.suggestion = state.suggestion;
  }

  /** startSession creates the HTTP session and starts its runtime. */
  async function startSession(tab: TerminalTab): Promise<void> {
    tab.status = "connecting";
    tab.errorCode = null;
    tab.exitCode = null;
    tab.exitSignal = "";
    const controller = new AbortController();
    controllers.set(tab.id, controller);
    tab.promptPhase = "unknown";
    tab.integrationDetected = false;
    tab.alternateScreen = false;
    tab.suggestion = null;
    try {
      // The version lookup runs beside the HTTP create and never gates the
      // session: a hung /version only drops the banner's version tag.
      const hostSession = isHostTerminalTarget(tab.target);
      const versionRequest = hostSession ? runningGothamVersion() : null;
      const session = await createTerminalSession(
        tab.serverId,
        {
          target: tab.target,
          ...(tab.root !== "" ? { root: tab.root } : {}),
          ...(tab.cwd !== "" ? { cwd: tab.cwd } : {}),
          cols: tab.cols,
          rows: tab.rows,
        },
        controller.signal,
      );
      // A closed tab/window aborts the create; never revive its state.
      if (controller.signal.aborted) {
        return;
      }
      // A tab can be closed while the create call is in flight.
      if (!findTab(tab.id)) {
        await closeTerminalSession(tab.serverId, session.id).catch(() => {
          // The tab is gone; the server expires an unattached session anyway.
        });
        return;
      }
      tab.sessionId = session.id;
      tab.cols = session.cols;
      tab.rows = session.rows;
      const runtime = new TerminalRuntime({
        session,
        getToken: currentTerminalToken,
        theme: appearance.theme,
        ...(hostSession
          ? {
              banner: {
                version: knownGothamVersion(),
                serverName: tab.serverName,
                serverId: tab.serverId,
                target: tab.target,
                serverLabel: i18n.global.t("terminal.banner.server"),
              },
              ...(versionRequest ? { bannerVersion: versionRequest } : {}),
            }
          : {}),
        onState: (patch) => applyPatch(tab, patch),
        quickCommands: () => useQuickCommandsStore().commands,
        onSuggestion: (state) => applySuggestion(tab, state),
      });
      runtimes.set(tab.id, runtime);
      const surface = surfaces.get(tab.id);
      if (surface) {
        runtime.attach(surface);
      }
      await runtime.connect();
    } catch (error) {
      // An abort is a deliberate close: it is not an error state to show.
      if (controller.signal.aborted) {
        return;
      }
      tab.status = "error";
      tab.errorCode = terminalFailure(error).code;
    } finally {
      controllers.delete(tab.id);
    }
  }

  /** createTab appends one tab and starts its session. */
  async function createTab(
    options: TerminalOpenOptions,
    mode: TerminalMode,
  ): Promise<TerminalTab> {
    const tab: TerminalTab = {
      id: tabId(),
      serverId: options.serverId,
      serverName: options.serverName ?? "",
      target: options.target ?? "host",
      root: options.root ?? "",
      cwd: options.cwd ?? "",
      title: terminalTitle(options),
      mode,
      sessionId: "",
      status: "connecting",
      cols: options.cols ?? DEFAULT_TERMINAL_COLS,
      rows: options.rows ?? DEFAULT_TERMINAL_ROWS,
      shell: "",
      serviceUser: "",
      exitCode: null,
      exitSignal: "",
      errorCode: null,
      createdAt: Date.now(),
      sourcePath: options.sourcePath ?? "",
      promptPhase: "unknown",
      integrationDetected: false,
      alternateScreen: false,
      suggestion: null,
    };
    tabs.value.push(tab);
    // Read the tab back from the reactive array: Vue wraps the raw object on
    // access, and every later mutation (runtime patches included) must go
    // through that proxy or the UI never re-renders.
    const stored = tabs.value[tabs.value.length - 1]!;
    if (mode === "window") {
      activeTabId.value = stored.id;
    }
    createTargets.set(stored.id, { key: targetKey(options), mode });
    await startSession(stored);
    return stored;
  }

  /**
   * openPageSession opens (or reuses) the page-mode session for one target.
   * Reuse is keyed by server + target + root + cwd, so revisiting the pane or
   * remounting the page cannot spawn a duplicate shell. A session that was
   * detached is returned as-is: the caller renders its "open in window" state
   * instead of creating a second session.
   *
   * `fresh: true` is the explicit exception: the host asked for a brand-new
   * page shell (for example from the moved-to-window notice), so the detached
   * tab is left in the window and a page tab is created. Rapid repeated fresh
   * opens for one target still share the in-flight promise.
   */
  function openPageSession(
    options: TerminalOpenOptions,
  ): Promise<TerminalTab> {
    if (options.fresh !== true) {
      const existing = findTabForTarget(options);
      if (existing) {
        // Another entry point (menu page, server tab) reused this shell: keep
        // the latest source so the dock can resume the pane the user came from.
        if (options.sourcePath) {
          existing.sourcePath = options.sourcePath;
        }
        return Promise.resolve(existing);
      }
    }
    const key = options.fresh === true
      ? `${targetKey(options)}\u0000fresh`
      : targetKey(options);
    const pending = inflight.get(key);
    if (pending) {
      return pending;
    }
    const created = createTab(options, "page").finally(() => {
      inflight.delete(key);
    });
    inflight.set(key, created);
    return created;
  }

  /**
   * openWindowSession opens a new tab in the floating window. A deliberate
   * later call is intentional (each call is a new independent shell); a second
   * activation for the SAME target while the first create is still in flight
   * is an accident from one gesture and joins that call instead of starting a
   * second shell.
   */
  async function openWindowSession(
    options: TerminalOpenOptions,
  ): Promise<TerminalTab> {
    const key = targetKey(options);
    const pending = windowInflight.get(key);
    if (pending) {
      return pending;
    }
    const windowStore = useTerminalWindowStore();
    // Restore a minimized window and bring it to the front so a new shell
    // opened from another surface is actually visible (JUS-248).
    windowStore.setMinimized(false);
    windowStore.raise();
    const created = createTab(options, "window");
    windowInflight.set(key, created);
    windowBusyKeys.value.add(key);
    // Hold the target's single-flight key briefly after settle so a fast create
    // cannot turn one human double-click into two shells (JUS-248 r2). The
    // catch keeps this scheduling chain quiet when the create itself rejects;
    // the caller still receives the rejection through `created`.
    void created
      .catch(() => undefined)
      .then(() => holdWindowKey(key, created));
    return created;
  }

  /**
   * detachToWindow performs the one-way transfer: the SAME tab, session id,
   * renderer and buffer move into the window. No HTTP create is issued.
   */
  function detachToWindow(id: string): void {
    const tab = findTab(id);
    if (!tab || tab.mode === "window") {
      return;
    }
    tab.mode = "window";
    activeTabId.value = tab.id;
    useTerminalWindowStore().setMinimized(false);
  }

  /** activate makes one window tab active. */
  function activate(id: string): void {
    if (findTab(id)) {
      activeTabId.value = id;
    }
  }

  /** attach offers a surface element to the live runtime (or queues it). */
  function attach(id: string, container: HTMLElement): void {
    surfaces.set(id, container);
    displayed.value.add(id);
    runtimes.get(id)?.attach(container);
  }

  /**
   * detach drops a pane's remembered element when that pane unmounts. The
   * runtime keeps its own host, so a later mount (or a transfer) re-attaches;
   * the store never holds a removed DOM node.
   */
  function detach(id: string, container: HTMLElement): void {
    if (surfaces.get(id) === container) {
      surfaces.delete(id);
      displayed.value.delete(id);
    }
  }

  /** isDisplayed reports whether a mounted pane currently owns the host. */
  function isDisplayed(id: string): boolean {
    return displayed.value.has(id);
  }

  /** focus focuses the live renderer of one tab. */
  function focus(id: string): void {
    runtimes.get(id)?.focus();
  }

  /** fit refits one live renderer (after a restore or a window resize). */
  function fit(id: string): void {
    runtimes.get(id)?.fit();
  }

  /**
   * setTheme applies one named theme to every live renderer and persists it
   * for this account. It never recreates a session: xterm repaints the same
   * buffer and the PTY keeps its id/process.
   */
  function setTheme(next: TerminalThemeName): void {
    appearance.setTheme(next);
    for (const runtime of runtimes.values()) {
      runtime.setTheme(next);
    }
  }

  /** acceptSuggestion inserts the matched command without running it. */
  function acceptSuggestion(id: string): TerminalCommandResult {
    const runtime = runtimes.get(id);
    if (!runtime) {
      return { ok: false, failure: "unavailable" };
    }
    return runtime.acceptSuggestion();
  }

  /** insertCommand inserts saved text at the prompt without running it. */
  function insertCommand(id: string, text: string): TerminalCommandResult {
    const runtime = runtimes.get(id);
    if (!runtime) {
      return { ok: false, failure: "unavailable" };
    }
    return runtime.insertCommand(text);
  }

  /** dismissSuggestion hides the abbreviation suggestion. */
  function dismissSuggestion(id: string): void {
    runtimes.get(id)?.dismissSuggestion();
  }

  /** setComposing suspends suggestions while an IME composition is open. */
  function setComposing(id: string, composing: boolean): void {
    runtimes.get(id)?.setComposing(composing);
  }

  /** retry is the ONLY reconnect path: it creates a new session, no replay. */
  async function retry(id: string): Promise<void> {
    const tab = findTab(id);
    if (!tab || retrying.has(id)) {
      return;
    }
    if (
      tab.status !== "disconnected" &&
      tab.status !== "error" &&
      tab.status !== "exited"
    ) {
      return;
    }
    retrying.add(id);
    const previousSession = tab.sessionId;
    const previousServer = tab.serverId;
    const runtime = runtimes.get(id);
    runtimes.delete(id);
    runtime?.dispose(true);
    tab.sessionId = "";
    try {
      if (previousSession !== "") {
        await closeTerminalSession(previousServer, previousSession).catch(() => {
          // Best-effort: an ended session answers 404, which is success here.
        });
      }
      if (!findTab(id)) {
        return;
      }
      await startSession(tab);
    } finally {
      retrying.delete(id);
    }
  }

  /**
   * requestClose asks for confirmation before releasing the given tabs. The
   * returned promise lets a session-terminating flow (logout) wait for the
   * user's answer: true means the listed sessions were released, false means
   * the user kept them. UI callers that do not care can ignore it.
   */
  function requestClose(ids: string[]): Promise<boolean> {
    if (closeRequest.value !== null) {
      return closePromise ?? Promise.resolve(false);
    }
    const unique = [...new Set(ids)].filter((id) => findTab(id) !== null);
    if (unique.length === 0) {
      return Promise.resolve(true);
    }
    closePromise = new Promise<boolean>((resolve) => {
      closeResolve = resolve;
    });
    closeRequest.value = {
      ids: unique,
      scope: unique.length > 1 ? "window" : "tab",
    };
    return closePromise;
  }

  /** settleClose resolves the awaited confirmation exactly once. */
  function settleClose(confirmed: boolean): void {
    const resolve = closeResolve;
    closeResolve = null;
    closePromise = null;
    resolve?.(confirmed);
  }

  /** cancelClose keeps every session intact and returns to the window. */
  function cancelClose(): void {
    closeRequest.value = null;
    settleClose(false);
  }

  /** confirmClose releases exactly the sessions the dialog listed. */
  function confirmClose(): void {
    const request = closeRequest.value;
    closeRequest.value = null;
    if (request) {
      closeTabs(request.ids);
    }
    settleClose(request !== null);
  }

  /**
   * confirmCloseAll runs the shared close path until no live session remains.
   * It is the logout gate: a cancel returns false and aborts the sign-out; an
   * already-open dialog is awaited and, if it targeted a subset, the remaining
   * live sessions get their own confirmation rather than being killed silently.
   */
  async function confirmCloseAll(): Promise<boolean> {
    while (hasLiveSessions.value) {
      const confirmed = await requestClose(tabs.value.map((tab) => tab.id));
      if (!confirmed) {
        return false;
      }
    }
    return true;
  }

  /** closeTabs disposes renderers and removes the tabs (no prompt). */
  function closeTabs(ids: string[]): void {
    for (const id of ids) {
      const tab = findTab(id);
      if (!tab) {
        continue;
      }
      // A create still in flight belongs to this tab: abort it and release its
      // single-flight key immediately instead of waiting for the HTTP timeout.
      // A settled tab's key is owned by the hold timer, never by this close.
      const target = createTargets.get(id);
      const controller = controllers.get(id);
      controller?.abort();
      controllers.delete(id);
      createTargets.delete(id);
      if (controller !== undefined && target?.mode === "window") {
        releaseWindowKey(target.key);
      }
      const runtime = runtimes.get(id);
      runtimes.delete(id);
      runtime?.dispose(true);
      surfaces.delete(id);
      displayed.value.delete(id);
      retrying.delete(id);
      if (tab.sessionId !== "") {
        void closeTerminalSession(tab.serverId, tab.sessionId).catch(() => {
          // The server also revokes an ended session; cleanup is best-effort.
        });
      }
      const index = tabs.value.findIndex((item) => item.id === id);
      if (index !== -1) {
        tabs.value.splice(index, 1);
      }
    }
    if (activeTabId.value !== "" && findTab(activeTabId.value) === null) {
      activeTabId.value = tabs.value[tabs.value.length - 1]?.id ?? "";
    }
    if (tabs.value.length === 0) {
      activeTabId.value = "";
      if (closeRequest.value !== null) {
        closeRequest.value = null;
        settleClose(true);
      }
      useTerminalWindowStore().setMinimized(false);
    }
  }

  /** closeAll strips every session through the same cleanup path. */
  function closeAll(): void {
    closeTabs(tabs.value.map((tab) => tab.id));
  }

  /**
   * reset disposes every renderer and socket for logout. It never issues a
   * DELETE (the sign-out already revokes the lineage) and keeps no transcript
   * or credential: the tabs array, runtimes and queued hosts are dropped.
   */
  function reset(): void {
    for (const controller of controllers.values()) {
      controller.abort();
    }
    controllers.clear();
    createTargets.clear();
    for (const timer of windowHoldTimers.values()) {
      clearTimeout(timer);
    }
    windowHoldTimers.clear();
    for (const runtime of runtimes.values()) {
      runtime.dispose(true);
    }
    runtimes.clear();
    surfaces.clear();
    displayed.value.clear();
    inflight.clear();
    windowInflight.clear();
    windowBusyKeys.value.clear();
    retrying.clear();
    tabs.value = [];
    activeTabId.value = "";
    closeRequest.value = null;
    settleClose(false);
  }

  return {
    tabs,
    activeTabId,
    activeTab,
    pageTabs,
    windowTabs,
    windowOpenCount,
    sessionCount,
    hasLiveSessions,
    closeRequest,
    theme: computed<TerminalThemeName>(() => appearance.theme),
    findTab,
    findTabForTarget,
    isWindowOpening,
    openPageSession,
    openWindowSession,
    detachToWindow,
    activate,
    attach,
    detach,
    isDisplayed,
    focus,
    fit,
    setTheme,
    acceptSuggestion,
    insertCommand,
    dismissSuggestion,
    setComposing,
    retry,
    requestClose,
    cancelClose,
    confirmClose,
    confirmCloseAll,
    closeTabs,
    closeAll,
    reset,
  };
});

export type { TerminalStatePatch, TerminalStatus };
