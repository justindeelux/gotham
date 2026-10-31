<script setup lang="ts">
import {
  NAlert,
  NButton,
  NEmpty,
  NPopconfirm,
  NSpin,
  NSwitch,
  NTag,
} from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  createSchedule,
  deleteSchedule,
  listSchedules,
  runScheduleNow,
  updateSchedule,
} from "@/features/cleanup/api/center";
import { listCleanupCategories, type CleanupCategory } from "@/features/cleanup/api/cleanup";
import CleanupScheduleModal from "@/features/cleanup/components/CleanupScheduleModal.vue";
import CleanupThresholdCard from "@/features/cleanup/components/CleanupThresholdCard.vue";
import type { CleanupSchedule, ScheduleForm } from "@/features/cleanup/schemas/center";
import { failureText } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import { formatDateTime } from "@/shared/utils/format";

const { t, te } = useI18n();
const serversStore = useServersStore();

const schedules = ref<CleanupSchedule[]>([]);
const categories = ref<CleanupCategory[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const modalOpen = ref(false);
const editing = ref<CleanupSchedule | null>(null);

const serverName = computed(() => (serverId: string) =>
  serversStore.servers.find((server) => server.id === serverId)?.name ?? serverId,
);

const tierByCategory = computed<Record<string, string>>(() => {
  const map: Record<string, string> = {};
  for (const category of categories.value) {
    map[category.key] = category.tier;
  }
  return map;
});

function categoryLabel(category: string): string {
  const key = `cleanup.categories.${category}`;
  return te(key) ? t(key) : category;
}

function tierLabel(category: string): string {
  const tier = tierByCategory.value[category] ?? "careful";
  return t(`cleanup.tiers.${tier}`);
}

function tierType(category: string): "success" | "warning" | "error" {
  const tier = tierByCategory.value[category] ?? "careful";
  return tier === "safe" ? "success" : tier === "dangerous" ? "error" : "warning";
}

function formFrom(schedule: CleanupSchedule, enabled = schedule.enabled): ScheduleForm {
  return {
    name: schedule.name,
    server_ids: [...schedule.server_ids],
    categories: [...schedule.categories],
    cron: schedule.cron,
    min_age_hours: schedule.min_age_hours,
    enabled,
  };
}

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const [loaded, loadedCategories] = await Promise.all([
      listSchedules(),
      categories.value.length > 0 ? Promise.resolve(categories.value) : listCleanupCategories(),
    ]);
    schedules.value = loaded;
    categories.value = loadedCategories;
  } catch (cause) {
    error.value = failureText(cause);
  } finally {
    loading.value = false;
  }
}

function openCreate(): void {
  editing.value = null;
  modalOpen.value = true;
}

function openEdit(schedule: CleanupSchedule): void {
  editing.value = schedule;
  modalOpen.value = true;
}

async function save(form: ScheduleForm): Promise<void> {
  saving.value = true;
  error.value = "";
  try {
    if (editing.value) {
      await updateSchedule(editing.value.id, form);
      notice.value = t("cleanup.schedules.saved");
    } else {
      await createSchedule(form);
      notice.value = t("cleanup.schedules.created");
    }
    modalOpen.value = false;
    await load();
  } catch (cause) {
    error.value = failureText(cause);
  } finally {
    saving.value = false;
  }
}

async function toggleEnabled(schedule: CleanupSchedule, value: boolean): Promise<void> {
  error.value = "";
  try {
    await updateSchedule(schedule.id, formFrom(schedule, value));
    await load();
  } catch (cause) {
    error.value = failureText(cause);
  }
}

async function runNow(schedule: CleanupSchedule): Promise<void> {
  error.value = "";
  notice.value = "";
  try {
    await runScheduleNow(schedule.id);
    notice.value = t("cleanup.schedules.started");
  } catch (cause) {
    error.value = failureText(cause);
  }
}

