<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NPopconfirm,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h } from "vue";
import { useI18n } from "vue-i18n";

import type { OrphanedVolume } from "@/features/servers/api/orphanedVolumes";
import { useOrphanedVolumes } from "@/features/servers/composables/useOrphanedVolumes";
import { formatBytes, formatDate } from "@/shared/utils/format";

const props = defineProps<{ serverId: string }>();
const serverId = computed<string>(() => props.serverId);
const { t } = useI18n();

const {
  volumes,
  loading,
  loaded,
  error,
  removing,
  fetchVolumes,
  removeVolume,
} = useOrphanedVolumes(serverId);

function createdCell(row: OrphanedVolume): string {
  return row.created_at ? formatDate(row.created_at) : "—";
}

function sizeCell(row: OrphanedVolume): string {
  return row.size_bytes > 0 ? formatBytes(row.size_bytes) : "—";
}

function rowKey(row: OrphanedVolume): string {
  return row.name;
}

const columns = computed<DataTableColumns<OrphanedVolume>>(() => [
  {
    title: t("servers.volumes.tableName"),
    key: "name",
    minWidth: 240,
    ellipsis: { tooltip: true },
    render: (row) => h("span", { class: "mono" }, row.name),
  },
  {
    title: t("servers.volumes.tableCreated"),
    key: "created_at",
    width: 180,
    render: (row) => h("span", { class: "mono muted" }, createdCell(row)),
  },
  {
    title: t("servers.volumes.tableSize"),
    key: "size_bytes",
    width: 120,
    render: (row) => h("span", { class: "mono" }, sizeCell(row)),
  },
  {
    title: t("servers.volumes.tableActions"),
    key: "actions",
    width: 140,
    render: (row) =>
      h(
        NPopconfirm,
        {
          onPositiveClick: () => void removeVolume(row.name),
          positiveText: t("servers.volumes.confirmRemove"),
          negativeText: t("servers.volumes.cancel"),
        },
        {
          trigger: () =>
            h(
              NButton,
              {
                size: "small",
                type: "error",
                secondary: true,
                loading: removing.value === row.name,
                disabled: removing.value !== null,
                "aria-label": t("servers.volumes.removeLabel", { name: row.name }),
              },
              { default: () => t("servers.volumes.remove") },
            ),
          default: () =>
            t("servers.volumes.removeConfirm", { name: row.name }),
        },
      ),
  },
]);

const emptyDescription = computed<string>(() =>
  loaded.value
    ? t("servers.volumes.emptyNone")
    : t("servers.volumes.emptyLoading"),
);
</script>

<template>
  <NCard class="volumes-card" :title="$t('servers.volumes.title')">
    <template #header-extra>
      <NButton size="small" secondary :loading="loading" @click="fetchVolumes(true)">
        {{ $t("servers.volumes.refresh") }}
      </NButton>
    </template>

    <p class="volumes-hint">{{ $t("servers.volumes.hint") }}</p>

    <NAlert v-if="error" type="error" :show-icon="true" class="volumes-alert">
      {{ error }}
    </NAlert>

    <NDataTable
      :columns="columns"
      :data="volumes"
      :loading="loading"
      :row-key="rowKey"
      :bordered="false"
      :scroll-x="680"
    >
      <template #empty>
        <NEmpty :description="emptyDescription" />
      </template>
    </NDataTable>
  </NCard>
</template>

<style scoped>
.volumes-card {
  margin-top: var(--space-4);
}

.volumes-hint {
  margin: 0 0 var(--space-3);
  color: var(--muted);
  font-size: var(--text-xs);
}

.volumes-alert {
  margin-bottom: var(--space-3);
}
</style>
