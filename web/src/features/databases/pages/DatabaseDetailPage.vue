<script setup lang="ts">
import {
  NAlert,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
} from "naive-ui";
import { provide, ref, watch } from "vue";
import { RouterLink } from "vue-router";

import DatabaseBackupsTab from "@/features/databases/components/DatabaseBackupsTab.vue";
import DatabaseDetailHeader from "@/features/databases/components/DatabaseDetailHeader.vue";
import DatabaseOverviewPanel from "@/features/databases/components/DatabaseOverviewPanel.vue";
import RenameDatabaseDialog from "@/features/databases/components/RenameDatabaseDialog.vue";
import RestoreConfirmDialog from "@/features/databases/components/RestoreConfirmDialog.vue";
import {
  databaseDetailKey,
  useDatabaseDetail,
} from "@/features/databases/composables/useDatabaseDetail";
import {
  databaseBackupsKey,
  useDatabaseBackups,
} from "@/features/databases/composables/useDatabaseBackups";

const activeTab = ref("overview");

const detail = useDatabaseDetail();
const backups = useDatabaseBackups(detail.dbId, activeTab);

provide(databaseDetailKey, detail);
provide(databaseBackupsKey, backups);

watch(detail.dbId, () => {
  activeTab.value = "overview";
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <RouterLink to="/databases">Databases</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted mono">{{ detail.database.value?.name ?? detail.shortId.value }}</span>
    </nav>

    <NSpin :show="detail.pageLoading.value">
      <NAlert
        v-if="detail.pageError.value"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ detail.pageError.value }}
      </NAlert>
      <NAlert
        v-else-if="!detail.pageLoading.value && !detail.database.value"
        type="warning"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        Database not found. It may have been deleted or belong to another
        account.
      </NAlert>

      <DatabaseDetailHeader />

      <NTabs v-model:value="activeTab" type="line" animated>
        <NTabPane name="overview" tab="Overview">
          <DatabaseOverviewPanel />
        </NTabPane>

        <NTabPane name="backups" tab="Backups">
          <DatabaseBackupsTab />
        </NTabPane>
      </NTabs>
    </NSpin>

    <RenameDatabaseDialog />
    <RestoreConfirmDialog />
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}
</style>