async function remove(schedule: CleanupSchedule): Promise<void> {
  error.value = "";
  try {
    await deleteSchedule(schedule.id);
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
  <div class="cleanup-schedules">
    <div class="cleanup-schedules__head">
      <p class="cleanup-schedules__desc">{{ t("cleanup.schedules.description") }}</p>
      <NButton type="primary" data-testid="cleanup-schedule-new" @click="openCreate">
        {{ t("cleanup.schedules.new") }}
      </NButton>
    </div>

    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
    <NAlert v-if="notice" type="success" :show-icon="true">{{ notice }}</NAlert>

    <CleanupThresholdCard :categories="categories" />

    <NSpin :show="loading">
      <NEmpty v-if="schedules.length === 0" :description="t('cleanup.schedules.empty')" />
      <ul v-else class="cleanup-schedule-list">
        <li
          v-for="schedule in schedules"
          :key="schedule.id"
          class="cleanup-schedule-row"
          :data-testid="`cleanup-schedule-${schedule.name}`"
        >
          <div class="cleanup-schedule-row__main">
            <div class="cleanup-schedule-row__title">
              <span class="cleanup-schedule-row__name">{{ schedule.name }}</span>
              <NTag v-if="!schedule.enabled" size="small">{{ t("cleanup.schedules.disabled") }}</NTag>
              <NTag size="small" type="info">{{ schedule.cron }}</NTag>
            </div>
            <div class="cleanup-schedule-row__meta">
              <span>{{ t("cleanup.schedules.nodes", { nodes: schedule.server_ids.map(serverName).join(", ") }) }}</span>
              <span>{{ t("cleanup.schedules.next", { time: formatDateTime(schedule.next_run_at) }) }}</span>
              <span v-if="schedule.last_run_at">
                {{ t("cleanup.schedules.last", { time: formatDateTime(schedule.last_run_at) }) }}
              </span>
              <span>{{ t("cleanup.schedules.minAge", { hours: schedule.min_age_hours }) }}</span>
            </div>
            <div class="cleanup-schedule-row__categories">
              <NTag
                v-for="category in schedule.categories"
                :key="`${schedule.id}-${category}`"
                size="small"
                :type="tierType(category)"
              >
                {{ categoryLabel(category) }} · {{ tierLabel(category) }}
              </NTag>
            </div>
          </div>

          <div class="cleanup-schedule-row__actions">
            <NSwitch
              :value="schedule.enabled"
              :aria-label="t('cleanup.schedules.enabledAria', { name: schedule.name })"
              @update:value="(value: boolean) => void toggleEnabled(schedule, value)"
            />
            <NButton size="small" @click="() => void runNow(schedule)">
              {{ t("cleanup.schedules.runNow") }}
            </NButton>
            <NButton size="small" @click="openEdit(schedule)">
              {{ t("cleanup.schedules.edit") }}
            </NButton>
            <NPopconfirm @positive-click="() => void remove(schedule)">
              <template #trigger>
                <NButton size="small" quaternary type="error">
                  {{ t("cleanup.schedules.delete") }}
                </NButton>
              </template>
              {{ t("cleanup.schedules.deleteConfirm", { name: schedule.name }) }}
            </NPopconfirm>
          </div>
        </li>
      </ul>
    </NSpin>

    <CleanupScheduleModal
      :show="modalOpen"
      :schedule="editing"
      :servers="serversStore.servers"
      :categories="categories"
      :saving="saving"
      @update:show="modalOpen = $event"
      @save="(form) => void save(form)"
    />
  </div>
</template>

<style scoped>
.cleanup-schedules {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.cleanup-schedules__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-3);
}

.cleanup-schedules__desc {
  margin: 0;
  max-width: 640px;
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-sm);
  color: var(--muted);
}

.cleanup-schedule-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.cleanup-schedule-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-3);
  min-width: 0;
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.cleanup-schedule-row__main {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
  flex: 1 1 320px;
}

.cleanup-schedule-row__title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.cleanup-schedule-row__name {
  min-width: 0;
  overflow-wrap: anywhere;
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-schedule-row__meta {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
  min-width: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-schedule-row__meta > span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.cleanup-schedule-row__categories {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.cleanup-schedule-row__actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
}

@media (max-width: 480px) {
  .cleanup-schedule-row__actions {
    width: 100%;
  }
}
</style>
