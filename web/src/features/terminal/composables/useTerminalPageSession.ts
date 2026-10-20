import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import type { Server } from "@/features/servers";
import { serverStatusKey, useServersStore } from "@/features/servers";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import { isContainerTerminalTarget as isContainerTarget } from "@/features/terminal/utils/terminal";
import type {
  TerminalOpenOptions,
  TerminalTab,
} from "@/features/terminal/stores/terminal";

/**
 * Page-mode tab lifecycle for the standalone terminal page (JUS-246/JUS-265).
 *
 * The page now owns a tab strip: several page sessions can be open at once and
 * the active one is selected here. Sessions are created only by an explicit
 * action (a server card in the empty state, the add-tab picker, or the
 * restart/transfer actions); a route change never starts a shell on its own.
 *
 * `openPageSession` remains idempotent per target, and the picker asks for a
 * fresh tab per deliberate pick with the store's single-flight key, so one
 * gesture can never create two shells.
 */
export function useTerminalPageSession() {
  const { t } = useI18n();
  const route = useRoute();
  const router = useRouter();
  const authStore = useAuthStore();
  const serversStore = useServersStore();
  const terminal = useTerminalStore();

  const selectedId = ref<string>(terminal.pageActiveTab?.serverId ?? "");
  const starting = ref(false);
  /** closed is true after the last page tab was confirmed closed. */
  const closed = ref(false);
  /** detachedId is the page tab that moved into the floating window. */
  const detachedId = ref<string>("");
  /** unknownServerId is a deep link to a node this control plane never knew. */
  const unknownServerId = ref<string>("");
  /** lastClosed remembers the target the restart button re-opens. */
  const lastClosed = ref<TerminalOpenOptions | null>(null);
  /** syncingQuery guards the route → selection echo while we write the URL. */
  let syncingQuery = false;

  const servers = computed<Server[]>(() => serversStore.servers);
  const pageTabs = computed(() => terminal.pageTabs);
  const activeTab = computed(() => terminal.pageActiveTab);
  const tabId = computed<string>(() => activeTab.value?.id ?? "");
  const selectedServer = computed<Server | null>(
    () =>
      servers.value.find((server) => server.id === selectedId.value) ?? null,
  );
  const serverReady = computed<boolean>(
    () => selectedServer.value?.status === "ready",
  );

  /** selectedStatusLabel localizes the current server's lifecycle word. */
  const selectedStatusLabel = computed<string>(() =>
    selectedServer.value
      ? t(serverStatusKey(selectedServer.value.status))
      : "",
  );

  /** sourcePathFor is the resume route of one server's host shell. */
  function sourcePathFor(serverId: string): string {
    return router.resolve({
      name: "terminal",
      query: { ...route.query, server: serverId },
    }).fullPath;
  }

  /** fetchServers loads the empty-state card list once per mount. */
  async function fetchServers(): Promise<void> {
    if (servers.value.length === 0 && !serversStore.loading) {
      await serversStore.fetchServers().catch(() => undefined);
    }
  }

  /**
   * resolveServer waits for the shell's server read to settle before deciding
   * a deep-linked id is unknown: the app shell may already be fetching the
   * list when the page mounts, so a non-empty "loading" store is not "empty".
   */
  async function resolveServer(serverId: string): Promise<Server | null> {
    const known = servers.value.find((server) => server.id === serverId);
    if (known !== undefined || serverId === "") {
      return known ?? null;
    }
    await fetchServers();
    for (
      let attempt = 0;
      attempt < 40 && servers.value.length === 0 && serversStore.loading;
      attempt += 1
    ) {
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
    return servers.value.find((server) => server.id === serverId) ?? null;
  }

  /** activateTab selects one page tab. */
  function activateTab(id: string): void {
    terminal.activatePage(id);
  }

  /** optionsFor reconstructs one tab's open options (restart path). */
  function optionsFor(
    tab: TerminalTab,
    fresh: boolean,
  ): TerminalOpenOptions {
    return {
      serverId: tab.serverId,
      serverName: tab.serverName,
      target: tab.target,
      root: tab.root,
      cwd: tab.cwd,
      applicationId: tab.applicationId,
      applicationName: tab.applicationName,
      service: tab.service,
      containerId: tab.containerId,
      containerName: tab.containerName,
      targetLabel: tab.targetLabel,
      sourcePath: route.fullPath,
      ...(fresh ? { fresh: true } : {}),
    };
  }

  /** optionsForTab reconstructs the active tab's open options. */
  function optionsForTab(fresh: boolean): TerminalOpenOptions | null {
    const tab = activeTab.value;
    return tab === null ? null : optionsFor(tab, fresh);
  }

  /**
   * startSession is the explicit restart/transfer action for the active tab.
   * It never runs on mount and never replaces a live session on its own.
   */
  async function startSession(fresh = false): Promise<void> {
    const options = optionsForTab(fresh);
    if (options === null) {
      return;
    }
    starting.value = true;
    try {
      const opened = await terminal.openPageSession(options);
      terminal.activatePage(opened.id);
    } finally {
      starting.value = false;
    }
  }

  /**
   * selectServer starts (or reuses) a host page session from the empty state.
   * The picker uses the same store entry point, so both paths share the
   * single-flight key and the authorization boundary.
   */
  async function selectServer(serverId: string): Promise<void> {
    const server = await resolveServer(serverId);
    if (server === null || server.id === "") {
      if (serverId !== "") {
        unknownServerId.value = serverId;
        selectedId.value = serverId;
      }
      return;
    }
    unknownServerId.value = "";
    selectedId.value = server.id;
    const existing = terminal.pageTabs.find(
      (tab) => tab.serverId === server.id && tab.target === "host",
    );
    if (existing) {
      terminal.activatePage(existing.id);
      return;
    }
    if (server.status !== "ready") {
      return;
    }
    starting.value = true;
    try {
      const tab = await terminal.openPageSession({
        serverId: server.id,
        serverName: server.name,
        sourcePath: sourcePathFor(server.id),
      });
      terminal.activatePage(tab.id);
    } finally {
      starting.value = false;
    }
  }

  /**
   * detachedTab is the moved session this page still owns: either the tab the
   * transfer just detached, or (after a remount/back navigation) the window
   * tab that records this exact route as its source.
   */
  const detachedTab = computed<TerminalTab | null>(() => {
    if (detachedId.value !== "") {
      const tracked = terminal.findTab(detachedId.value);
      if (tracked?.mode === "window") {
        return tracked;
      }
    }
    return (
      terminal.tabs.find(
        (tab) => tab.mode === "window" && tab.sourcePath === route.fullPath,
      ) ?? null
    );
  });

  /** closedIsContainer keeps the restart card's mode truthful. */
  const closedIsContainer = computed<boolean>(() =>
    isContainerTarget(lastClosed.value?.target ?? ""),
  );

  /**
   * restartClosed re-opens the target of the last closed page tab, only after
   * the explicit Start new session button.
   */
  async function restartClosed(): Promise<void> {
    const options = lastClosed.value;
    if (options === null) {
      return;
    }
    starting.value = true;
    try {
      const opened = await terminal.openPageSession({ ...options, fresh: true });
      closed.value = false;
      lastClosed.value = null;
      terminal.activatePage(opened.id);
    } finally {
      starting.value = false;
    }
  }

  /** restartDetached opens a new page session for the moved target. */
  async function restartDetached(): Promise<void> {
    const tab = terminal.findTab(detachedId.value);
    if (tab === null) {
      return;
    }
    starting.value = true;
    try {
      const opened = await terminal.openPageSession(optionsFor(tab, true));
      detachedId.value = "";
      terminal.activatePage(opened.id);
    } finally {
      starting.value = false;
    }
  }

  /** closeDetached closes the moved session through the shared dialog. */
  function closeDetached(): void {
    const tab = terminal.findTab(detachedId.value);
    if (tab !== null) {
      void terminal.requestClose([tab.id]);
    }
  }

  /** syncQuery mirrors the active tab's server into the URL for resume. */
  function syncQuery(): void {
    const next = selectedId.value;
    const current =
      typeof route.query.server === "string" ? route.query.server : "";
    if (current === next) {
      return;
    }
    syncingQuery = true;
    const query = { ...route.query };
    if (next === "") {
      delete query.server;
    } else {
      query.server = next;
    }
    void router.replace({ name: "terminal", query }).finally(() => {
      syncingQuery = false;
    });
  }

  // The active tab owns the server selection label; a detached/closed tab
  // falls back to the next page tab (or the empty picker) through the store.
  watch(
    () => activeTab.value?.id,
    () => {
      // Keep the last server while the empty/closed card is showing, so its
      // restart copy can still name the node the session belonged to.
      if (activeTab.value !== null) {
        selectedId.value = activeTab.value.serverId;
      }
    },
  );

  watch(selectedId, syncQuery);

  // Track what the page should show when its last tab goes away: a session
  // moved into the window keeps the one-way notice, while a closed tab keeps
  // the explicit restart card. Neither path starts a shell by itself.
  watch(activeTab, (next, previous) => {
    if (next !== null) {
      closed.value = false;
      detachedId.value = "";
      lastClosed.value = null;
      unknownServerId.value = "";
      return;
    }
    if (previous === null) {
      return;
    }
    const existing = terminal.findTab(previous.id);
    if (existing === null) {
      closed.value = true;
      detachedId.value = "";
      lastClosed.value = optionsFor(previous, false);
      return;
    }
    if (existing.mode === "window") {
      closed.value = false;
      detachedId.value = existing.id;
    }
  });

  // A node that comes online while it is the selected empty-state server
  // starts the shell the operator already asked for. After a close the user
  // must press the button.
  watch(
    () => selectedServer.value?.status,
    (status) => {
      if (
        status === "ready" &&
        selectedId.value !== "" &&
        pageTabs.value.length === 0 &&
        !closed.value &&
        detachedId.value === "" &&
        !starting.value
      ) {
        void selectServer(selectedId.value);
      }
    },
  );

  // The URL is the resume contract (dock, bookmarks, back/forward).
  watch(
    () => route.query.server,
    (value) => {
      if (syncingQuery) {
        return;
      }
      const next = typeof value === "string" ? value : "";
      if (next === "" || next === selectedId.value) {
        return;
      }
      const existing = terminal.pageTabs.find(
        (tab) => tab.serverId === next && tab.target === "host",
      );
      if (existing) {
        terminal.activatePage(existing.id);
        return;
      }
      void selectServer(next);
    },
  );

  // Demotion is a backend-enforced boundary; the client leaves the operator
  // surface immediately with the same forbidden page the route guard uses.
  const demoted = computed<boolean>(
    () => authStore.isAuthenticated && !authStore.isPlatformAdmin,
  );
  watch(demoted, (isDemoted) => {
    if (isDemoted) {
      void router.replace({ name: "forbidden" });
    }
  });

  onMounted(async () => {
    if (!authStore.isAuthenticated) {
      // The route guard owns the sign-in redirect.
      return;
    }
    if (!authStore.isPlatformAdmin) {
      void router.replace({ name: "forbidden" });
      return;
    }
    await fetchServers();
    const queryId =
      typeof route.query.server === "string" ? route.query.server : "";
    if (queryId !== "") {
      const existing = terminal.pageTabs.find(
        (tab) => tab.serverId === queryId && tab.target === "host",
      );
      if (existing) {
        terminal.activatePage(existing.id);
        selectedId.value = existing.serverId;
      } else {
        await selectServer(queryId);
      }
      return;
    }
    // Re-entering the menu with live page sessions resumes the newest one; the
    // store reuses the shell, so no second create is issued.
    const last = terminal.pageActiveTab;
    if (last !== null) {
      terminal.activatePage(last.id);
      selectedId.value = last.serverId;
    }
  });

  return {
    servers,
    pageTabs,
    selectedId,
    selectedServer,
    serverReady,
    selectedStatusLabel,
    tabId,
    activeTab,
    starting,
    closed,
    closedIsContainer,
    detachedId,
    detachedTab,
    unknownServerId,
    startSession,
    restartClosed,
    restartDetached,
    closeDetached,
    selectServer,
    activateTab,
  };
}
