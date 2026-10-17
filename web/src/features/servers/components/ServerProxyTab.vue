<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted } from "vue";
import type { VNode } from "vue";

import { proxyText, useProxyStore } from "@/features/domains";
import type { ProxyRouter } from "@/features/domains";

const props = defineProps<{ serverId: string }>();

const proxyStore = useProxyStore();

/** routersOfNode filters the shared router cache to this node. */
const routersOfNode = computed<ProxyRouter[]>(() =>
  proxyStore.routers.filter((router) => router.server_id === props.serverId),
);

/** nodeState is this node's reported read outcome, if any. */
const nodeState = computed(
  () =>
    proxyStore.routerNodes.find((node) => node.server_id === props.serverId) ??
    null,
);

/** syncStatus is the node's reported sync state, never invented. */
const syncStatus = computed<string>(() => nodeState.value?.sync_status ?? "unknown");

/** refresh loads the generated routers (shared cache, all nodes). */
async function refresh(): Promise<void> {
  await proxyStore.fetchRouters().catch(() => undefined);
}

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

/** syncLabel renders the node sync status; unknown is never a success. */
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

onMounted(() => {
  if (proxyStore.routers.length === 0 && !proxyStore.routersLoading) {
    void refresh();
  }
});
</script>

<template>
  <NCard style="margin-top: 16px" :title="$t('servers.proxy.title')">
    <template #header-extra>
      <NSpace :size="8" align="center">
        <NTag :type="syncTagType(syncStatus)" size="small">
          {{ syncLabel(syncStatus) }}
        </NTag>
        <NButton
          size="small"
          :loading="proxyStore.routersLoading"
          @click="void refresh()"
        >
          {{ $t("servers.proxy.refresh") }}
        </NButton>
      </NSpace>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="proxyStore.routersError" type="error" :show-icon="true">
        <NSpace align="center" :size="12" wrap>
          <span>{{ proxyStore.routersError }}</span>
          <NButton size="small" @click="void refresh()">
            {{ $t("servers.proxy.retry") }}
          </NButton>
        </NSpace>
      </NAlert>
      <NAlert
        v-if="nodeState?.error"
        type="warning"
        :show-icon="true"
      >
        {{ nodeState.error }}
      </NAlert>
      <NSpin :show="proxyStore.routersLoading">
        <NDataTable
          class="router-table"
          data-testid="server-routers-table"
          :columns="columns"
          :data="routersOfNode"
          :bordered="false"
          :single-line="false"
          :scroll-x="640"
        >
          <template #empty>
            <NEmpty
              :description="
                proxyStore.routersLoading
                  ? $t('servers.proxy.loading')
                  : $t('domains.routers.empty')
              "
            >
              <template v-if="!proxyStore.routersLoading" #extra>
                <p class="empty-hint">
                  {{ $t("domains.routers.emptyHint") }}
                </p>
              </template>
            </NEmpty>
          </template>
        </NDataTable>
      </NSpin>
    </NSpace>
  </NCard>
</template>

<style scoped>
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
