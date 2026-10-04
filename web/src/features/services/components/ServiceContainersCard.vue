<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NSpace, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";

import type { ComposeServiceContainer } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";

/** ServiceContainersCard renders the on-demand node containers card. */
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

const containerColumns: DataTableColumns<ComposeServiceContainer> = [
  { title: "Service", key: "service", width: 140 },
  { title: "Container", key: "name", minWidth: 220 },
  {
    title: "Image",
    key: "image",
    minWidth: 200,
    ellipsis: { tooltip: true },
  },
  { title: "State", key: "state", width: 110 },
  { title: "Status", key: "status", width: 160 },
  { title: "Health", key: "health", width: 110 },
];
</script>

<template>
  <NCard title="Containers">
    <template #header-extra>
      <NSpace :size="8" align="center">
        <NText depth="3" class="small">observed from the node agent</NText>
        <NButton size="small" :loading="containersLoading" @click="loadContainers">
          Refresh containers
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
        description="No container list loaded."
      >
        <template #extra>
          <p class="empty-hint">
            Reading the project's containers dials the node agent, so it
            happens on demand. Without a connected agent the API answers
            502.
            <template v-if="containersLoaded">
              The last read returned no containers for this project.
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
