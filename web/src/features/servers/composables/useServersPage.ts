
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

  const filteredServers = computed<Server[]>(() =>
    serversStore.servers.filter(
      (server) =>
        matchesFilter(server, activeFilter.value) &&
        matchesSearch(server, searchQuery.value),
    ),
  );

  const page = ref(1);

  const pageCount = computed<number>(() =>
    Math.max(1, Math.ceil(filteredServers.value.length / serversPageSize)),
  );

  const currentPage = computed<number>(() => Math.min(page.value, pageCount.value));

  const pagedServers = computed<Server[]>(() =>
    filteredServers.value.slice(
      (currentPage.value - 1) * serversPageSize,
      currentPage.value * serversPageSize,
    ),
  );

  watch([activeFilter, searchQuery], () => {
    page.value = 1;
  });

  watch(pageCount, (count) => {
    if (page.value > count) {
      page.value = count;
    }
  });

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

  function openNode(id: string): Promise<void> {
    return router.push({ name: "server-detail", params: { id } }).then(() => undefined);
  }

  function openContainers(id: string): Promise<void> {
    return router.push({ name: "server-containers", params: { id } }).then(() => undefined);
  }

  function openEdit(server: Server): void {
    editTarget.value = server;
    editOpen.value = true;
  }

  function handleUpdated(updated: Server): void {
    message.success(t("servers.toasts.saved", { name: updated.name }));
  }

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
    });
    serversStore.pollServers();
  });

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
