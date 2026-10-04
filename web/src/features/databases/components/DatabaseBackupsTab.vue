<script setup lang="ts">
import { NSpace } from "naive-ui";
import { inject } from "vue";

import BackupRunsCard from "@/features/databases/components/BackupRunsCard.vue";
import BackupSchedulesCard from "@/features/databases/components/BackupSchedulesCard.vue";
import BackupTargetsCard from "@/features/databases/components/BackupTargetsCard.vue";
import RestoreHistoryCard from "@/features/databases/components/RestoreHistoryCard.vue";
import { databaseBackupsKey } from "@/features/databases/composables/useDatabaseBackups";

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <BackupRunsCard />
    <RestoreHistoryCard v-if="backups.restores.value.length > 0" />
    <BackupSchedulesCard />
    <BackupTargetsCard />
  </NSpace>
</template>
