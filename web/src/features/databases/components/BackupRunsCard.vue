<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { inject } from "vue";

import type { DatabaseBackup } from "@/features/databases/api/backups";
import { databaseBackupsKey } from "@/features/databases/composables/useDatabaseBackups";
import { runDisplay, statusTagType } from "@/features/databases/utils/backupStatus";
import { formatBytes, relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NCard :title="t('databases.backups.runs.title')">
    <template #header-extra>
      <NSpace align="center" :size="8">
        <NSelect
          v-model:value="backups.backupTargetId.value"
          :options="backups.backupTargetOptions.value"
          :placeholder="t('databases.backups.runs.destination')"
          :aria-label="t('databases.backups.runs.destinationAria')"
          style="width: 220px"
        />
        <NButton
          size="small"
          secondary
          :disabled="backups.backupsStore.backupsLoading"
          @click="() => void backups.fetchBackupTab()"
        >
          {{ t("databases.backups.runs.refresh") }}
        </NButton>
        <NPopconfirm @positive-click="() => void backups.handleBackupNow()">
          <template #trigger>
            <NButton
              size="small"
              type="primary"
              :loading="backups.backupsStore.backupsActing"
            >
              {{ t("databases.backups.runs.backupNow") }}
            </NButton>
          </template>
          {{ t("databases.backups.runs.backupConfirm") }}
        </NPopconfirm>
      </NSpace>
    </template>
    <NAlert
      v-if="backups.backupsStore.backupsError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ backups.backupsStore.backupsError }}
    </NAlert>
    <NAlert
      v-if="backups.backupsStore.targets.length === 0"
      type="info"
      :show-icon="false"
      style="margin-bottom: 12px"
    >
      {{ t("databases.backups.runs.noTarget") }}
    </NAlert>
    <NSpin :show="backups.backupsStore.backupsLoading">
      <NSpace
        v-if="backups.backups.value.length > 0"
        vertical
        :size="12"
        style="width: 100%"
      >
        <div
          v-for="backup in (backups.backups.value as DatabaseBackup[])"
          :key="backup.id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NTag :type="statusTagType(backup.status)" size="small">
                {{ runDisplay("runStatus", backup.status) }}
              </NTag>
              <NTag size="small" :bordered="false">
                {{ runDisplay("runType", backup.type) }}
              </NTag>
              <NText class="mono" depth="3">
                {{
                  backup.status === "running"
                    ? t("databases.backups.runs.sizePending")
                    : formatBytes(backup.size)
                }}
              </NText>
            </NSpace>
            <NText class="mono backup-row__location">
              {{ backup.location || t("databases.backups.runs.dumpInProgress") }}
            </NText>
            <NText
              v-if="backup.error"
              type="error"
              class="backup-row__error"
            >
              {{ backup.error }}
            </NText>
            <NText depth="3">
              <span :title="backup.created_at">
                {{ relativeTime(backup.created_at) }}
              </span>
              <span v-if="backup.schedule_id" class="mono">
                · {{ t("databases.backups.runs.scheduled") }}
              </span>
            </NText>
          </div>
          <NSpace class="backup-row__actions" align="center" :size="8">
            <NButton
              size="small"
              secondary
              :aria-label="t('databases.backups.runs.restoreAria', { when: relativeTime(backup.created_at) })"
              :disabled="backup.status !== 'completed'"
              @click="backups.openRestore(backup)"
            >
              {{ t("databases.backups.runs.restore") }}
            </NButton>
            <NPopconfirm
              @positive-click="() => void backups.handleDeleteBackup(backup.id)"
            >
              <template #trigger>
                <NButton
                  size="small"
                  type="error"
                  ghost
                  :aria-label="t('databases.backups.runs.deleteAria', { when: relativeTime(backup.created_at) })"
                  :disabled="backup.status === 'running'"
                >
                  {{ t("common.actions.delete") }}
                </NButton>
              </template>
              {{ t("databases.backups.runs.deleteConfirm") }}
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!backups.backupsStore.backupsLoading"
        :description="t('databases.backups.runs.empty')"
      >
        <template #extra>
          <p class="empty-hint">
            {{ t("databases.backups.runs.emptyHint") }}
          </p>
        </template>
      </NEmpty>
    </NSpin>
  </NCard>
</template>

<style scoped src="./backupRows.css">
</style>
