<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSpin,
  NTag,
} from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import CleanupConfirmModal from "@/features/cleanup/components/CleanupConfirmModal.vue";
import CleanupNodePicker from "@/features/cleanup/components/CleanupNodePicker.vue";
import CleanupResults from "@/features/cleanup/components/CleanupResults.vue";
import {
  useCleanupPage,
  type ScanNodeState,
} from "@/features/cleanup/composables/useCleanupPage";
import { reasonKey } from "@/features/cleanup/utils/cleanupView";
import type { NodeTotal } from "@/features/cleanup/utils/selection";
import { formatBytes } from "@/shared/utils/format";

const { t, te } = useI18n();
const {
  phase,
  serversStore,
  selectedServerIds,
  minAgeValue,
  minAgeUnit,
  scans,
  selectableItems,
  selectedKeys,
  summary,
  hasDangerous,
  confirmOpen,
  runs,
  totalFreed,
  serverName,
  toggleItemKey,
  setCategory,
  startScan,
  backToSelect,
  startClean,
} = useCleanupPage();

const confirmNodes = computed<NodeTotal[]>(() =>
  Object.entries(summary.value.byNode).map(([serverId, value]) => ({
    serverId,
    count: value.count,
    bytes: value.bytes,
  })),
);

const runMessageKeys: Record<string, string> = {
  "in use": "cleanup.run.messages.inUse",
  "too new": "cleanup.run.messages.tooNew",
  "no longer present": "cleanup.run.messages.gone",
  "manual only": "cleanup.run.messages.manualOnly",
  "agent unavailable": "cleanup.run.messages.agentUnavailable",
  "agent too old": "cleanup.run.messages.agentTooOld",
  "agent returned no result": "cleanup.run.messages.noResult",
};

function statusLabel(status: ScanNodeState["status"]): string {
  return t(`cleanup.scan.status.${status}`);
}

function scanTagType(status: ScanNodeState["status"]): "success" | "warning" | "error" {
  if (status === "done") {
    return "success";
  }
  return status === "failed" ? "error" : "warning";
}

function reasonText(reason: string): string {
  const key = reasonKey(reason);
  if (key) {
    return t(key);
  }
  return reason || t("cleanup.reasons.unknown");
}

function runStatusLabel(status: string): string {
  const key = `cleanup.run.${status}`;
  return te(key) ? t(key) : status;
}

function runStatusType(status: string): "success" | "warning" | "error" | "default" {
  if (status === "ok" || status === "removed") {
    return "success";
  }
  if (status === "skipped") {
    return "warning";
  }
  return status === "failed" ? "error" : "default";
}

function runTagType(status: string): "success" | "warning" | "error" {
  if (status === "ok") {
    return "success";
  }
  return status === "skipped" ? "warning" : "error";
}

function messageText(message: string): string {
  const key = runMessageKeys[message];
  return key ? t(key) : message;
}

function openConfirm(): void {
  if (summary.value.count > 0) {
    confirmOpen.value = true;
  }
}
</script>

