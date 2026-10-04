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
import { relativeTime } from "@/shared/utils/format";

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NCard title="Schedules">
    <template #header-extra>
      <NText depth="3">Cron in the control plane</NText>
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
              Next run
              <span :title="schedule.next_run_at">
                {{ relativeTime(schedule.next_run_at) }}
              </span>
              <span v-if="schedule.last_run_at">
                · last
                <span :title="schedule.last_run_at">
                  {{ relativeTime(schedule.last_run_at) }}
                </span>
              </span>
            </NText>
          </div>
          <NSpace class="backup-row__actions" align="center" :size="8">
            <NSwitch
              :value="schedule.enabled"
              :aria-label="`Enable schedule ${schedule.cron}`"
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
              <template #checked>On</template>
              <template #unchecked>Off</template>
            </NSwitch>
            <NButton
              size="small"
              secondary
              :aria-label="`Edit schedule ${schedule.cron}`"
              @click="backups.openScheduleEdit(schedule)"
            >
              Edit
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
                  :aria-label="`Delete schedule ${schedule.cron}`"
                >
                  Delete
                </NButton>
              </template>
              Delete this schedule? Past backups stay untouched.
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!backups.backupsStore.schedulesLoading"
        description="No schedules yet — automatic backups are off"
      />
    </NSpin>
    <div class="schedule-form">
      <NText strong>
        {{ backups.editingScheduleId.value === null ? "New schedule" : "Edit schedule" }}
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
          aria-label="Cron expression"
        />
        <NSelect
          v-model:value="backups.scheduleTargetId.value"
          :options="backups.scheduleTargetOptions.value"
          placeholder="Destination"
          aria-label="Schedule destination"
          style="width: 220px"
        />
        <NSwitch
          v-model:value="backups.scheduleEnabled.value"
          aria-label="Enable the new schedule"
        >
          <template #checked>On</template>
          <template #unchecked>Off</template>
        </NSwitch>
        <NButton
          type="primary"
          :loading="backups.backupsStore.schedulesActing"
          :disabled="backups.scheduleCron.value.trim() === ''"
          @click="() => void backups.handleCreateSchedule()"
        >
          {{
            backups.editingScheduleId.value === null ? "Add schedule" : "Save schedule"
          }}
        </NButton>
        <NButton
          v-if="backups.editingScheduleId.value !== null"
          @click="backups.resetScheduleForm()"
        >
          Cancel
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
