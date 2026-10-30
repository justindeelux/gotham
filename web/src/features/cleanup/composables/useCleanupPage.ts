import { computed, onMounted, ref, watch } from "vue";

import { createExclusion } from "@/features/cleanup/api/center";
import {
  cleanNode,
  scanNode,
  type CleanupItem,
  type CleanupNodeScan,
  type CleanupNotice,
  type CleanupRunItem,
} from "@/features/cleanup/api/cleanup";
import {
  defaultSelection,
  itemKey,
  selectionSummary,
  setCategorySelection,
  toggleItem,
  type SelectableItem,
} from "@/features/cleanup/utils/selection";
import { failureText } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers/stores/servers";

export type CleanupPhase = "select" | "scanning" | "results" | "cleaning" | "done";
export type ScanNodeStatus = "pending" | "scanning" | "done" | "skipped" | "failed";

export interface ScanNodeState {
  serverId: string;
  status: ScanNodeStatus;
  reason: string;
  items: CleanupItem[];
  notices: CleanupNotice[];
}

export interface RunNodeState {
  serverId: string;
  status: string;
  reason: string;
  reclaimedBytes: number;
  items: CleanupRunItem[];
  error: string;
}

function toNodeState(id: string): ScanNodeState {
  return { serverId: id, status: "pending", reason: "", items: [], notices: [] };
}

function fromNodeScan(scan: CleanupNodeScan): ScanNodeState {
  const status: ScanNodeStatus =
    scan.status === "ok" ? "done" : scan.status === "skipped" ? "skipped" : "failed";
  return {
    serverId: scan.server_id,
    status,
    reason: scan.reason,
    items: scan.items,
    notices: scan.notices,
  };
}

export function useCleanupPage() {
  const serversStore = useServersStore();

  const phase = ref<CleanupPhase>("select");
  const selectedServerIds = ref<Set<string>>(new Set());
  const minAgeValue = ref(24);
  const minAgeUnit = ref<"hours" | "days">("hours");
  const scans = ref<ScanNodeState[]>([]);
  const selectedKeys = ref<Set<string>>(new Set());
  const confirmOpen = ref(false);
  const runs = ref<RunNodeState[]>([]);
  const excludeError = ref("");
  const initialized = ref(false);

  const minAgeHours = computed(() =>
    minAgeUnit.value === "days" ? minAgeValue.value * 24 : minAgeValue.value,
  );

  const onlineServers = computed(() =>
    serversStore.servers.filter((server) => server.status === "ready"),
  );

  watch(
    () => serversStore.servers,
    (servers) => {
      if (initialized.value || servers.length === 0) {
        return;
      }
      initialized.value = true;
      selectedServerIds.value = new Set(
        servers.filter((server) => server.status === "ready").map((server) => server.id),
      );
    },
    { immediate: true },
  );

  const selectableItems = computed<SelectableItem[]>(() =>
    scans.value.flatMap((scan) =>
      scan.items.map((item) => ({
        key: itemKey(scan.serverId, item.id),
        id: item.id,
        serverId: scan.serverId,
        category: item.category,
        tier: item.tier,
        name: item.name,
        sizeBytes: item.size_bytes,
        createdAt: item.created_at,
        reason: item.reason,
      })),
    ),
  );

  const selectedItems = computed(() =>
    selectableItems.value.filter((item) => selectedKeys.value.has(item.key)),
  );

  const summary = computed(() =>
    selectionSummary(selectableItems.value, selectedKeys.value),
  );

  const hasDangerous = computed(() =>
    selectedItems.value.some((item) => item.tier === "dangerous"),
  );

  function serverName(serverId: string): string {
    return (
      serversStore.servers.find((server) => server.id === serverId)?.name ?? serverId
    );
  }

  function toggleServer(serverId: string, checked: boolean): void {
    const next = new Set(selectedServerIds.value);
    if (checked) {
      next.add(serverId);
    } else {
      next.delete(serverId);
    }
    selectedServerIds.value = next;
  }

  function toggleItemKey(key: string, checked: boolean): void {
    selectedKeys.value = toggleItem(selectedKeys.value, key, checked);
  }

  async function excludeItem(item: SelectableItem): Promise<void> {
    excludeError.value = "";
    try {
      await createExclusion({
        server_id: item.serverId,
        category: item.category,
        match: item.name,
      });
    } catch (error) {
      excludeError.value = failureText(error);
      return;
    }
    const scan = scans.value.find((state) => state.serverId === item.serverId);
    if (scan) {
      scan.items = scan.items.filter((entry) => entry.id !== item.id);
    }
    const next = new Set(selectedKeys.value);
    next.delete(item.key);
    selectedKeys.value = next;
  }

  function setCategory(category: string, select: boolean): void {
    selectedKeys.value = setCategorySelection(
      selectableItems.value,
      selectedKeys.value,
      category,
      select,
    );
  }

  async function startScan(): Promise<void> {
    if (phase.value === "scanning") {
      return;
    }
    const ids = [...selectedServerIds.value];
    if (ids.length === 0) {
      return;
    }
    phase.value = "scanning";
    runs.value = [];
    scans.value = ids.map(toNodeState);
    await Promise.all(
      scans.value.map(async (state) => {
        state.status = "scanning";
        try {
          const scan = await scanNode(state.serverId, minAgeHours.value);
          Object.assign(state, fromNodeScan(scan));
        } catch (error) {
          state.status = "failed";
          state.reason = failureText(error);
        }
      }),
    );
    selectedKeys.value = defaultSelection(selectableItems.value);
    phase.value = "results";
  }

  function backToSelect(): void {
    phase.value = "select";
    scans.value = [];
    selectedKeys.value = new Set();
    runs.value = [];
  }

  async function startClean(): Promise<void> {
    if (phase.value === "cleaning") {
      return;
    }
    const ids = selectedItems.value;
    if (ids.length === 0) {
      return;
    }
    confirmOpen.value = false;
    phase.value = "cleaning";
    const byServer = new Map<string, string[]>();
    for (const item of ids) {
      const list = byServer.get(item.serverId) ?? [];
      list.push(item.id);
      byServer.set(item.serverId, list);
    }
    runs.value = [];
    await Promise.all(
      [...byServer.entries()].map(async ([serverId, itemIds]) => {
        try {
          const result = await cleanNode(serverId, itemIds, minAgeHours.value);
          runs.value.push({
            serverId,
            status: result.status,
            reason: result.reason,
            reclaimedBytes: result.reclaimed_bytes,
            items: result.items,
            error: "",
          });
        } catch (error) {
          runs.value.push({
            serverId,
            status: "failed",
            reason: "",
            reclaimedBytes: 0,
            items: [],
            error: failureText(error),
          });
        }
      }),
    );
    phase.value = "done";
  }

  const totalFreed = computed(() =>
    runs.value.reduce((sum, run) => sum + run.reclaimedBytes, 0),
  );

  onMounted(() => {
    if (serversStore.servers.length === 0 && !serversStore.loading) {
      void serversStore.fetchServers().catch(() => undefined);
    }
  });

  return {
    phase,
    serversStore,
    onlineServers,
    selectedServerIds,
    minAgeValue,
    minAgeUnit,
    minAgeHours,
    scans,
    selectableItems,
    selectedKeys,
    selectedItems,
    summary,
    hasDangerous,
    confirmOpen,
    runs,
    excludeError,
    totalFreed,
    serverName,
    toggleServer,
    toggleItemKey,
    setCategory,
    startScan,
    backToSelect,
    startClean,
    excludeItem,
  };
}
