<script setup lang="ts">
import { NAlert, NButton, NModal, NSpace, NText } from "naive-ui";
import { computed, inject } from "vue";

import { resourceBackupsKey } from "@/features/backups/composables/useResourceBackups";
import { formatBytes, relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const props = withDefaults(defineProps<{ resourceLabel?: string }>(), {
  resourceLabel: "",
});

const backups = inject(resourceBackupsKey)!;

const warningByType = computed<Record<string, string>>(() => ({
  database: t("backups.restore.warningDatabase"),
  application: t("backups.restore.warningApplication"),
  service: t("backups.restore.warningService"),
}));

const resourceLabel = computed<string>(
  () =>
    props.resourceLabel ||
    t("backups.resources.shortId", {
      id: backups.resource.value.resource_id.slice(0, 8),
    }),
);

const restorePrompt = computed<string>(() =>
  t("backups.restore.prompt", {
    name: resourceLabel.value,
    backup:
      backups.restoreCandidate.value?.location ||
      backups.restoreCandidate.value?.id ||
      "",
    when: relativeTime(backups.restoreCandidate.value?.created_at),
    size: formatBytes(backups.restoreCandidate.value?.size ?? 0),
  }),
);

// A backup without a checksum predates integrity checks: it still restores,
// but the dialog must not promise a verification that will not run.
const integrityNotice = computed<string>(() =>
  backups.restoreCandidate.value?.verified
    ? t("backups.restore.verifiedCheck")
    : t("backups.restore.noChecksum"),
);
</script>

<template>
  <NModal
    v-model:show="backups.restoreOpen.value"
    preset="card"
    class="app-modal"
    :title="t('backups.restore.title')"
    style="width: 520px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NText>{{ restorePrompt }}</NText>
      <NAlert type="info" :show-icon="true">
        {{ integrityNotice }}
      </NAlert>
      <NAlert type="warning" :show-icon="true">
        {{
          warningByType[backups.resource.value.resource_type] ??
          t("backups.restore.warningApplication")
        }}
      </NAlert>
      <NSpace justify="end" :size="8">
        <NButton @click="backups.restoreOpen.value = false">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="warning"
          :loading="backups.restoring.value"
          @click="() => void backups.handleRestoreConfirm()"
        >
          {{ t("backups.restore.confirm") }}
        </NButton>
      </NSpace>
    </NSpace>
  </NModal>
</template>
