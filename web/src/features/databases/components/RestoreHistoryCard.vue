<script setup lang="ts">
import {
  NAlert,
  NCard,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { inject } from "vue";

import { databaseBackupsKey } from "@/features/databases/composables/useDatabaseBackups";
import { statusTagType } from "@/features/databases/utils/backupStatus";
import { relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NCard :title="t('databases.backups.restores.title')">
    <template #header-extra>
      <NText depth="3">{{ t("databases.backups.restores.subtitle") }}</NText>
    </template>
    <NAlert
      v-if="backups.backupsStore.restoresError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ backups.backupsStore.restoresError }}
    </NAlert>
    <NSpin :show="backups.backupsStore.restoresLoading">
      <NSpace vertical :size="12" style="width: 100%">
        <div
          v-for="restore in backups.restores.value"
          :key="restore.id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NTag :type="statusTagType(restore.status)" size="small">
                {{ t(`databases.backups.runStatus.${restore.status}`) }}
              </NTag>
              <NText class="mono" depth="3">
                {{ t("databases.backups.restores.backupRef", { id: restore.backup_id.slice(0, 8) }) }}
              </NText>
            </NSpace>
            <NText
              v-if="restore.error"
              type="error"
              class="backup-row__error"
            >
              {{ restore.error }}
            </NText>
            <NText depth="3">
              <span :title="restore.created_at">
                {{ relativeTime(restore.created_at) }}
              </span>
              <span v-if="restore.finished_at">
                · {{ t("databases.backups.restores.finished") }}
                <span :title="restore.finished_at">
                  {{ relativeTime(restore.finished_at) }}
                </span>
              </span>
            </NText>
          </div>
        </div>
      </NSpace>
    </NSpin>
  </NCard>
</template>

<style scoped src="./backupRows.css">
</style>
