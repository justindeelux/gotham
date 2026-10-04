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
import { statusTagType } from "@/features/databases/utils/backupStatus";
import { formatBytes, relativeTime } from "@/shared/utils/format";

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NCard title="Backups">
    <template #header-extra>
      <NSpace align="center" :size="8">
        <NSelect
          v-model:value="backups.backupTargetId.value"
          :options="backups.backupTargetOptions.value"
          placeholder="Destination"
          aria-label="Backup destination"
          style="width: 220px"
        />
        <NButton
          size="small"
          secondary
          :disabled="backups.backupsStore.backupsLoading"
          @click="() => void backups.fetchBackupTab()"
        >
          Refresh
        </NButton>
        <NPopconfirm @positive-click="() => void backups.handleBackupNow()">
          <template #trigger>
            <NButton
              size="small"
              type="primary"
              :loading="backups.backupsStore.backupsActing"
            >
              Backup now
            </NButton>
          </template>
          A backup stops this database while the dump runs, so it is
          briefly unavailable. Continue?
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
      No S3 target configured — backups are stored on the control
      plane disk. Add an S3-compatible target below to keep them
      off-node.
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
                {{ backup.status }}
              </NTag>
              <NTag size="small" :bordered="false">
                {{ backup.type }}
              </NTag>
              <NText class="mono" depth="3">
                {{
                  backup.status === "running"
                    ? "size pending"
                    : formatBytes(backup.size)
                }}
              </NText>
            </NSpace>
            <NText class="mono backup-row__location">
              {{ backup.location || "Dump in progress…" }}
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
                · scheduled
              </span>
            </NText>
          </div>
          <NSpace class="backup-row__actions" align="center" :size="8">
            <NButton
              size="small"
              secondary
              :aria-label="`Restore backup from ${relativeTime(backup.created_at)}`"
              :disabled="backup.status !== 'completed'"
              @click="backups.openRestore(backup)"
            >
              Restore
            </NButton>
            <NPopconfirm
              @positive-click="() => void backups.handleDeleteBackup(backup.id)"
            >
              <template #trigger>
                <NButton
                  size="small"
                  type="error"
                  ghost
                  :aria-label="`Delete backup from ${relativeTime(backup.created_at)}`"
                  :disabled="backup.status === 'running'"
                >
                  Delete
                </NButton>
              </template>
              Delete this backup? The stored artifact goes first,
              then the row. This cannot be undone.
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!backups.backupsStore.backupsLoading"
        description="No backups yet"
      >
        <template #extra>
          <p class="empty-hint">
            Queue a manual backup above, or add a schedule so the
            control plane dumps this database automatically.
          </p>
        </template>
      </NEmpty>
    </NSpin>
  </NCard>
</template>

<style scoped src="./backupRows.css">
</style>
