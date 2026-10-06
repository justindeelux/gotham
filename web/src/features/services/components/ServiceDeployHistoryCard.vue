<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NTag, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h } from "vue";
import type { VNode } from "vue";

import { deployStateLabel, deployStateTagType } from "@/features/services/api/services";
import type { ServiceDeploy } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { durationText } from "@/features/services/utils/deployDuration";
import { activeLocale, i18n } from "@/shared/i18n";
import { relativeTime } from "@/shared/utils/format";
import { computed } from "vue";

/** ServiceDeployHistoryCard renders the deploy attempts table card. */

/**
 * t renders card copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without reloading history.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

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
    { default: () => deployStateLabel(deploy.state) },
  );
}

/** errorCell renders the redacted failure message, or an em dash. */
function errorCell(deploy: ServiceDeploy): VNode {
  if (!deploy.error) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h("span", { class: "mono error-text" }, deploy.error);
}

/** deployColumns resolves headers in the active locale. */
const deployColumns = computed<DataTableColumns<ServiceDeploy>>(() => [
  {
    title: t("services.history.columns.deploy"),
    key: "id",
    width: 110,
    render: (row) => h("span", { class: "mono" }, row.id.slice(0, 8)),
  },
  {
    title: t("services.history.columns.state"),
    key: "state",
    width: 120,
    render: (row) => stateCell(row),
  },
  {
    title: t("services.history.columns.error"),
    key: "error",
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => errorCell(row),
  },
  {
    title: t("services.history.columns.duration"),
    key: "duration",
    width: 100,
    render: (row) => h("span", { class: "num" }, durationText(row)),
  },
  {
    title: t("services.history.columns.created"),
    key: "created_at",
    width: 120,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: t("services.history.columns.finished"),
    key: "finished_at",
    width: 120,
    render: (row) => relativeTime(row.finished_at),
  },
]);

/** deployRowKey identifies a history row by its deploy id. */
function deployRowKey(row: ServiceDeploy): string {
  return row.id;
}
</script>

<template>
  <NCard :title="t('services.history.title')">
    <template #header-extra>
      <NText v-if="historyLoaded" depth="3" class="small">
        {{
          t(
            deploys.length === 1
              ? "services.history.attemptsOne"
              : "services.history.attemptsOther",
            { count: deploys.length },
          )
        }}
      </NText>
      <NText v-else-if="historyUnavailable" depth="3" class="small">
        {{ t("services.history.unavailableTag") }}
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
          {{ t("services.history.unavailableAlert", { error: historyUnavailable }) }}
          <template v-if="deploys.length > 0">
            {{ t("services.history.staleNote") }}
          </template>
        </span>
        <NButton
          size="small"
          :loading="historyLoading"
          @click="retryHistory"
        >
          {{ t("common.actions.retry") }}
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
      :description="t('services.history.empty')"
      data-testid="history-empty"
    >
      <template #extra>
        <p class="empty-hint">
          {{ t("services.history.emptyHint") }}
        </p>
      </template>
    </NEmpty>
    <NEmpty
      v-else-if="historyLoading"
      :description="t('services.history.reading')"
    />
    <NEmpty
      v-else
      :description="t('services.history.notLoaded')"
    />
    <template #footer>
      <NText depth="3" class="small">
        <template v-if="latestDeploy">
          {{
            t("services.history.newest", {
              id: latestDeploy.id.slice(0, 8),
              state: deployStateLabel(latestDeploy.state),
              when: relativeTime(latestDeploy.created_at),
            })
          }}
        </template>
        <template v-else-if="historyLoaded">
          {{ t("services.history.nothingDeployed") }}
        </template>
        <template v-else-if="historyUnavailable">
          {{ t("services.history.unreadable") }}
        </template>
        <template v-else>
          {{ t("services.history.readingNow") }}
        </template>
        {{ t("services.history.noTimelineNote") }}
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
