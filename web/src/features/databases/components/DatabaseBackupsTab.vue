<script setup lang="ts">
import { NSpace } from "naive-ui";
import { computed, inject } from "vue";

import BackupRunsCard from "@/features/databases/components/BackupRunsCard.vue";
import BackupProtectionCard from "@/features/databases/components/BackupProtectionCard.vue";
import BackupSchedulesCard from "@/features/databases/components/BackupSchedulesCard.vue";
import BackupTargetsCard from "@/features/databases/components/BackupTargetsCard.vue";
import RestoreHistoryCard from "@/features/databases/components/RestoreHistoryCard.vue";
import { databaseBackupsKey } from "@/features/databases/composables/useDatabaseBackups";

const backups = inject(databaseBackupsKey)!;

const protection = computed(() =>
  backups.backupsStore.protectionOf(backups.dbId.value),
);

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

function targetHref(targetId: string): string {
  return `#backup-target-${targetId}`;
}
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <BackupProtectionCard
      :summary="protection"
      :loading="backups.backupsStore.protectionLoading"
      :error="backups.backupsStore.protectionError"
      :target-label="backups.targetLabel"
      :target-href="targetHref"
      :run-href="runHref"
    />
    <BackupRunsCard />
    <RestoreHistoryCard v-if="backups.restores.value.length > 0" />
    <BackupSchedulesCard />
    <BackupTargetsCard />
  </NSpace>
</template>
