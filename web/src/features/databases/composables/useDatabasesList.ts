import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";

import {
  describeDatabaseError,
} from "@/features/databases/api/databases";
import type { Database } from "@/features/databases/api/databases";
import {
  engineBreakdown,
  filterCounts,
  matchesFilter,
  matchesSearch,
} from "@/features/databases/utils/databaseFilters";
import type { DatabaseFilter } from "@/features/databases/utils/databaseFilters";
import { useDatabasesStore } from "@/features/databases/stores/databases";
import { useServersStore } from "@/features/servers";

/**
 * useDatabasesList owns the databases list page state: filter chips, search,
 * derived counts, row actions and the list polling lifecycle. The page and
 * its table share one instance owned by the page.
 */
export function useDatabasesList() {
  const router = useRouter();
  const message = useMessage();
  const databasesStore = useDatabasesStore();
  const serversStore = useServersStore();

  const wizardOpen = ref(false);
  const restartingId = ref<string | null>(null);
  const activeFilter = ref<DatabaseFilter>("all");
  const searchQuery = ref("");

  /** serverName resolves a node id to its display name. */
  function serverName(serverId: string): string {
    return (
      serversStore.servers.find((server) => server.id === serverId)?.name ??
      serverId.slice(0, 8)
    );
  }

  /** filteredDatabases applies the chip filter and the search query. */
  const filteredDatabases = computed<Database[]>(() =>
    databasesStore.databases.filter(
      (item) => matchesFilter(item, activeFilter.value) && matchesSearch(item, searchQuery.value, serverName),
    ),
  );

  const counts = computed<Record<DatabaseFilter, number>>(() =>
    filterCounts(databasesStore.databases),
  );

  const breakdown = computed<string>(() =>
    engineBreakdown(databasesStore.databases),
  );

  /** fetchAll loads the databases and the node names. */
  async function fetchAll(): Promise<void> {
    try {
      await databasesStore.fetchDatabases();
    } catch {
      // The store already exposes the error; the alert renders it.
    }
    void serversStore.fetchServers().catch(() => undefined);
  }

  /** handleDelete removes one database, surfacing backend errors honestly. */
  async function handleDelete(database: Database): Promise<void> {
    try {
      await databasesStore.remove(database.id);
      message.success(
        `Database "${database.name}" deleted · volume kept for 7 days`,
      );
    } catch (error) {
      message.error(describeDatabaseError(error));
    }
  }

  /** handleRestart re-runs one database container in place. */
  async function handleRestart(database: Database): Promise<void> {
    restartingId.value = database.id;
    try {
      await databasesStore.restart(database.id);
      message.success(`Database "${database.name}" restarting`);
    } catch (error) {
      message.error(describeDatabaseError(error));
    } finally {
      restartingId.value = null;
    }
  }

  /** openDetails navigates to one database's detail page. */
  function openDetails(database: Database): void {
    void router.push({
      name: "database-detail",
      params: { id: database.id },
    });
  }

  onMounted(() => {
    void fetchAll();
    databasesStore.pollDatabases();
  });

  onUnmounted(() => {
    databasesStore.stopPolling();
  });

  return {
    databasesStore,
    serversStore,
    wizardOpen,
    restartingId,
    activeFilter,
    searchQuery,
    filteredDatabases,
    counts,
    breakdown,
    serverName,
    fetchAll,
    handleDelete,
    handleRestart,
    openDetails,
  };
}

export type DatabasesList = ReturnType<typeof useDatabasesList>;
