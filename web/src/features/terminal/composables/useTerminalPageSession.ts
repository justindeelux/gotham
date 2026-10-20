import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import type { Server } from "@/features/servers";
import { useServersStore } from "@/features/servers";
import { useTerminalStore } from "@/features/terminal/stores/terminal";

/**
 * Selection and single-flight session lifecycle for the standalone terminal
 * page (JUS-246).
 *
 * The operator picks an authorized server first; only then is a page-mode
 * session requested. `openPageSession` is idempotent and single-flight, so a
 * revisiting route (or the app dock's resume action) reuses the live session.
 * Switching servers runs the shared confirmation before the old page shell is
 * released. When the selected tab disappears (closed/removed) the composable
 * forgets its id and exposes an explicit restart state; it never creates a
 * replacement on its own.
 */
export function useTerminalPageSession() {
  const { t } = useI18n();
  const route = useRoute();
  const router = useRouter();
  const authStore = useAuthStore();
  const serversStore = useServersStore();
  const terminal = useTerminalStore();

  const selectedId = ref<string>("");
  const tabId = ref<string>("");
  const starting = ref(false);
  /** closed marks a tab that vanished under a mounted page/tab host. */
  const closed = ref(false);
  /** syncingQuery guards the route → selection echo while we write the URL. */
  let syncingQuery = false;
  /** selectionToken discards a dialog result superseded by a newer pick. */
  let selectionToken = 0;

  const servers = computed<Server[]>(() => serversStore.servers);
  const selectedServer = computed<Server | null>(
    () => servers.value.find((server) => server.id === selectedId.value) ?? null,
  );
  const serverReady = computed<boolean>(
    () => selectedServer.value?.status === "ready",
  );
  const activeTab = computed(() =>
    tabId.value === "" ? null : terminal.findTab(tabId.value),
  );

  /** serverOptions label each node with its live status for the switcher. */
  const serverOptions = computed(() =>
    servers.value.map((server) => ({
      label:
        server.status === "ready"
          ? server.name
          : `${server.name} · ${t(`servers.status.${server.status}`)}`,
      value: server.id,
    })),
  );

  /** sourcePath is the route the app dock resumes for this page session. */
  function sourcePathFor(serverId: string): string {
    return router.resolve({ name: "terminal", query: { server: serverId } })
      .fullPath;
  }

  /** statusLabel localizes the server lifecycle word. */
  function statusLabel(server: Server): string {
    return t(`servers.status.${server.status}`);
  }

  /** syncQuery mirrors the selection into the URL for reload/dock resume. */
  function syncQuery(): void {
    const current =
      typeof route.query.server === "string" ? route.query.server : "";
    if (current === selectedId.value) {
      return;
    }
    syncingQuery = true;
    const query = { ...route.query };
    if (selectedId.value === "") {
      delete query.server;
    } else {
      query.server = selectedId.value;
    }
    void router
      .replace({ name: "terminal", query })
      .finally(() => {
        syncingQuery = false;
      });
  }

  /**
   * startSession creates or reuses the shell. It is the only creation path;
   * `fresh` is the explicit "start a new session" action from a closed pane or
   * a moved-to-window notice.
   */
  async function startSession(fresh = false): Promise<void> {
    const server = selectedServer.value;
    if (!server || server.id === "" || server.status !== "ready") {
      tabId.value = "";
      return;
    }
    closed.value = false;
    const request = {
      serverId: server.id,
      serverName: server.name,
      sourcePath: sourcePathFor(server.id),
    };
    if (!fresh) {
      const existing = terminal.findTabForTarget(request);
      if (existing) {
        tabId.value = existing.id;
        return;
      }
    }
    starting.value = true;
    try {
      const tab = await terminal.openPageSession(
        fresh ? { ...request, fresh: true } : request,
      );
      if (selectedId.value === server.id) {
        tabId.value = tab.id;
      }
    } finally {
      starting.value = false;
    }
  }

  /**
   * selectServer runs the confirmed close lifecycle when the old page shell
   * would otherwise be stranded: cancel keeps it and restores the switcher,
   * confirm releases exactly that session before the next one starts.
   */
  async function selectServer(nextId: string): Promise<void> {
    if (nextId === "" || nextId === selectedId.value) {
      return;
    }
    const token = ++selectionToken;
    const previous = selectedId.value;
    const previousTab = previous
      ? terminal.pageTabs.find((tab) => tab.serverId === previous)
      : undefined;
    if (previousTab && previousTab.mode === "page") {
      const confirmed = await terminal.requestClose([previousTab.id]);
      if (!confirmed || token !== selectionToken) {
        selectedId.value = previous;
        return;
      }
    }
    if (selectedId.value !== previous) {
      return;
    }
    selectedId.value = nextId;
    tabId.value = "";
    closed.value = false;
    syncQuery();
    await startSession();
  }

  /** onServerSelect adapts Naive's value type to the selection flow. */
  function onServerSelect(value: string | number | null): void {
    void selectServer(String(value ?? ""));
  }

  // The URL is the resume contract (dock, bookmarks, back/forward).
  watch(
    () => route.query.server,
    (value) => {
      if (syncingQuery) {
        return;
      }
      const next = typeof value === "string" ? value : "";
      if (next === selectedId.value) {
        return;
      }
      if (next === "") {
        selectedId.value = "";
        tabId.value = "";
        closed.value = false;
        return;
      }
      void selectServer(next);
    },
  );

  // A tab removed by any route (close confirmation, reset) must not leave the
  // host pointing at a dead id. This only forgets the id; the stage renders an
  // explicit restart button, never an automatic create.
  watch(
    () => (tabId.value === "" ? undefined : terminal.findTab(tabId.value)),
    (tab) => {
      if (tabId.value !== "" && tab === null) {
        tabId.value = "";
        starting.value = false;
        closed.value = true;
      }
    },
  );

  // A node that comes online while its server is selected starts the shell the
  // operator already asked for. After a close the user must press the button.
  watch(
    () => selectedServer.value?.status,
    (status) => {
      if (
        status === "ready" &&
        selectedId.value !== "" &&
        tabId.value === "" &&
        !closed.value &&
        !starting.value
      ) {
        void startSession();
      }
    },
  );

  // Demotion is a backend-enforced boundary; the client leaves the operator
  // surface immediately with the same forbidden page the route guard uses.
  watch(
    () => authStore.isPlatformAdmin,
    (isAdmin) => {
      if (!isAdmin) {
        void router.replace({ name: "forbidden" });
      }
    },
  );

  onMounted(async () => {
    if (!authStore.isPlatformAdmin) {
      void router.replace({ name: "forbidden" });
      return;
    }
    if (servers.value.length === 0 && !serversStore.loading) {
      await serversStore.fetchServers().catch(() => undefined);
    }
    const queryId =
      typeof route.query.server === "string" ? route.query.server : "";
    if (queryId !== "") {
      selectedId.value = queryId;
      await startSession();
      return;
    }
    // Re-entering the menu with exactly one live page session resumes it; the
    // store reuses the shell, so no second create is issued.
    const pageTabs = terminal.pageTabs;
    if (pageTabs.length === 1) {
      selectedId.value = pageTabs[0]!.serverId;
      syncQuery();
      await startSession();
    }
  });

  return {
    servers,
    selectedId,
    selectedServer,
    serverReady,
    serverOptions,
    statusLabel,
    tabId,
    starting,
    closed,
    activeTab,
    startSession,
    selectServer,
    onServerSelect,
  };
}
