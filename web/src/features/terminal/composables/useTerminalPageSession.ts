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

export function useTerminalPageSession() {
  const { t } = useI18n();
  const route = useRoute();
  const router = useRouter();
  const authStore = useAuthStore();
  const serversStore = useServersStore();
  const terminal = useTerminalStore();

  const selectedId = ref<string>(terminal.pageActiveTab?.serverId ?? "");
  const starting = ref(false);
  const closed = ref(false);
  const detachedId = ref<string>("");
  const unknownServerId = ref<string>("");
  const lastClosed = ref<TerminalOpenOptions | null>(null);
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

  const selectedStatusLabel = computed<string>(() =>
    selectedServer.value
      ? t(serverStatusKey(selectedServer.value.status))
      : "",
  );

  function sourcePathFor(serverId: string): string {
    return router.resolve({
      name: "terminal",
      query: { ...route.query, server: serverId },
    }).fullPath;
  }

  async function fetchServers(): Promise<void> {
    if (servers.value.length === 0 && !serversStore.loading) {
      await serversStore.fetchServers().catch(() => undefined);
    }
  }

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

  function activateTab(id: string): void {
    terminal.activatePage(id);
  }

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

  function optionsForTab(fresh: boolean): TerminalOpenOptions | null {
    const tab = activeTab.value;
    return tab === null ? null : optionsFor(tab, fresh);
  }

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

  const closedIsContainer = computed<boolean>(() =>
    isContainerTarget(lastClosed.value?.target ?? ""),
  );

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

  function closeDetached(): void {
    const tab = terminal.findTab(detachedId.value);
    if (tab !== null) {
      void terminal.requestClose([tab.id]);
    }
  }

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

  watch(
    () => activeTab.value?.id,
    () => {
      if (activeTab.value !== null) {
        selectedId.value = activeTab.value.serverId;
      }
    },
  );

  watch(selectedId, syncQuery);

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
