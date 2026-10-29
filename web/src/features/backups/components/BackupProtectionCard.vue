<script setup lang="ts">
import { NAlert, NCard, NEmpty, NSpace, NSpin, NTag, NText } from "naive-ui";
import { inject } from "vue";

import type { BackupProtectionStatus } from "@/features/backups/api/backups";
import { resourceBackupsKey } from "@/features/backups/composables/useResourceBackups";
import { relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const backups = inject(resourceBackupsKey)!;

function statusType(
  status: BackupProtectionStatus,
): "default" | "success" | "info" | "warning" | "error" {
  switch (status) {
    case "protected":
      return "success";
    case "overdue":
      return "warning";
    case "stuck":
      return "error";
    case "running":
    case "pending":
    case "scheduled":
      return "info";
    default:
      return "default";
  }
}

function durationLabel(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return t("databases.backups.protection.justNow");
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return t("databases.backups.protection.minutes", { count: minutes });
  }
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0
    ? t("databases.backups.protection.hours", { count: hours })
    : t("databases.backups.protection.hoursMinutes", { hours, minutes: rest });
}

function maxRunLabel(seconds: number): string {
  return t("databases.backups.protection.maxRunValue", {
    minutes: Math.round(seconds / 60),
  });
}

// A run older than the loaded page has no element to scroll to; fall back to
// the runs card instead of leaving a dead anchor.
function runHref(backupId?: string): string {
  if (!backupId) {
    return "#backup-runs";
  }
  return backups.backups.value.some((item) => item.id === backupId)
    ? `#backup-run-${backupId}`
    : "#backup-runs";
}
</script>

<template>
  <NCard :title="t('databases.backups.protection.title')">
    <template #header-extra>
      <NText depth="3">{{ t("databases.backups.protection.subtitle") }}</NText>
    </template>
    <NAlert
      v-if="backups.backupsStore.protectionError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ backups.backupsStore.protectionError }}
    </NAlert>
    <NSpin :show="backups.backupsStore.protectionLoading">
      <NAlert
        v-if="
          !backups.backupsStore.protectionError &&
          (backups.protection.value?.status ?? 'not_configured') === 'not_configured'
        "
        type="info"
        :show-icon="false"
      >
        {{ t("databases.backups.protection.notConfigured") }}
      </NAlert>
      <NEmpty
        v-else-if="backups.protection.value && backups.protection.value.schedules.length === 0"
        :description="t('databases.backups.protection.notConfigured')"
      />
      <NSpace v-else-if="backups.protection.value" vertical :size="12" style="width: 100%">
        <div
          v-for="item in backups.protection.value.schedules"
          :key="item.schedule_id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NTag :type="statusType(item.status)" size="small">
                {{
                  t(`databases.backups.protection.status.${item.status}`)
                }}
              </NTag>
              <NText class="mono" strong>{{ item.cron }}</NText>
              <NTag size="small" :bordered="false">
                {{ backups.providerLabel(item.target_id) }}
              </NTag>
            </NSpace>
            <NText depth="3">
              {{ t("databases.backups.protection.lastSuccess") }}
              <a
                v-if="item.last_success_at"
                class="protection-link"
                :href="runHref(item.last_backup_id)"
                :title="item.last_success_at"
              >
                {{ relativeTime(item.last_success_at) }}
              </a>
              <span v-else>{{ t("databases.backups.protection.never") }}</span>
              <span v-if="item.expected_slot_at">
                · {{ t("databases.backups.protection.expectedSlot") }}
                <span :title="item.expected_slot_at">
                  {{ relativeTime(item.expected_slot_at) }}
                </span>
              </span>
            </NText>
            <NText
              v-if="item.status === 'overdue' || item.status === 'stuck'"
              type="error"
            >
              {{ t("databases.backups.protection.overdueFor", {
                duration: durationLabel(item.overdue_seconds),
              }) }}
            </NText>
            <NText v-if="item.running_since" depth="3">
              {{ t("databases.backups.protection.runningSince", {
                time: relativeTime(item.running_since),
              }) }}
              <a
                v-if="item.running_since"
                class="protection-link"
                :href="runHref(item.running_backup_id)"
              >
                {{ t("databases.backups.runs.title") }}
              </a>
            </NText>
            <NText depth="3">
              {{ maxRunLabel(item.max_run_seconds) }}
            </NText>
          </div>
        </div>
      </NSpace>
    </NSpin>
  </NCard>
</template>

<style scoped src="./backupRows.css"></style>

<style scoped>
.protection-link {
  color: var(--link);
  text-decoration: none;
}

.protection-link:hover {
  text-decoration: underline;
}
</style>
