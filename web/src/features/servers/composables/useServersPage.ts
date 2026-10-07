// List-page state for ServersPage: filtering, pagination and row actions.
//
// The page component keeps route concerns (deep-link ?add=1), layout and modal
// wiring; everything else lives here so the page stays a thin composer.

import { useMessage } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { describeServerError } from "@/features/servers/api/servers";
import type { Server } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import {
  matchesFilter,
  matchesSearch,
  needsAgentUpdate,
} from "@/features/servers/utils/serverListView";
import type { ServerFilter } from "@/features/servers/utils/serverListView";

/**
 * Page size bounds the mounted cards. The store replaces the whole list every
 * five seconds, so rendering every node (three progress bars each) would make
 * the update cost grow without limit; the mockup has no pagination, so the
 * control stays a single row under the grid.
 */
export const serversPageSize = 12;

export function useServersPage() {
  const router = useRouter();
  const route = useRoute();
  const serversStore = useServersStore();
  const message = useMessage();
  const { t } = useI18n();

  const wizardOpen = ref(false);
  const editOpen = ref(false);
  const editTarget = ref<Server | null>(null);
  const validatingId = ref<string | null>(null);
  const checkingAll = ref(false);

  const activeFilter = ref<ServerFilter>("all");
  const searchQuery = ref("");

  const readyCount = computed<number>(
    () => serversStore.servers.filter((server) => server.status === "ready").length,
  );
  const offlineCount = computed<number>(
    () => serversStore.servers.filter((server) => server.status === "offline").length,
  );
  const updateCount = computed<number>(
    () => serversStore.servers.filter((server) => needsAgentUpdate(server)).length,
  );

  /** filteredServers applies the chip filter and the search query client-side. */
  const filteredServers = computed<Server[]>(() =>
    serversStore.servers.filter(
      (server) =>
        matchesFilter(server, activeFilter.value) &&
        matchesSearch(server, searchQuery.value),
    ),
  );

  const page = ref(1);

  /** pageCount is the last page the filtered list has. */
  const pageCount = computed<number>(() =>
    Math.max(1, Math.ceil(filteredServers.value.length / serversPageSize)),
  );

  /**
   * currentPage clamps the requested page to the available range: deleting the
   * only node on the last page (or the five-second refresh shrinking the list)
   * would otherwise leave an empty slice and hide a control that could return
   * the operator to the data.
   */
  const currentPage = computed<number>(() => Math.min(page.value, pageCount.value));

  /** pagedServers is the visible slice of the filtered list. */
  const pagedServers = computed<Server[]>(() =>
    filteredServers.value.slice(
      (currentPage.value - 1) * serversPageSize,
      currentPage.value * serversPageSize,
    ),
  );

  // A filter or search change re-enters at the first page.
  watch([activeFilter, searchQuery], () => {
    page.value = 1;
  });

  // Follow the list down when it shrinks under the current page.
  watch(pageCount, (count) => {
    if (page.value > count) {
      page.value = count;
    }
  });

  /** handleValidate probes one server and reports the outcome. */
  async function handleValidate(server: Server): Promise<void> {
    validatingId.value = server.id;
    try {
      const outcome = await serversStore.validate(server.id);
      if (outcome.ok) {
        message.success(t("servers.toasts.validationPassed", { name: server.name }));
        return;
      }
      const failed = outcome.checks
        .filter((check) => !check.ok)
        .map((check) => check.name)
        .join(", ");
      message.error(
        outcome.message || t("servers.toasts.validationFailed", { name: server.name, failed }),
      );
    } catch (error) {
      message.error(describeServerError(error));
    } finally {
      validatingId.value = null;
    }
  }

  /**
   * handleCheckAll re-runs the SSH probes for every registered server using the
   * existing per-server validate flow, then reports a summary.
   */
  async function handleCheckAll(): Promise<void> {
    if (checkingAll.value || serversStore.servers.length === 0) {
      return;
    }
    checkingAll.value = true;
    try {
      const results = await Promise.allSettled(
        serversStore.servers.map((server) => serversStore.validate(server.id)),
      );
      const failed = results.filter(
        (result) => result.status === "rejected" || !result.value.ok,
      ).length;
      const total = results.length;
      if (failed === 0) {
        const key =
          total === 1 ? "servers.toasts.sshAllPassedOne" : "servers.toasts.sshAllPassedOther";
        message.success(t(key, { total }));
      } else {
        message.error(t("servers.toasts.sshSomeFailed", { failed, total }));
      }
    } finally {
      checkingAll.value = false;
    }
  }

  /** openNode navigates to the server-detail route for one server. */
  function openNode(id: string): Promise<void> {
    return router.push({ name: "server-detail", params: { id } }).then(() => undefined);
  }

  /** openContainers navigates to the server-containers route for one server. */
  function openContainers(id: string): Promise<void> {
    return router.push({ name: "server-containers", params: { id } }).then(() => undefined);
  }

  /** openEdit opens the edit modal prefilled from one server. */
  function openEdit(server: Server): void {
    editTarget.value = server;
    editOpen.value = true;
  }

  /** handleUpdated refreshes the edited row in place. */
  function handleUpdated(updated: Server): void {
    message.success(t("servers.toasts.saved", { name: updated.name }));
  }

  /** handleDelete removes one server after the popconfirm is accepted. */
  async function handleDelete(server: Server): Promise<void> {
    try {
      await serversStore.removeServer(server.id);
      message.success(t("servers.toasts.deleted", { name: server.name }));
    } catch (error) {
      message.error(describeServerError(error));
    }
  }

  onMounted(() => {
    void serversStore.fetchServers().catch(() => {
      // The store already exposes the error; message rendering is enough here.
    });
    serversStore.pollServers();
  });

  // The dashboard's "Add server" CTA deep-links here with ?add=1. Open the wizard
  // once and strip the flag with a replace, so a refresh or a back navigation
  // does not reopen it (B4-3).
  watch(
    () => route.query.add,
    (value) => {
      if (value === "1") {
        wizardOpen.value = true;
        void router.replace({ name: "servers" });
      }
    },
    { immediate: true },
  );

  return {
    serversStore,
    wizardOpen,
    editOpen,
    editTarget,
    validatingId,
    checkingAll,
    activeFilter,
    searchQuery,
    readyCount,
    offlineCount,
    updateCount,
    filteredServers,
    page,
    pageCount,
    currentPage,
    pagedServers,
    handleValidate,
    handleCheckAll,
    openNode,
    openContainers,
    openEdit,
    handleUpdated,
    handleDelete,
  };
}
