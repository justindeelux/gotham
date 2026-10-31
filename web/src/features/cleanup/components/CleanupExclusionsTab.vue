<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  NSpin,
  NTag,
} from "naive-ui";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { deleteExclusion, listExclusions } from "@/features/cleanup/api/center";
import type { CleanupExclusion } from "@/features/cleanup/schemas/center";
import { failureText } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import { formatDateTime } from "@/shared/utils/format";

const { t, te } = useI18n();
const serversStore = useServersStore();

const exclusions = ref<CleanupExclusion[]>([]);
const loading = ref(false);
const error = ref("");

function serverLabel(serverId?: string): string {
  if (!serverId) {
    return t("cleanup.exclusions.allNodes");
  }
  return serversStore.servers.find((server) => server.id === serverId)?.name ?? serverId;
}

function categoryLabel(category: string): string {
  const key = `cleanup.categories.${category}`;
  return te(key) ? t(key) : category;
}

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    exclusions.value = await listExclusions();
  } catch (cause) {
    error.value = failureText(cause);
  } finally {
    loading.value = false;
  }
}

async function remove(exclusion: CleanupExclusion): Promise<void> {
  error.value = "";
  try {
    await deleteExclusion(exclusion.id);
    await load();
  } catch (cause) {
    error.value = failureText(cause);
  }
}

onMounted(() => {
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => undefined);
  }
  void load();
});
</script>

<template>
  <div class="cleanup-exclusions">
    <p class="cleanup-exclusions__desc">{{ t("cleanup.exclusions.description") }}</p>

    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>

    <NSpin :show="loading">
      <NEmpty v-if="exclusions.length === 0" :description="t('cleanup.exclusions.empty')" />
      <NCard v-else class="cleanup-exclusions__card">
        <ul class="cleanup-exclusion-list">
          <li
            v-for="exclusion in exclusions"
            :key="exclusion.id"
            class="cleanup-exclusion-row"
            :data-testid="`cleanup-exclusion-${exclusion.match}`"
          >
            <div class="cleanup-exclusion-row__main">
              <span class="cleanup-exclusion-row__match">{{ exclusion.match }}</span>
              <NTag size="small">{{ categoryLabel(exclusion.category) }}</NTag>
              <NTag size="small" type="info">{{ serverLabel(exclusion.server_id) }}</NTag>
              <span v-if="exclusion.note" class="cleanup-exclusion-row__meta">
                {{ exclusion.note }}
              </span>
              <span class="cleanup-exclusion-row__meta">
                {{ formatDateTime(exclusion.created_at) }}
              </span>
            </div>
            <NPopconfirm @positive-click="() => void remove(exclusion)">
              <template #trigger>
                <NButton size="small" quaternary type="error">
                  {{ t("cleanup.exclusions.remove") }}
                </NButton>
              </template>
              {{ t("cleanup.exclusions.removeConfirm", { match: exclusion.match }) }}
            </NPopconfirm>
          </li>
        </ul>
      </NCard>
    </NSpin>
  </div>
</template>

<style scoped>
.cleanup-exclusions {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.cleanup-exclusions__desc {
  margin: 0;
  max-width: 640px;
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-sm);
  color: var(--muted);
}

.cleanup-exclusions__card {
  min-width: 0;
}

.cleanup-exclusion-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.cleanup-exclusion-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-3);
  min-width: 0;
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--border);
}

.cleanup-exclusion-row:last-child {
  border-bottom: 0;
}

.cleanup-exclusion-row__main {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
  flex: 1 1 280px;
}

.cleanup-exclusion-row__match {
  min-width: 0;
  overflow-wrap: anywhere;
  font-weight: 500;
  color: var(--fg);
}

.cleanup-exclusion-row__meta {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}
</style>
