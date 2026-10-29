<script setup lang="ts">
import { NSpace } from "naive-ui";
import { provide, toRef } from "vue";

import BackupProtectionCard from "@/features/backups/components/BackupProtectionCard.vue";
import BackupRestoreDialog from "@/features/backups/components/BackupRestoreDialog.vue";
import BackupRunsCard from "@/features/backups/components/BackupRunsCard.vue";
import BackupSchedulesCard from "@/features/backups/components/BackupSchedulesCard.vue";
import RestoreHistoryCard from "@/features/backups/components/RestoreHistoryCard.vue";
import {
  resourceBackupsKey,
  useResourceBackups,
} from "@/features/backups/composables/useResourceBackups";
import type { ResourceType } from "@/features/backups/api/backups";
import { useAuthStore } from "@/features/auth";
import { i18n } from "@/shared/i18n";

const props = withDefaults(
  defineProps<{
    resourceType: ResourceType;
    resourceId: string;
    active?: boolean;
    resourceLabel?: string;
  }>(),
  { active: true, resourceLabel: "" },
);

const backups = useResourceBackups(
  toRef(props, "resourceType"),
  toRef(props, "resourceId"),
  toRef(props, "active"),
);
provide(resourceBackupsKey, backups);

const authStore = useAuthStore();

function t(key: string): string {
  return String(i18n.global.t(key));
}
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <BackupProtectionCard
      :summary="backups.protection.value"
      :loading="backups.backupsStore.protectionLoading"
      :error="backups.backupsStore.protectionError"
      :target-label="backups.providerLabel"
      :run-href="backups.runHref"
    />
    <BackupRunsCard />
    <RestoreHistoryCard v-if="backups.restores.value.length > 0" />
    <BackupSchedulesCard />
    <p v-if="authStore.isOwner" class="storage-link">
      {{ t("backups.resourceTab.storageHint") }}
      <RouterLink :to="{ name: 'backups', query: { tab: 'storage' } }">
        {{ t("backups.resourceTab.openStorage") }}
      </RouterLink>
    </p>
    <BackupRestoreDialog :resource-label="resourceLabel" />
  </NSpace>
</template>

<style scoped>
.storage-link {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0;
}
</style>
