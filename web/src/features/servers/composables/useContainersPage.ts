
import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { ComputedRef } from "vue";
import { useI18n } from "vue-i18n";

import type { Container, ContainerAction } from "@/features/servers/api/containers";
import {
  describeContainerError,
  listContainers,
  restartContainer,
  startContainer,
  stopContainer,
} from "@/features/servers/api/containers";
import { failureText, getServer } from "@/features/servers/api/servers";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import {
  countByFilter,
  isRunning,
  matchesFilter,
} from "@/features/servers/utils/containerView";
import type { ContainerFilter } from "@/features/servers/utils/containerView";

const pollIntervalMs = 5_000;

export function useContainersPage(serverId: ComputedRef<string>) {
  const message = useMessage();
  const { locale, t } = useI18n();

  const containers = ref<Container[]>([]);
  const loading = ref(false);
  const fetchFailure = ref<unknown>(null);
  const error = computed<string | null>(() =>
    fetchFailure.value === null || fetchFailure.value === undefined
      ? null
      : failureText(fetchFailure.value, locale.value),
  );
  const serverName = ref<string>("");
  const loaded = ref(false);

  const pending = ref<Record<string, boolean>>({});

  const selected = ref<Container | null>(null);
  const drawerOpen = ref(false);

  const activeFilter = ref<ContainerFilter>("all");
  const searchQuery = ref("");

  const filteredContainers = computed<Container[]>(() => {
    const needle = searchQuery.value.trim().toLowerCase();
    return containers.value.filter((row) => {
      if (!matchesFilter(row, activeFilter.value)) {
        return false;
      }
      if (needle === "") {
        return true;
      }
      return `${row.name} ${row.image}`.toLowerCase().includes(needle);
    });
  });

  const filterCounts = computed<Record<ContainerFilter, number>>(() => ({
    all: countByFilter(containers.value, "all"),
    running: countByFilter(containers.value, "running"),
    exited: countByFilter(containers.value, "exited"),
  }));

  const isNarrow = useMediaQuery("(max-width: 760px)");

  const drawerWidth = computed<number | string>(() =>
    isNarrow.value ? "94vw" : 720,
  );

  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function isPending(containerId: string, action: ContainerAction): boolean {
    return pending.value[`${containerId}:${action}`] === true;
  }

  function isBusy(containerId: string): boolean {
    return Object.keys(pending.value).some((key) =>
      key.startsWith(`${containerId}:`),
    );
  }

  function openLogs(row: Container): void {
    selected.value = row;
    drawerOpen.value = true;
  }

  async function fetchContainers(showLoading: boolean): Promise<void> {
    if (!serverId.value) {
      return;
    }
    if (showLoading) {
      loading.value = true;
    }
    try {
      containers.value = await listContainers(serverId.value);
      fetchFailure.value = null;
      loaded.value = true;
    } catch (err) {
      fetchFailure.value = err;
    } finally {
      loading.value = false;
    }
  }

  async function fetchServer(): Promise<void> {
    if (!serverId.value) {
      return;
    }
    try {
      const server = await getServer(serverId.value);
      serverName.value = server.name;
    } catch {
    }
  }

  async function runAction(
    row: Container,
    action: ContainerAction,
    label: string,
  ): Promise<void> {
    const key = `${row.id}:${action}`;
    pending.value = { ...pending.value, [key]: true };
    try {
      if (action === "start") {
        await startContainer(serverId.value, row.id);
      } else if (action === "stop") {
        await stopContainer(serverId.value, row.id);
      } else {
        await restartContainer(serverId.value, row.id);
      }
      message.success(t("servers.containers.actionRequested", { action: label, name: row.name }));
      await fetchContainers(false);
    } catch (err) {
      message.error(describeContainerError(err));
    } finally {
      const next = { ...pending.value };
      delete next[key];
      pending.value = next;
    }
  }

  function startPolling(): void {
    if (pollTimer !== null) {
      return;
    }
    pollTimer = setInterval(() => {
      void fetchContainers(false);
    }, pollIntervalMs);
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function initialize(): Promise<void> {
    loaded.value = false;
    await Promise.all([fetchServer(), fetchContainers(true)]);
  }

  watch(() => serverId.value, () => {
    selected.value = null;
    drawerOpen.value = false;
    void initialize();
  });

  onMounted(() => {
    void initialize();
    startPolling();
  });

  onUnmounted(() => {
    stopPolling();
  });

  return {
    containers,
    loading,
    error,
    serverName,
    loaded,
    activeFilter,
    searchQuery,
    filteredContainers,
    filterCounts,
    drawerWidth,
    selected,
    drawerOpen,
    isRunning,
    isPending,
    isBusy,
    openLogs,
    fetchContainers,
    runAction,
  };
}
