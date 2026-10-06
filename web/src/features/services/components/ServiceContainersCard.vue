<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NSpace, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";

import type { ComposeServiceContainer } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";
import { computed } from "vue";

/** ServiceContainersCard renders the on-demand node containers card. */

/**
 * t renders card copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without reloading containers.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const {
  containers,
  containersLoading,
  containersError,
  containersLoaded,
  loadContainers,
} = useServiceDetailContext();

/** containerRowKey identifies a container row by its container id. */
function containerRowKey(row: ComposeServiceContainer): string {
  return row.container_id;
}

/** containerColumns resolves headers in the active locale. */
const containerColumns = computed<DataTableColumns<ComposeServiceContainer>>(() => [
  { title: t("services.containers.columns.service"), key: "service", width: 140 },
  { title: t("services.containers.columns.container"), key: "name", minWidth: 220 },
  {
    title: t("services.containers.columns.image"),
    key: "image",
    minWidth: 200,
    ellipsis: { tooltip: true },
  },
  { title: t("services.containers.columns.state"), key: "state", width: 110 },
  { title: t("services.containers.columns.status"), key: "status", width: 160 },
  { title: t("services.containers.columns.health"), key: "health", width: 110 },
]);
</script>

<template>
  <NCard :title="t('services.containers.title')">
    <template #header-extra>
      <NSpace :size="8" align="center">
        <NText depth="3" class="small">{{ t("services.containers.observedNote") }}</NText>
        <NButton size="small" :loading="containersLoading" @click="loadContainers">
          {{ t("services.containers.refresh") }}
        </NButton>
      </NSpace>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="containersError" type="warning" :show-icon="true">
        {{ containersError }}
      </NAlert>
      <NDataTable
        v-if="containers.length > 0"
        :data="containers"
        :columns="containerColumns"
        :row-key="containerRowKey"
        :bordered="false"
        :scroll-x="1000"
        size="small"
      />
      <NEmpty
        v-else-if="!containersLoading"
        :description="t('services.containers.empty')"
      >
        <template #extra>
          <p class="empty-hint">
            {{ t("services.containers.hint") }}
            <template v-if="containersLoaded">
              {{ t("services.containers.emptyAfterRead") }}
            </template>
          </p>
        </template>
      </NEmpty>
    </NSpace>
  </NCard>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0;
  max-width: 70ch;
}
</style>
