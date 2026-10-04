<script setup lang="ts">
import { NAlert, NButton, NModal, NSpace, NText } from "naive-ui";
import { inject } from "vue";

import { databaseBackupsKey } from "@/features/databases/composables/useDatabaseBackups";
import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import { formatBytes, relativeTime } from "@/shared/utils/format";

const detail = inject(databaseDetailKey)!;
const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NModal
    v-model:show="backups.restoreOpen.value"
    preset="card"
    title="Restore database"
    style="width: 520px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NText>
        Restore
        <NText strong class="mono">{{ detail.database.value?.name ?? detail.shortId.value }}</NText>
        from the backup
        <NText strong class="mono">
          {{ backups.restoreCandidate.value?.location || backups.restoreCandidate.value?.id }}
        </NText>
        ({{ relativeTime(backups.restoreCandidate.value?.created_at) }},
        {{ formatBytes(backups.restoreCandidate.value?.size ?? 0) }})?
      </NText>
      <NAlert type="warning" :show-icon="true">
        The restore runs in a temporary container and overwrites the current
        data of this database. This cannot be undone — back up first if the
        live data still matters.
      </NAlert>
      <NSpace justify="end" :size="8">
        <NButton @click="backups.restoreOpen.value = false">Cancel</NButton>
        <NButton
          type="warning"
          :loading="backups.restoring.value"
          @click="() => void backups.handleRestoreConfirm()"
        >
          Restore · overwrite data
        </NButton>
      </NSpace>
    </NSpace>
  </NModal>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
