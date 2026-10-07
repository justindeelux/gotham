<script setup lang="ts">
import { NAlert, NButton, NModal, NSpace, NText } from "naive-ui";
import { computed, inject } from "vue";

import { databaseBackupsKey } from "@/features/databases/composables/useDatabaseBackups";
import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import { formatBytes, relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const detail = inject(databaseDetailKey)!;
const backups = inject(databaseBackupsKey)!;

/** restorePrompt keeps the database/backup identity inside one sentence. */
const restorePrompt = computed<string>(() =>
  String(
    t("databases.backups.dialog.prompt", {
      name: detail.database.value?.name ?? detail.shortId.value,
      backup:
        (backups.restoreCandidate.value?.location ||
          backups.restoreCandidate.value?.id) ??
        "",
      when: relativeTime(backups.restoreCandidate.value?.created_at),
      size: formatBytes(backups.restoreCandidate.value?.size ?? 0),
    }),
  ),
);
</script>

<template>
  <NModal
    v-model:show="backups.restoreOpen.value"
    preset="card"
    :title="t('databases.backups.dialog.title')"
    style="width: 520px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NText>
        {{ restorePrompt }}
      </NText>
      <NAlert type="warning" :show-icon="true">
        {{ t("databases.backups.dialog.warning") }}
      </NAlert>
      <NSpace justify="end" :size="8">
        <NButton @click="backups.restoreOpen.value = false">{{ t("common.actions.cancel") }}</NButton>
        <NButton
          type="warning"
          :loading="backups.restoring.value"
          @click="() => void backups.handleRestoreConfirm()"
        >
          {{ t("databases.backups.dialog.confirm") }}
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
