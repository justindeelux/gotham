<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NInput,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { inject } from "vue";

import {
  CRON_PRESETS,
  databaseBackupsKey,
} from "@/features/databases/composables/useDatabaseBackups";
import { isCronPresent } from "@/features/databases/schemas/databases";
import { relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NCard :title="t('databases.backups.schedules.title')">
    <template #header-extra>
      <NText depth="3">{{ t("databases.backups.schedules.subtitle") }}</NText>
    </template>
    <NAlert
      v-if="backups.backupsStore.schedulesError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ backups.backupsStore.schedulesError }}
    </NAlert>
    <NSpin :show="backups.backupsStore.schedulesLoading">
      <NSpace
        v-if="backups.backupsStore.schedulesOf(backups.dbId.value).length > 0"
        vertical
        :size="12"
        style="width: 100%"
      >
        <div
          v-for="schedule in backups.backupsStore.schedulesOf(backups.dbId.value)"
          :key="schedule.id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NText class="mono" strong>
                {{ schedule.cron }}
              </NText>
              <NTag size="small" :bordered="false">
                {{ backups.targetLabel(schedule.target_id) }}
              </NTag>
            </NSpace>
            <NText depth="3">
              {{ t("databases.backups.schedules.nextRun") }}
              <span :title="schedule.next_run_at">
                {{ relativeTime(schedule.next_run_at) }}
              </span>
              <span v-if="schedule.last_run_at">
                · {{ t("databases.backups.schedules.lastRun") }}
                <span :title="schedule.last_run_at">
                  {{ relativeTime(schedule.last_run_at) }}
                </span>
              </span>
            </NText>
          </div>
          <NSpace class="backup-row__actions" align="center" :size="8">
            <NSwitch
              :value="schedule.enabled"
              :aria-label="t('databases.backups.schedules.enableAria', { cron: schedule.cron })"
              :loading="backups.backupsStore.schedulesActing"
              @update:value="
                (enabled: boolean) =>
                  void backups.handleToggleSchedule(
                    schedule.id,
                    schedule.cron,
                    enabled,
                  )
              "
            >
              <template #checked>{{ t("databases.backups.schedules.on") }}</template>
              <template #unchecked>{{ t("databases.backups.schedules.off") }}</template>
            </NSwitch>
            <NButton
              size="small"
              secondary
              :aria-label="t('databases.backups.schedules.editAria', { cron: schedule.cron })"
              @click="backups.openScheduleEdit(schedule)"
            >
              {{ t("common.actions.edit") }}
            </NButton>
            <NPopconfirm
              @positive-click="
                () => void backups.handleDeleteSchedule(schedule.id)
              "
            >
              <template #trigger>
                <NButton
                  size="small"
                  type="error"
                  ghost
                  :aria-label="t('databases.backups.schedules.deleteAria', { cron: schedule.cron })"
                >
                  {{ t("common.actions.delete") }}
                </NButton>
              </template>
              {{ t("databases.backups.schedules.deleteConfirm") }}
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!backups.backupsStore.schedulesLoading"
        :description="t('databases.backups.schedules.empty')"
      />
    </NSpin>
    <div class="schedule-form">
      <NText strong>
        {{ backups.editingScheduleId.value === null ? t("databases.backups.schedules.newTitle") : t("databases.backups.schedules.editTitle") }}
      </NText>
      <NSpace align="center" :size="8">
        <NButton
          v-for="preset in CRON_PRESETS"
          :key="preset"
          size="small"
          secondary
          @click="backups.scheduleCron.value = preset"
        >
          {{ preset }}
        </NButton>
      </NSpace>
      <div class="schedule-form__row">
        <NInput
          v-model:value="backups.scheduleCron.value"
          class="mono grow"
          placeholder="0 2 * * *"
          :aria-label="t('databases.backups.schedules.cronAria')"
        />
        <NSelect
          v-model:value="backups.scheduleTargetId.value"
          :options="backups.scheduleTargetOptions.value"
          :placeholder="t('databases.backups.runs.destination')"
          :aria-label="t('databases.backups.schedules.destinationAria')"
          style="width: 220px"
        />
        <NSwitch
          v-model:value="backups.scheduleEnabled.value"
          :aria-label="t('databases.backups.schedules.enableNewAria')"
        >
          <template #checked>{{ t("databases.backups.schedules.on") }}</template>
          <template #unchecked>{{ t("databases.backups.schedules.off") }}</template>
        </NSwitch>
        <NButton
          type="primary"
          :loading="backups.backupsStore.schedulesActing"
          :disabled="!isCronPresent(backups.scheduleCron.value)"
          @click="() => void backups.handleCreateSchedule()"
        >
          {{
            backups.editingScheduleId.value === null ? t("databases.backups.schedules.add") : t("databases.backups.schedules.save")
          }}
        </NButton>
        <NButton
          v-if="backups.editingScheduleId.value !== null"
          @click="backups.resetScheduleForm()"
        >
          {{ t("common.actions.cancel") }}
        </NButton>
      </div>
    </div>
  </NCard>
</template>

<style scoped src="./backupRows.css">
</style>

<style scoped>
.schedule-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px dashed var(--border);
}

.schedule-form__row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}
</style>
