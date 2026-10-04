// List state for ContainersPage: loading, filtering, polling and row actions.
//
// The page keeps layout (breadcrumb, card, drawer); the table owns its
// columns and the toolbar owns its chips.

import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { ComputedRef } from "vue";

import type { Container, ContainerAction } from "@/features/servers/api/containers";
import {
  describeContainerError,
  listContainers,
  restartContainer,
  startContainer,
  stopContainer,
} from "@/features/servers/api/containers";
import { getServer } from "@/features/servers/api/servers";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import {
  countByFilter,
  isRunning,
  matchesFilter,
} from "@/features/servers/utils/containerView";
import type { ContainerFilter } from "@/features/servers/utils/containerView";

/** Polling cadence for the container list, in milliseconds. */
const pollIntervalMs = 5_000;

export function useContainersPage(serverId: ComputedRef<string>) {
  const message = useMessage();

  const containers = ref<Container[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);
  const serverName = ref<string>("");
  const loaded = ref(false);

  /** Per-action in-flight markers keyed by `${containerId}:${action}`. */
  const pending = ref<Record<string, boolean>>({});

  const selected = ref<Container | null>(null);
  const drawerOpen = ref(false);

  const activeFilter = ref<ContainerFilter>("all");
  const searchQuery = ref("");

  /** filteredContainers applies the chip filter and the name/image search. */
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

  /** filterCounts renders live chip counts; never invented. */
  const filterCounts = computed<Record<ContainerFilter, number>>(() => ({
    all: countByFilter(containers.value, "all"),
    running: countByFilter(containers.value, "running"),
    exited: countByFilter(containers.value, "exited"),
  }));

  /** isNarrow tracks viewports where the fixed log drawer would overflow. */
  const isNarrow = useMediaQuery("(max-width: 760px)");

  /** drawerWidth keeps the log drawer inside narrow viewports. */
  const drawerWidth = computed<number | string>(() =>
    isNarrow.value ? "94vw" : 720,
  );

  let pollTimer: ReturnType<typeof setInterval> | null = null;

  /** isPending reports whether one action is in flight for a container. */
  function isPending(containerId: string, action: ContainerAction): boolean {
    return pending.value[`${containerId}:${action}`] === true;
  }

  /** isBusy reports whether any action is in flight for a container. */
  function isBusy(containerId: string): boolean {
    return Object.keys(pending.value).some((key) =>
      key.startsWith(`${containerId}:`),
    );
  }

  /** openLogs selects a container and opens the log drawer. */
  function openLogs(row: Container): void {
    selected.value = row;
    drawerOpen.value = true;
  }

  /** fetchContainers reloads the list; `showLoading` drives the table spinner. */
  async function fetchContainers(showLoading: boolean): Promise<void> {
    if (!serverId.value) {
      return;
    }
    if (showLoading) {
      loading.value = true;
    }
    try {
      containers.value = await listContainers(serverId.value);
      error.value = null;
      loaded.value = true;
    } catch (err) {
      error.value = describeContainerError(err);
    } finally {
      loading.value = false;
    }
  }

  /** fetchServer loads the server name for the heading; failures are ignored. */
  async function fetchServer(): Promise<void> {
    if (!serverId.value) {
      return;
    }
    try {
      const server = await getServer(serverId.value);
      serverName.value = server.name;
    } catch {
      // The breadcrumb falls back to the server id when the lookup fails.
    }
  }

  /** runAction invokes one lifecycle action and refreshes the list on success. */
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
      message.success(`${label} requested for ${row.name}`);
      await fetchContainers(false);
    } catch (err) {
      message.error(describeContainerError(err));
    } finally {
      const next = { ...pending.value };
      delete next[key];
      pending.value = next;
    }
  }

  /** startPolling refreshes the container list in place every few seconds. */
  function startPolling(): void {
    if (pollTimer !== null) {
      return;
    }
    pollTimer = setInterval(() => {
      void fetchContainers(false);
    }, pollIntervalMs);
  }

  /** stopPolling clears the refresh interval. */
  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  /** initialize loads the server name and the container list. */
  async function initialize(): Promise<void> {
    loaded.value = false;
    await Promise.all([fetchServer(), fetchContainers(true)]);
  }

  // The page derives serverId from the route; a change resets the drawer.
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
