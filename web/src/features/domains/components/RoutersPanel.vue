<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NInput,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted } from "vue";
import type { VNode } from "vue";

import { proxyText } from "@/features/domains/api/proxy";
import type { ProxyRouter } from "@/features/domains/api/proxy";
import { useRouters } from "@/features/domains/composables/useRouters";
import { useProxyStore } from "@/features/domains/stores/proxy";

const proxyStore = useProxyStore();
const { query, filteredRouters, failedNodes, refresh } = useRouters();

onMounted(() => {
  if (proxyStore.routers.length === 0 && !proxyStore.routersLoading) {
    void proxyStore.fetchRouters().catch(() => undefined);
  }
});

/** kindLabel renders the owning-resource kind in the current locale. */
function kindLabel(kind: ProxyRouter["kind"]): string {
  switch (kind) {
    case "service":
      return proxyText("domains.routers.kindService", "Service");
    case "redirect":
      return proxyText("domains.routers.kindRedirect", "Redirect");
    default:
      return proxyText("domains.routers.kindApplication", "Application");
  }
}

/** syncLabel renders one node's sync status; unknown is never a success. */
function syncLabel(status: string): string {
  switch (status) {
    case "synced":
      return proxyText("domains.routers.syncSynced", "synced");
    case "pending":
      return proxyText("domains.routers.syncPending", "sync pending");
    default:
      return proxyText("domains.routers.syncUnknown", "sync unknown");
  }
}

/** syncTagType maps the node sync status onto a tag style. */
function syncTagType(status: string): "success" | "warning" | "default" {
  switch (status) {
    case "synced":
      return "success";
    case "pending":
      return "warning";
    default:
      return "default";
  }
}

/** nodeSyncOf resolves the sync status of the router's node, if reported. */
function nodeSyncOf(router: ProxyRouter): string {
  return (
    proxyStore.routerNodes.find((node) => node.server_id === router.server_id)
      ?.sync_status ?? "unknown"
  );
}

/** hostCell renders the host with its backend target. */
function hostCell(router: ProxyRouter): VNode {
  return h("div", { class: "cell-main" }, [
    h("span", { class: "mono cell-name" }, router.host),
    router.target
      ? h("span", { class: "cell-sub" }, `→ ${router.target}`)
      : null,
  ]);
}

/** sourceCell renders the owning-resource kind and name. */
function sourceCell(router: ProxyRouter): VNode {
  return h("div", { class: "cell-main" }, [
    h(NTag, { size: "small" }, { default: () => kindLabel(router.kind) }),
    router.owner_name
      ? h("span", { class: "mono cell-sub" }, router.owner_name)
      : null,
  ]);
}

/** nodeCell renders the node name with its sync status. */
function nodeCell(router: ProxyRouter): VNode {
  const status = nodeSyncOf(router);
  return h("div", { class: "cell-main" }, [
    h(
      "span",
      { class: "mono" },
      router.server_name !== "" ? router.server_name : router.server_id.slice(0, 8),
    ),
    h(
      NTag,
      { size: "small", type: syncTagType(status) },
      { default: () => syncLabel(status) },
    ),
  ]);
}

/** tlsCell renders the TLS resolver, or plain HTTP when there is none. */
function tlsCell(router: ProxyRouter): VNode {
  if (!router.tls_resolver) {
    return h(NText, { depth: 3 }, { default: () => proxyText("domains.routers.httpOnly", "HTTP") });
  }
  return h(NTag, { size: "small", type: "success" }, { default: () => router.tls_resolver });
}

const columns: DataTableColumns<ProxyRouter> = [
  {
    title: () => proxyText("domains.routers.host", "Host"),
    key: "host",
    render: (router) => hostCell(router),
  },
  {
    title: () => proxyText("domains.routers.source", "Source"),
    key: "kind",
    render: (router) => sourceCell(router),
  },
  {
    title: () => proxyText("domains.routers.node", "Node"),
    key: "server_id",
    render: (router) => nodeCell(router),
  },
  {
    title: () => proxyText("domains.routers.entrypoints", "Entrypoints"),
    key: "entrypoints",
    render: (router) =>
      h("span", { class: "mono cell-sub" }, router.entrypoints.join("/")),
  },
  {
    title: () => proxyText("domains.routers.tls", "TLS"),
    key: "tls_resolver",
    render: (router) => tlsCell(router),
  },
];

/** emptyText renders the empty state through the current locale. */
const emptyText = computed(() => proxyText("domains.routers.empty", "No routers generated yet."));
</script>

<template>
  <NCard style="margin-top: 16px" :title="$t('domains.routers.title')">
    <template #header-extra>
      <NSpace :size="8" align="center">
        <NInput
          v-model:value="query"
          clearable
          style="max-width: 260px"
          :placeholder="$t('domains.routers.searchPlaceholder')"
          :aria-label="$t('domains.routers.searchAria')"
        />
        <NButton size="small" :loading="proxyStore.routersLoading" @click="refresh">
          {{ $t("domains.routers.refresh") }}
        </NButton>
      </NSpace>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="proxyStore.routersError" type="error" :show-icon="true">
        {{ proxyStore.routersError }}
      </NAlert>
      <NAlert
        v-for="node in failedNodes"
        :key="node.server_id"
        type="warning"
        :show-icon="true"
      >
        {{ node.server_name || node.server_id }}: {{ node.error }}
      </NAlert>
      <NSpin v-if="proxyStore.routersLoading && proxyStore.routers.length === 0" />
      <NDataTable
        v-else
        class="router-table"
        :columns="columns"
        :data="filteredRouters"
        :bordered="false"
        :single-line="false"
      >
        <template #empty>
          <NEmpty :description="emptyText">
            <template #extra>
              <p class="empty-hint">
                {{ $t("domains.routers.emptyHint") }}
              </p>
            </template>
          </NEmpty>
        </template>
      </NDataTable>
    </NSpace>
  </NCard>
</template>

<style scoped>
/* Router-list cell stack (local to this panel: the certificate table owns
   its own pair in JUS-88, so this rule stays scoped here). Every multi-line
   cell stacks its lines with the shared 4px step, matching the other
   tables' cell-name/cell-sub pairs. The :deep span is load-bearing: column
   bodies render through NDataTable render callbacks, so the inner divs
   never carry this component's scope attribute. */
.router-table :deep(.cell-main) {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-1);
}

.router-table :deep(.cell-name) {
  color: var(--fg-2);
  font-weight: 600;
}

.router-table :deep(.cell-sub) {
  display: block;
  font-size: var(--text-xs);
  color: var(--muted);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0;
  max-width: 60ch;
}
</style>
