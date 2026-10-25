import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  applicationTerminalFailure,
  closeApplicationTerminalSession,
  closeTerminalSession,
  createApplicationTerminalSession,
  createTerminalSession,
  currentTerminalToken,
  listApplicationTerminalTargets,
  terminalFailure,
} from "@/features/terminal/api/terminal";
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import type { ApplicationTerminalTarget } from "@/features/terminal/schemas/terminal";
import {
  chooseRecoveryTarget,
  containerTabTitle,
} from "@/features/terminal/utils/containerRecovery";
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
import { isContainerTerminalTarget } from "@/features/terminal/utils/terminal";
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

export type TerminalMode = "page" | "window";

export interface TerminalTab {
  id: string;
  serverId: string;
  serverName: string;
  target: string;
  root: string;
  cwd: string;
  title: string;
  applicationId: string;
  applicationName: string;
  service: string;
  containerId: string;
  containerName: string;
  targetLabel: string;
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
  sourcePath: string;
  promptPhase: PromptPhase;
  integrationDetected: boolean;
  alternateScreen: boolean;
  suggestion: QuickCommandSuggestion | null;
}

export interface TerminalOpenOptions {
  serverId: string;
  serverName?: string;
  target?: string;
  root?: string;
  cwd?: string;
  cols?: number;
  rows?: number;
  title?: string;
  applicationId?: string;
  applicationName?: string;
  service?: string;
  containerId?: string;
  containerName?: string;
  targetLabel?: string;
  sourcePath?: string;
  fresh?: boolean;
}

export interface TerminalCloseRequest {
  ids: string[];
  scope: "tab" | "window";
}

const DEFAULT_TERMINAL_COLS = 120;
const DEFAULT_TERMINAL_ROWS = 32;

export const WINDOW_SINGLE_FLIGHT_HOLD_MS = 600;

export const PAGE_SINGLE_FLIGHT_HOLD_MS = WINDOW_SINGLE_FLIGHT_HOLD_MS;

