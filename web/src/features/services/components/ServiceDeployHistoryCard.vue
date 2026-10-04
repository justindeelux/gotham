<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NTag, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h } from "vue";
import type { VNode } from "vue";

import { deployStateTagType } from "@/features/services/api/services";
import type { ServiceDeploy } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { durationText } from "@/features/services/utils/deployDuration";
import { relativeTime } from "@/shared/utils/format";

/** ServiceDeployHistoryCard renders the deploy attempts table card. */
const {
  deploys,
  latestDeploy,
  historyLoaded,
  historyUnavailable,
  historyLoading,
  retryHistory,
} = useServiceDetailContext();

/** stateCell renders one deploy state as a tag. */
function stateCell(deploy: ServiceDeploy): VNode {
  return h(
    NTag,
    { size: "small", type: deployStateTagType(deploy.state) },
    { default: () => deploy.state },
  );
}

/** errorCell renders the redacted failure message, or an em dash. */
function errorCell(deploy: ServiceDeploy): VNode {
  if (!deploy.error) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h("span", { class: "mono error-text" }, deploy.error);
}

const deployColumns: DataTableColumns<ServiceDeploy> = [
  {
    title: "Deploy",
    key: "id",
    width: 110,
    render: (row) => h("span", { class: "mono" }, row.id.slice(0, 8)),
  },
  {
    title: "State",
    key: "state",
    width: 120,
    render: (row) => stateCell(row),
  },
  {
    title: "Error",
    key: "error",
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => errorCell(row),
  },
  {
    title: "Duration",
    key: "duration",
    width: 100,
    render: (row) => h("span", { class: "num" }, durationText(row)),
  },
  {
    title: "Created",
    key: "created_at",
    width: 120,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: "Finished",
    key: "finished_at",
    width: 120,
    render: (row) => relativeTime(row.finished_at),
  },
];

/** deployRowKey identifies a history row by its deploy id. */
function deployRowKey(row: ServiceDeploy): string {
  return row.id;
}
</script>

<template>
  <NCard title="Deploy history">
    <template #header-extra>
      <NText v-if="historyLoaded" depth="3" class="small">
        {{ deploys.length }} attempts · newest first
      </NText>
      <NText v-else-if="historyUnavailable" depth="3" class="small">
        unavailable
      </NText>
    </template>
    <NAlert
      v-if="historyUnavailable"
      type="warning"
      :show-icon="true"
      data-testid="history-unavailable"
    >
      <div class="history-error">
        <span>
          Deploy history unavailable: {{ historyUnavailable }}
          <template v-if="deploys.length > 0">
            — showing the last successful read.
          </template>
        </span>
        <NButton
          size="small"
          :loading="historyLoading"
          @click="retryHistory"
        >
          Retry
        </NButton>
      </div>
    </NAlert>
    <NDataTable
      v-if="deploys.length > 0"
      :data="deploys"
      :columns="deployColumns"
      :row-key="deployRowKey"
      :bordered="false"
      :scroll-x="900"
    />
    <NEmpty
      v-else-if="historyLoaded"
      description="No deploys yet."
      data-testid="history-empty"
    >
      <template #extra>
        <p class="empty-hint">
          Deploy renders the stored document on the node and records the
          attempt here. The rendered snapshot of each deploy is what a
          rollback would redeploy.
        </p>
      </template>
    </NEmpty>
    <NEmpty
      v-else-if="historyLoading"
      description="Reading deploy history…"
    />
    <NEmpty
      v-else
      description="Deploy history not loaded."
    />
    <template #footer>
      <NText depth="3" class="small">
        <template v-if="latestDeploy">
          Newest attempt:
          <span class="mono">{{ latestDeploy.id.slice(0, 8) }}</span> ·
          {{ latestDeploy.state }} · {{ relativeTime(latestDeploy.created_at) }}.
        </template>
        <template v-else-if="historyLoaded">
          Nothing has been deployed yet.
        </template>
        <template v-else-if="historyUnavailable">
          The history could not be read, so nothing is claimed about earlier
          attempts.
        </template>
        <template v-else>
          Reading the history…
        </template>
        The API does not expose a per-deploy log or a step timeline; logs
        stream from the running project instead.
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.history-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.small {
  font-size: var(--text-xs);
}

.mono {
  font-family: var(--font-mono);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0;
  max-width: 70ch;
}
</style>
