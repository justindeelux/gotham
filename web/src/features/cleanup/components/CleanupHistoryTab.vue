<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSelect,
  NSpin,
  NTag,
} from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { getRun, listRuns } from "@/features/cleanup/api/center";
import type { CleanupRun, CleanupRunDetail } from "@/features/cleanup/schemas/center";
import { failureText } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import { formatBytes, formatDateTime } from "@/shared/utils/format";

const { t, te } = useI18n();
const serversStore = useServersStore();

const runs = ref<CleanupRun[]>([]);
const details = ref<Record<string, CleanupRunDetail>>({});
const expanded = ref<string | null>(null);
const serverFilter = ref<string | null>(null);
const loading = ref(false);
const loadingMore = ref(false);
const hasMore = ref(false);
const error = ref("");
const limit = 25;

const serverOptions = computed(() =>
  serversStore.servers.map((server) => ({ label: server.name, value: server.id })),
);

function serverName(serverId: string): string {
  return serversStore.servers.find((server) => server.id === serverId)?.name ?? serverId;
}

function triggerLabel(trigger: string): string {
  const key = `cleanup.history.triggers.${trigger}`;
  return te(key) ? t(key) : trigger;
}

function statusLabel(status: string): string {
  const key = `cleanup.history.status.${status}`;
  return te(key) ? t(key) : status;
}

function statusType(status: string): "success" | "warning" | "error" | "default" {
  if (status === "ok") {
    return "success";
  }
  return status === "skipped" ? "warning" : status === "failed" ? "error" : "default";
}

function actorText(run: CleanupRun): string {
  return run.schedule_name || run.actor || "—";
}

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  expanded.value = null;
  details.value = {};
  try {
    runs.value = await listRuns({
      serverId: serverFilter.value || undefined,
      limit,
    });
    hasMore.value = runs.value.length === limit;
  } catch (cause) {
    error.value = failureText(cause);
  } finally {
    loading.value = false;
  }
}

async function loadMore(): Promise<void> {
  const last = runs.value[runs.value.length - 1];
  if (!last) {
    return;
  }
  loadingMore.value = true;
  error.value = "";
  try {
    const next = await listRuns({
      serverId: serverFilter.value || undefined,
      limit,
      before: last.started_at,
    });
    const seen = new Set(runs.value.map((run) => run.id));
    runs.value = [...runs.value, ...next.filter((run) => !seen.has(run.id))];
    hasMore.value = next.length === limit;
  } catch (cause) {
    error.value = failureText(cause);
  } finally {
    loadingMore.value = false;
  }
}

async function toggle(run: CleanupRun): Promise<void> {
  if (expanded.value === run.id) {
    expanded.value = null;
    return;
  }
  expanded.value = run.id;
  if (details.value[run.id]) {
    return;
  }
  try {
    const detail = await getRun(run.id);
    details.value = { ...details.value, [run.id]: detail };
  } catch (cause) {
    error.value = failureText(cause);
  }
}

watch(serverFilter, () => {
  void load();
});

onMounted(() => {
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => undefined);
  }
  void load();
});
</script>

