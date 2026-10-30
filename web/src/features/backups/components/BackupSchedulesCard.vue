<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NEmpty,
  NInput,
  NInputGroup,
  NInputGroupLabel,
  NInputNumber,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { computed, inject } from "vue";

import { RESOURCE_BACKUP_CRON_PRESETS, resourceBackupsKey } from "@/features/backups/composables/useResourceBackups";
import { isCronPresent } from "@/features/backups/schemas/backups";
import { describeCron } from "@/features/backups/utils/backupStatus";
import { relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const backups = inject(resourceBackupsKey)!;

const cronFeedbackId = "backup-schedule-cron-feedback";
const cronSummary = computed(() =>
  describeCron(backups.scheduleCron.value, { short: true }),
);
const cronInvalid = computed(
  () =>
    backups.scheduleCron.value.trim() !== "" && cronSummary.value === "",
);

function presetLabel(preset: string): string {
  return describeCron(preset, { short: true });
}

function isPresetActive(preset: string): boolean {
  return backups.scheduleCron.value === preset;
}
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
        v-if="backups.backupsStore.schedulesOf(backups.resource.value).length > 0"
        vertical
        :size="12"
        style="width: 100%"
      >
        <div
          v-for="schedule in backups.backupsStore.schedulesOf(backups.resource.value)"
          :key="schedule.id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NText class="mono" strong>
                {{ schedule.cron }}
              </NText>
              <NTag size="small" :bordered="false">
                {{ backups.providerLabel(schedule.provider_id) }}
              </NTag>
            </NSpace>
            <NText depth="3">{{ describeCron(schedule.cron) }}</NText>
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
              <span>
                · {{ t("databases.backups.protection.maxRunValue", {
                  minutes: Math.round(schedule.max_run_seconds / 60),
                }) }}
              </span>
              <span>
                ·
                {{
                  schedule.keep_last == null
                    ? t("backups.schedules.keepAll")
                    : t("backups.schedules.keepLast", {
                        count: schedule.keep_last,
                      })
                }}
              </span>
            </NText>
            <NTag
              v-if="!schedule.pause_during_backup"
              size="small"
              type="warning"
              :bordered="false"
            >
              {{ t("backups.resourceTab.noPauseTag") }}
            </NTag>
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
      <div class="schedule-form__presets">
        <NButton
          v-for="preset in RESOURCE_BACKUP_CRON_PRESETS"
          :key="preset"
          size="small"
          :type="isPresetActive(preset) ? 'primary' : 'default'"
          :secondary="!isPresetActive(preset)"
          :aria-pressed="isPresetActive(preset)"
          @click="backups.scheduleCron.value = preset"
        >
          {{ presetLabel(preset) }}
        </NButton>
      </div>
      <NText depth="3">{{ t("backups.schedules.keepLastHint") }}</NText>
      <NInputGroup class="schedule-form__cron-group">
        <NInput
          v-model:value="backups.scheduleCron.value"
          class="mono"
          placeholder="0 2 * * *"
          :status="cronInvalid ? 'error' : undefined"
          :aria-label="t('databases.backups.schedules.cronAria')"
          :input-props="{
            'aria-describedby':
              cronSummary !== '' || cronInvalid ? cronFeedbackId : undefined,
          }"
        />
        <NInputGroupLabel class="schedule-form__cron-label">
          <NText
            v-if="cronInvalid"
            :id="cronFeedbackId"
            type="error"
            role="alert"
          >
            {{ t("backups.cron.invalid") }}
          </NText>
          <NText v-else-if="cronSummary" :id="cronFeedbackId" depth="3">
            {{ cronSummary }}
          </NText>
        </NInputGroupLabel>
      </NInputGroup>
      <div class="schedule-form__row">
        <div class="schedule-form__field">
          <NText depth="3">
            {{ t("databases.backups.protection.maxRunLabel") }}
          </NText>
          <NInputNumber
            v-model:value="backups.scheduleMaxRunMinutes.value"
            :min="15"
            :max="1440"
            :step="15"
            style="width: 150px"
            :aria-label="t('databases.backups.protection.maxRunAria')"
          >
            <template #suffix>
              {{ t("databases.backups.protection.minutesSuffix") }}
            </template>
          </NInputNumber>
        </div>
        <div class="schedule-form__field">
          <NText depth="3">{{ t("backups.schedules.keepLastLabel") }}</NText>
          <NInputNumber
            v-model:value="backups.scheduleKeepLast.value"
            :min="1"
            :max="365"
            style="width: 120px"
            :aria-label="t('backups.schedules.keepLastLabel')"
          />
        </div>
        <div class="schedule-form__field">
          <NText depth="3">
            {{ t("databases.backups.runs.destination") }}
          </NText>
          <NSelect
            v-model:value="backups.scheduleProviderId.value"
            :options="backups.scheduleProviderOptions.value"
            :placeholder="t('databases.backups.runs.destination')"
            :aria-label="t('databases.backups.schedules.destinationAria')"
            style="width: 220px"
          />
        </div>
        <div class="schedule-form__field">
          <NText depth="3">
            {{ t("databases.backups.schedules.enableLabel") }}
          </NText>
          <NSwitch
            v-model:value="backups.scheduleEnabled.value"
            :aria-label="t('databases.backups.schedules.enableNewAria')"
          >
            <template #checked>{{ t("databases.backups.schedules.on") }}</template>
            <template #unchecked>{{ t("databases.backups.schedules.off") }}</template>
          </NSwitch>
        </div>
      </div>
      <div
        v-if="backups.resource.value.resource_type !== 'database'"
        class="schedule-form__row schedule-pause"
      >
        <NCheckbox v-model:checked="backups.schedulePauseDuringBackup.value">
          {{ t("backups.schedules.pauseLabel") }}
        </NCheckbox>
        <NText depth="3">
          {{
            backups.schedulePauseDuringBackup.value
              ? t("backups.resourceTab.pauseHelper")
              : t("backups.resourceTab.noPauseHelper")
          }}
        </NText>
      </div>
      <div class="schedule-form__actions">
        <NButton
          type="primary"
          :loading="backups.backupsStore.schedulesActing"
          :disabled="!isCronPresent(backups.scheduleCron.value) || cronInvalid"
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

.schedule-form__presets {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.schedule-form__row {
  display: flex;
  align-items: flex-end;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.schedule-form__field {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.schedule-form__cron-group {
  width: 100%;
}

.schedule-form__cron-label {
  max-width: 55%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.schedule-form__actions {
  display: flex;
  gap: var(--space-2);
}

@media (max-width: 640px) {
  .schedule-form__cron-group {
    flex-direction: column;
  }

  .schedule-form__cron-label {
    width: 100%;
    max-width: none;
    justify-content: flex-start;
    border-top: 1px solid var(--border);
    border-left: 0;
  }
}

.schedule-pause {
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-1);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-2);
}
</style>