<template>
  <div class="cleanup-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("cleanup.page.eyebrow") }}</p>
        <h1>{{ t("cleanup.page.title") }}</h1>
        <p class="page-desc">{{ t("cleanup.page.description") }}</p>
      </div>
    </div>

    <NAlert v-if="serversStore.error" type="error" :show-icon="true">
      {{ serversStore.error }}
    </NAlert>

    <template v-if="phase === 'select'">
      <CleanupNodePicker
        :servers="serversStore.servers"
        :selected="selectedServerIds"
        :min-age-value="minAgeValue"
        :min-age-unit="minAgeUnit"
        @update:selected="selectedServerIds = $event"
        @update:min-age-value="minAgeValue = $event"
        @update:min-age-unit="minAgeUnit = $event"
      />

      <div class="cleanup-actions">
        <NButton
          type="primary"
          :disabled="selectedServerIds.size === 0"
          data-testid="cleanup-scan"
          @click="() => void startScan()"
        >
          {{ t("cleanup.actions.scan") }}
        </NButton>
      </div>
    </template>

    <NCard
      v-else-if="phase === 'scanning'"
      :title="t('cleanup.scan.status.scanning')"
      class="cleanup-card"
    >
      <ul class="cleanup-progress">
        <li v-for="scan in scans" :key="scan.serverId" class="cleanup-progress__row">
          <span class="cleanup-progress__name">{{ serverName(scan.serverId) }}</span>
          <NTag size="small" :type="scanTagType(scan.status)">
            {{ statusLabel(scan.status) }}
          </NTag>
          <NSpin v-if="scan.status === 'scanning'" :size="14" />
          <span v-if="scan.reason" class="cleanup-progress__reason">
            {{ reasonText(scan.reason) }}
          </span>
        </li>
      </ul>
    </NCard>

    <template v-else-if="phase === 'results'">
      <CleanupResults
        :scans="scans"
        :items="selectableItems"
        :selected="selectedKeys"
        :server-name="serverName"
        @toggle-item="toggleItemKey"
        @set-category="setCategory"
      />

      <div class="cleanup-actions">
        <NButton @click="backToSelect">{{ t("cleanup.actions.back") }}</NButton>
        <span class="cleanup-selection">
          {{ t("cleanup.results.selected", {
            count: summary.count,
            size: formatBytes(summary.bytes),
          }) }}
        </span>
        <NButton
          type="error"
          :disabled="summary.count === 0"
          data-testid="cleanup-clean"
          @click="openConfirm"
        >
          {{ t("cleanup.actions.clean") }}
        </NButton>
      </div>
    </template>

    <NCard
      v-else-if="phase === 'cleaning'"
      :title="t('cleanup.actions.clean')"
      class="cleanup-card"
    >
      <NSpin show size="small">
        <p class="cleanup-hint">{{ t("cleanup.confirm.body") }}</p>
      </NSpin>
    </NCard>

    <NCard v-else :title="t('cleanup.run.title')" class="cleanup-card">
      <NAlert
        v-for="run in runs.filter((item) => item.error)"
        :key="run.serverId"
        type="error"
        :show-icon="true"
        class="cleanup-run__error"
      >
        {{ serverName(run.serverId) }}: {{ run.error }}
      </NAlert>

      <div v-for="run in runs" :key="run.serverId" class="cleanup-run">
        <div class="cleanup-run__head">
          <span class="cleanup-run__name">{{ serverName(run.serverId) }}</span>
          <NTag size="small" :type="runTagType(run.status)">
            {{ runStatusLabel(run.status) }}
          </NTag>
          <span class="cleanup-run__freed">
            {{ t("cleanup.run.freed", { size: formatBytes(run.reclaimedBytes) }) }}
          </span>
          <span v-if="run.reason" class="cleanup-run__reason">
            {{ reasonText(run.reason) }}
          </span>
        </div>

        <NEmpty
          v-if="run.items.length === 0 && !run.error"
          :description="t('cleanup.run.nothing')"
        />
        <ul v-else class="cleanup-run__items">
          <li v-for="item in run.items" :key="`${run.serverId}-${item.id}`" class="cleanup-run__item">
            <NTag size="small" :type="runStatusType(item.status)">
              {{ runStatusLabel(item.status) }}
            </NTag>
            <span class="cleanup-run__item-name">{{ item.name }}</span>
            <span v-if="item.message" class="cleanup-run__meta">
              {{ messageText(item.message) }}
            </span>
            <span v-if="item.reclaimed_bytes > 0" class="cleanup-run__meta">
              {{ formatBytes(item.reclaimed_bytes) }}
            </span>
          </li>
        </ul>
      </div>

      <p class="cleanup-run__total">
        {{ t("cleanup.run.total", { size: formatBytes(totalFreed) }) }}
      </p>

      <div class="cleanup-actions">
        <NButton type="primary" @click="() => void startScan()">
          {{ t("cleanup.run.scanAgain") }}
        </NButton>
      </div>
    </NCard>

    <CleanupConfirmModal
      :show="confirmOpen"
      :nodes="confirmNodes"
      :total-bytes="summary.bytes"
      :has-dangerous="hasDangerous"
      :server-name="serverName"
      @update:show="confirmOpen = $event"
      @confirm="() => void startClean()"
    />
  </div>
</template>

<style scoped>
.cleanup-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.cleanup-card {
  min-width: 0;
}

.cleanup-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.cleanup-selection {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-sm);
  color: var(--muted);
}

.cleanup-hint {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--muted);
}

.cleanup-progress {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.cleanup-progress__row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-3);
  min-width: 0;
}

.cleanup-progress__name {
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-progress__reason {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-run {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-bottom: var(--space-4);
}

.cleanup-run__error {
  margin-bottom: var(--space-3);
}

.cleanup-run__head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-3);
  min-width: 0;
}

.cleanup-run__name {
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-run__freed,
.cleanup-run__reason {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-run__items {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.cleanup-run__item {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.cleanup-run__item-name {
  min-width: 0;
  overflow-wrap: anywhere;
  font-weight: 500;
  color: var(--fg);
}

.cleanup-run__meta {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-run__total {
  margin: 0 0 var(--space-3);
  font-weight: 600;
  color: var(--fg-2);
}
</style>
