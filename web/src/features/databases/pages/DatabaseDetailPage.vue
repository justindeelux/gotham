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

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

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
        {{ t("databases.errors.databaseNotFound") }}
      </NAlert>

      <DatabaseDetailHeader />

      <NTabs v-model:value="activeTab" type="line" animated>
        <NTabPane name="overview" :tab="t('databases.detail.tabs.overview')">
          <DatabaseOverviewPanel />
        </NTabPane>

        <NTabPane name="backups" :tab="t('databases.detail.tabs.backups')">
          <DatabaseBackupsTab />
        </NTabPane>

        <NTabPane v-if="detail.canWrite.value" name="settings" :tab="t('databases.detail.tabs.settings')">
          <ResourceMoveCard
            v-if="detail.database.value"
            :project-id="detail.database.value.project_id"
            :environment-id="detail.database.value.environment_id"
            :server-id="detail.database.value.server_id"
            :saving="detail.moveSaving.value"
            :error="detail.moveError.value"
            server-pinned
            :server-pinned-reason="t('databases.detail.serverPinnedReason')"
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