function tabId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `term-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function targetKey(options: TerminalOpenOptions): string {
  return [
    options.serverId,
    options.target ?? "host",
    options.root ?? "",
    options.cwd ?? "",
    options.applicationId ?? "",
    options.service ?? "",
    options.containerId ?? "",
  ].join("\u0000");
}

function terminalTitle(options: TerminalOpenOptions): string {
  if (options.title) {
    return options.title;
  }
  if (isContainerTerminalTarget(options.target ?? "")) {
    const application = options.applicationName || options.serverName;
    const target = options.targetLabel || options.containerName;
    if (application && target) {
      return `${application} · ${target}`;
    }
    return target || application || options.serverId;
  }
  const name = options.serverName || options.serverId;
  const where = options.cwd && options.cwd !== "." ? options.cwd : options.root;
  return where ? `${name} · ${where}` : name;
}

export const useTerminalStore = defineStore("terminal", () => {
  const tabs = ref<TerminalTab[]>([]);
  const activeTabId = ref<string>("");
  const activePageTabId = ref<string>("");
  const closeRequest = ref<TerminalCloseRequest | null>(null);
  const appearance = useTerminalAppearanceStore();

  const runtimes = new Map<string, TerminalRuntime>();
  const surfaces = new Map<string, HTMLElement>();
  const displayed = ref<Set<string>>(new Set());
  const inflight = new Map<string, Promise<TerminalTab>>();
  const pageHoldTimers = new Map<string, ReturnType<typeof setTimeout>>();
  const windowInflight = new Map<string, Promise<TerminalTab>>();
  const windowHoldTimers = new Map<string, ReturnType<typeof setTimeout>>();
  const windowBusyKeys = ref<Set<string>>(new Set());
  const controllers = new Map<string, AbortController>();
  const createTargets = new Map<string, { key: string; mode: TerminalMode }>();
  const retrying = new Set<string>();
  const retryHandlers = new Map<string, () => void | Promise<void>>();
  const dismissedContainerRestarts = new Map<string, string>();
  let closeResolve: ((_confirmed: boolean) => void) | null = null;
  let closePromise: Promise<boolean> | null = null;

  const activeTab = computed<TerminalTab | null>(
    () => tabs.value.find((tab) => tab.id === activeTabId.value) ?? null,
  );
  const pageTabs = computed<TerminalTab[]>(() =>
    tabs.value.filter((tab) => tab.mode === "page"),
  );
  const pageActiveTab = computed<TerminalTab | null>(
    () =>
      pageTabs.value.find((tab) => tab.id === activePageTabId.value) ??
      pageTabs.value[pageTabs.value.length - 1] ??
      null,
  );
  const windowTabs = computed<TerminalTab[]>(() =>
    tabs.value.filter((tab) => tab.mode === "window"),
  );
  const windowOpenCount = computed<number>(() => windowTabs.value.length);
  const sessionCount = computed<number>(() => tabs.value.length);
  const hasLiveSessions = computed<boolean>(() =>
    tabs.value.some(
      (tab) => tab.status === "connecting" || tab.status === "connected",
    ),
  );

  function findTab(id: string): TerminalTab | null {
    return tabs.value.find((tab) => tab.id === id) ?? null;
  }

  function findTabForTarget(options: TerminalOpenOptions): TerminalTab | null {
    const key = targetKey(options);
    const matches = tabs.value.filter((tab) => targetKey(tab) === key);
    return (
      matches.find((tab) => tab.mode === "page") ?? matches[0] ?? null
    );
  }

  function releasePageKey(key: string): void {
    const timer = pageHoldTimers.get(key);
    if (timer !== undefined) {
      clearTimeout(timer);
      pageHoldTimers.delete(key);
    }
    inflight.delete(key);
  }

  function holdPageKey(key: string, created: Promise<TerminalTab>): void {
    if (inflight.get(key) !== created) {
      return;
    }
    const timer = setTimeout(() => {
      pageHoldTimers.delete(key);
      if (inflight.get(key) === created) {
        inflight.delete(key);
      }
    }, PAGE_SINGLE_FLIGHT_HOLD_MS);
    pageHoldTimers.set(key, timer);
  }

  function releaseWindowKey(key: string): void {
    const timer = windowHoldTimers.get(key);
    if (timer !== undefined) {
      clearTimeout(timer);
      windowHoldTimers.delete(key);
    }
    windowInflight.delete(key);
    windowBusyKeys.value.delete(key);
  }

  function holdWindowKey(
    key: string,
    created: Promise<TerminalTab>,
  ): void {
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

  function isWindowOpening(options: TerminalOpenOptions): boolean {
    return windowBusyKeys.value.has(targetKey(options));
  }

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

  async function requestSession(
    tab: TerminalTab,
    signal: AbortSignal,
  ) {
    if (isContainerTerminalTarget(tab.target)) {
      return createApplicationTerminalSession(
        tab.applicationId,
        {
          service: tab.service,
          containerId: tab.containerId,
          cols: tab.cols,
          rows: tab.rows,
        },
        signal,
      );
    }
    return createTerminalSession(
      tab.serverId,
      {
        target: tab.target,
        ...(tab.root !== "" ? { root: tab.root } : {}),
        ...(tab.cwd !== "" ? { cwd: tab.cwd } : {}),
        cols: tab.cols,
        rows: tab.rows,
      },
      signal,
    );
  }

  function closeSession(tab: TerminalTab, sessionId: string): Promise<void> {
    if (isContainerTerminalTarget(tab.target)) {
      return closeApplicationTerminalSession(
        tab.applicationId,
        sessionId,
      );
    }
    return closeTerminalSession(tab.serverId, sessionId);
  }

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
      const hostSession = isHostTerminalTarget(tab.target);
      const versionRequest = hostSession ? runningGothamVersion() : null;
      const session = await requestSession(tab, controller.signal);
      if (controller.signal.aborted) {
        return;
      }
      if (!findTab(tab.id)) {
        await closeSession(tab, session.id).catch(() => {
        });
        return;
      }
      if (isContainerTerminalTarget(session.target)) {
        tab.applicationId = session.application_id ?? tab.applicationId;
        tab.service = session.service ?? tab.service;
        tab.containerId = session.container_id ?? tab.containerId;
        tab.containerName = session.container_name ?? tab.containerName;
        tab.targetLabel = session.target_label ?? tab.targetLabel;
        tab.title = terminalTitle({
          serverId: tab.serverId,
          serverName: tab.serverName,
          target: tab.target,
          applicationId: tab.applicationId,
          applicationName: tab.applicationName,
          containerId: tab.containerId,
          containerName: tab.containerName,
          targetLabel: tab.targetLabel,
        });
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
      if (controller.signal.aborted) {
        return;
      }
      tab.status = "error";
      tab.errorCode = isContainerTerminalTarget(tab.target)
        ? applicationTerminalFailure(error).code
        : terminalFailure(error).code;
    } finally {
      controllers.delete(tab.id);
    }
  }

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
      applicationId: options.applicationId ?? "",
      applicationName: options.applicationName ?? "",
      service: options.service ?? "",
      containerId: options.containerId ?? "",
      containerName: options.containerName ?? "",
      targetLabel: options.targetLabel ?? "",
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
    const stored = tabs.value[tabs.value.length - 1]!;
    if (mode === "window") {
      activeTabId.value = stored.id;
    } else {
      activePageTabId.value = stored.id;
    }
    createTargets.set(stored.id, { key: targetKey(options), mode });
    await startSession(stored);
    return stored;
  }

  function openPageSession(
    options: TerminalOpenOptions,
  ): Promise<TerminalTab> {
    if (options.fresh !== true) {
      const existing = findTabForTarget(options);
      if (existing) {
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
    const created = createTab(options, "page");
    inflight.set(key, created);
    void created
      .catch(() => undefined)
      .then(() => holdPageKey(key, created));
    return created;
  }

  async function openWindowSession(
    options: TerminalOpenOptions,
  ): Promise<TerminalTab> {
    const key = targetKey(options);
    const pending = windowInflight.get(key);
    if (pending) {
      return pending;
    }
    const windowStore = useTerminalWindowStore();
    windowStore.setMinimized(false);
    windowStore.raise();
    const created = createTab(options, "window");
    windowInflight.set(key, created);
    windowBusyKeys.value.add(key);
    void created
      .catch(() => undefined)
      .then(() => holdWindowKey(key, created));
    return created;
  }

  function detachToWindow(id: string): void {
    const tab = findTab(id);
    if (!tab || tab.mode === "window") {
      return;
    }
    tab.mode = "window";
    activeTabId.value = tab.id;
    if (activePageTabId.value === id) {
      const next = pageTabs.value[pageTabs.value.length - 1];
      activePageTabId.value = next?.id ?? "";
    }
    useTerminalWindowStore().setMinimized(false);
  }

  function activate(id: string): void {
    if (findTab(id)) {
      activeTabId.value = id;
    }
  }

  function activatePage(id: string): void {
    const tab = findTab(id);
    if (tab?.mode === "page") {
      activePageTabId.value = id;
    }
  }

  function attach(id: string, container: HTMLElement): void {
    surfaces.set(id, container);
    displayed.value.add(id);
    runtimes.get(id)?.attach(container);
  }

  function detach(id: string, container: HTMLElement): void {
    if (surfaces.get(id) === container) {
      surfaces.delete(id);
      displayed.value.delete(id);
    }
  }

  function isDisplayed(id: string): boolean {
    return displayed.value.has(id);
  }

  function focus(id: string): void {
    runtimes.get(id)?.focus();
  }

  function fit(id: string): void {
    runtimes.get(id)?.fit();
  }

  function cursorRow(id: string): { top: number; height: number } | null {
    return runtimes.get(id)?.getCursorRow?.() ?? null;
  }

  function watchCursor(id: string, listener: () => void): () => void {
    return runtimes.get(id)?.watchCursor?.(listener) ?? (() => {});
  }

  function setTheme(next: TerminalThemeName): void {
    appearance.setTheme(next);
    for (const runtime of runtimes.values()) {
      runtime.setTheme(next);
    }
  }

  function acceptSuggestion(id: string): TerminalCommandResult {
    const runtime = runtimes.get(id);
    if (!runtime) {
      return { ok: false, failure: "unavailable" };
    }
    return runtime.acceptSuggestion();
  }

  function insertCommand(id: string, text: string): TerminalCommandResult {
    const runtime = runtimes.get(id);
    if (!runtime) {
      return { ok: false, failure: "unavailable" };
    }
    return runtime.insertCommand(text);
  }

  function dismissSuggestion(id: string): void {
    runtimes.get(id)?.dismissSuggestion();
  }

  function setComposing(id: string, composing: boolean): void {
    runtimes.get(id)?.setComposing(composing);
  }

  function registerRetryHandler(
    id: string,
    handler: () => void | Promise<void>,
  ): () => void {
    retryHandlers.set(id, handler);
    return () => {
      if (retryHandlers.get(id) === handler) {
        retryHandlers.delete(id);
      }
    };
  }

  function dismissContainerRestart(
    applicationId: string,
    target: string,
  ): void {
    if (applicationId === "") {
      return;
    }
    dismissedContainerRestarts.set(applicationId, target);
  }

  function containerRestartTarget(applicationId: string): string | null {
    return dismissedContainerRestarts.get(applicationId) ?? null;
  }

  function clearContainerRestart(applicationId: string): void {
    dismissedContainerRestarts.delete(applicationId);
  }

  function applyContainerTarget(
    tab: TerminalTab,
    target: ApplicationTerminalTarget,
  ): void {
    tab.service = target.service;
    tab.containerId = target.container_id;
    tab.containerName = target.name;
    tab.targetLabel = target.label || target.name;
    tab.title = containerTabTitle(tab.applicationName, tab.targetLabel);
  }

  async function recoverContainerTab(
    id: string,
    tab: TerminalTab,
  ): Promise<void> {
    const previousSession = tab.sessionId;
    const runtime = runtimes.get(id);
    runtimes.delete(id);
    runtime?.dispose(true);
    tab.sessionId = "";
    try {
      if (previousSession !== "") {
        await closeSession(tab, previousSession).catch(() => {
        });
      }
      if (!findTab(id)) {
        return;
      }
      const resolved = await listApplicationTerminalTargets(tab.applicationId);
      if (!findTab(id)) {
        return;
      }
      const choice = chooseRecoveryTarget(
        resolved.containers,
        { service: tab.service, containerId: tab.containerId },
        true,
      );
      if (choice.target === null) {
        tab.status = "error";
        tab.errorCode = "containerGone";
        return;
      }
      applyContainerTarget(tab, choice.target);
      await startSession(tab);
    } catch (error) {
      if (findTab(id)) {
        tab.status = "error";
        tab.errorCode = applicationTerminalFailure(error).code;
      }
    }
  }

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
    try {
      if (isContainerTerminalTarget(tab.target)) {
        const handler = retryHandlers.get(id);
        if (handler) {
          await handler();
          return;
        }
        await recoverContainerTab(id, tab);
        return;
      }
      const previousSession = tab.sessionId;
      const runtime = runtimes.get(id);
      runtimes.delete(id);
      runtime?.dispose(true);
      tab.sessionId = "";
      if (previousSession !== "") {
        await closeSession(tab, previousSession).catch(() => {
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

  function settleClose(confirmed: boolean): void {
    const resolve = closeResolve;
    closeResolve = null;
    closePromise = null;
    resolve?.(confirmed);
  }

  function cancelClose(): void {
    closeRequest.value = null;
    settleClose(false);
  }

  function confirmClose(): void {
    const request = closeRequest.value;
    closeRequest.value = null;
    if (request) {
      closeTabs(request.ids);
    }
    settleClose(request !== null);
  }

  async function confirmCloseAll(): Promise<boolean> {
    while (hasLiveSessions.value) {
      const confirmed = await requestClose(tabs.value.map((tab) => tab.id));
      if (!confirmed) {
        return false;
      }
    }
    return true;
  }

  function closeTabs(ids: string[]): void {
    for (const id of ids) {
      const tab = findTab(id);
      if (!tab) {
        continue;
      }
      const target = createTargets.get(id);
      const controller = controllers.get(id);
      controller?.abort();
      controllers.delete(id);
      createTargets.delete(id);
      if (controller !== undefined && target?.mode === "window") {
        releaseWindowKey(target.key);
      }
      if (target?.mode === "page") {
        releasePageKey(target.key);
      }
      const runtime = runtimes.get(id);
      runtimes.delete(id);
      runtime?.dispose(true);
      surfaces.delete(id);
      displayed.value.delete(id);
      retrying.delete(id);
      retryHandlers.delete(id);
      if (tab.sessionId !== "") {
        void closeSession(tab, tab.sessionId).catch(() => {
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
    if (activePageTabId.value !== "" && findTab(activePageTabId.value) === null) {
      const pages = tabs.value.filter((tab) => tab.mode === "page");
      activePageTabId.value = pages[pages.length - 1]?.id ?? "";
    }
    if (tabs.value.length === 0) {
      activeTabId.value = "";
      activePageTabId.value = "";
      if (closeRequest.value !== null) {
        closeRequest.value = null;
        settleClose(true);
      }
      useTerminalWindowStore().setMinimized(false);
    }
  }

  function closeAll(): void {
    closeTabs(tabs.value.map((tab) => tab.id));
  }

  function closeContainerSessions(applicationId = ""): void {
    const ids = tabs.value
      .filter(
        (tab) =>
          isContainerTerminalTarget(tab.target) &&
          (applicationId === "" || tab.applicationId === applicationId),
      )
      .map((tab) => tab.id);
    if (ids.length > 0) {
      closeTabs(ids);
    }
  }

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
    for (const timer of pageHoldTimers.values()) {
      clearTimeout(timer);
    }
    pageHoldTimers.clear();
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
    retryHandlers.clear();
    dismissedContainerRestarts.clear();
    tabs.value = [];
    activeTabId.value = "";
    activePageTabId.value = "";
    closeRequest.value = null;
    settleClose(false);
  }

  return {
    tabs,
    activeTabId,
    activePageTabId,
    activeTab,
    pageActiveTab,
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
    activatePage,
    attach,
    detach,
    isDisplayed,
    focus,
    fit,
    cursorRow,
    watchCursor,
    setTheme,
    acceptSuggestion,
    insertCommand,
    dismissSuggestion,
    setComposing,
    retry,
    registerRetryHandler,
    dismissContainerRestart,
    containerRestartTarget,
    clearContainerRestart,
    requestClose,
    cancelClose,
    confirmClose,
    confirmCloseAll,
    closeTabs,
    closeAll,
    closeContainerSessions,
    reset,
  };
});

export type { TerminalStatePatch, TerminalStatus };