<template>
  <div class="cleanup-history">
    <div class="cleanup-history__head">
      <p class="cleanup-history__desc">{{ t("cleanup.history.description") }}</p>
      <NSelect
        v-model:value="serverFilter"
        :options="serverOptions"
        :placeholder="t('cleanup.history.allServers')"
        :aria-label="t('cleanup.history.filter')"
        clearable
        class="cleanup-history__filter"
      />
    </div>

    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>

    <NSpin :show="loading">
      <NEmpty v-if="runs.length === 0" :description="t('cleanup.history.empty')" />
      <NCard v-else class="cleanup-history__card">
        <ul class="cleanup-history-list">
          <li
            v-for="run in runs"
            :key="run.id"
            class="cleanup-history-row"
            :data-testid="`cleanup-run-${run.id}`"
          >
            <button
              type="button"
              class="cleanup-history-row__head"
              :aria-expanded="expanded === run.id"
              @click="() => void toggle(run)"
            >
              <span class="cleanup-history-row__time">{{ formatDateTime(run.started_at) }}</span>
              <NTag size="small" type="info">{{ triggerLabel(run.trigger) }}</NTag>
              <span class="cleanup-history-row__actor">{{ actorText(run) }}</span>
              <span class="cleanup-history-row__node">{{ serverName(run.server_id) }}</span>
              <NTag size="small" :type="statusType(run.status)">
                {{ statusLabel(run.status) }}
              </NTag>
              <span class="cleanup-history-row__freed">
                {{ formatBytes(run.reclaimed_bytes) }}
              </span>
              <span v-if="run.reason" class="cleanup-history-row__reason">{{ run.reason }}</span>
            </button>

            <div v-if="expanded === run.id" class="cleanup-history-row__detail">
              <p
                v-if="Object.keys(run.categories ?? {}).length > 0"
                class="cleanup-history-row__categories"
              >
                <NTag
                  v-for="(bytes, category) in run.categories ?? {}"
                  :key="`${run.id}-${category}`"
                  size="small"
                >
                  {{ t(`cleanup.categories.${category}`) }} · {{ formatBytes(bytes) }}
                </NTag>
              </p>
              <NSpin
                v-if="!details[run.id]"
                size="small"
                :description="t('cleanup.history.loadingItems')"
              />
              <NEmpty
                v-else-if="(details[run.id]?.items?.length ?? 0) === 0"
                :description="t('cleanup.history.noItems')"
              />
              <ul v-else class="cleanup-history-items">
                <li
                  v-for="item in details[run.id]?.items ?? []"
                  :key="`${run.id}-${item.id}`"
                  class="cleanup-history-item"
                >
                  <span class="cleanup-history-item__name">{{ item.name }}</span>
                  <span class="cleanup-history-item__meta">{{ item.category }}</span>
                  <NTag size="small" :type="item.status === 'removed' ? 'success' : item.status === 'skipped' ? 'warning' : 'error'">
                    {{ item.status }}
                  </NTag>
                  <span v-if="item.message" class="cleanup-history-item__meta">
                    {{ item.message }}
                  </span>
                  <span v-if="item.reclaimed_bytes > 0" class="cleanup-history-item__meta">
                    {{ formatBytes(item.reclaimed_bytes) }}
                  </span>
                </li>
              </ul>
            </div>
          </li>
        </ul>
      </NCard>

      <div v-if="hasMore" class="cleanup-history__more">
        <NButton :loading="loadingMore" @click="() => void loadMore()">
          {{ t("cleanup.history.loadMore") }}
        </NButton>
      </div>
    </NSpin>
  </div>
</template>

<style scoped>
.cleanup-history {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.cleanup-history__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.cleanup-history__desc {
  margin: 0;
  max-width: 640px;
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-sm);
  color: var(--muted);
}

.cleanup-history__filter {
  width: 220px;
  max-width: 100%;
}

.cleanup-history__card {
  min-width: 0;
}

.cleanup-history-list,
.cleanup-history-items {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.cleanup-history-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--border);
}

.cleanup-history-row:last-child {
  border-bottom: 0;
}

.cleanup-history-row__head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-3);
  min-width: 0;
  width: 100%;
  padding: 0;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.cleanup-history-row__time,
.cleanup-history-row__actor,
.cleanup-history-row__node,
.cleanup-history-row__freed,
.cleanup-history-row__reason {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-history-row__actor,
.cleanup-history-row__node {
  font-weight: 500;
  color: var(--fg);
}

.cleanup-history-row__reason {
  flex: 1 1 160px;
}

.cleanup-history-row__detail {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-2) 0 var(--space-2) var(--space-4);
}

.cleanup-history-row__categories {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin: 0;
}

.cleanup-history-item {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.cleanup-history-item__name {
  min-width: 0;
  overflow-wrap: anywhere;
  font-weight: 500;
  color: var(--fg);
}

.cleanup-history-item__meta {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-history__more {
  display: flex;
  justify-content: center;
  margin-top: var(--space-2);
}
</style>
