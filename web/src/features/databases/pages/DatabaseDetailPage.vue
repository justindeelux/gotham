<script setup lang="ts">
import {
  NAlert,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
} from "naive-ui";
import { provide, ref, watch } from "vue";

import DatabaseBackupsTab from "@/features/databases/components/DatabaseBackupsTab.vue";
import DatabaseDetailHeader from "@/features/databases/components/DatabaseDetailHeader.vue";
import DatabaseOverviewPanel from "@/features/databases/components/DatabaseOverviewPanel.vue";
import RenameDatabaseDialog from "@/features/databases/components/RenameDatabaseDialog.vue";
import RestoreConfirmDialog from "@/features/databases/components/RestoreConfirmDialog.vue";
import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import ResourceMoveCard from "@/features/projects/components/ResourceMoveCard.vue";
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
    <ProjectBreadcrumb
      v-if="detail.database.value"
      :project-name="detail.database.value.project_name"
      :project-id="detail.database.value.project_id"
      :environment-name="detail.database.value.environment_name"
      :environment-id="detail.database.value.environment_id"
      :resource-name="detail.database.value.name"
    />
    <nav v-else class="breadcrumb" aria-label="Breadcrumb">
      <span class="muted mono">{{ detail.shortId.value }}</span>
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

        <NTabPane v-if="detail.canWrite.value" name="settings" tab="Settings">
          <ResourceMoveCard
            v-if="detail.database.value"
            :project-id="detail.database.value.project_id"
            :environment-id="detail.database.value.environment_id"
            :server-id="detail.database.value.server_id"
            :saving="detail.moveSaving.value"
            :error="detail.moveError.value"
            @save="detail.handleMove"
          />
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
