<script setup lang="ts">
import { NButton, NCard, NSpace, NTabPane, NTabs, NTooltip } from "naive-ui";
import { provide, ref, watch } from "vue";
import { useRoute } from "vue-router";

import ResourceBackupsTab from "@/features/backups/components/ResourceBackupsTab.vue";
import DatabaseDetailHeader from "@/features/databases/components/DatabaseDetailHeader.vue";
import DatabaseLogsTab from "@/features/databases/components/DatabaseLogsTab.vue";
import DatabaseProvisioningBanner from "@/features/databases/components/DatabaseProvisioningBanner.vue";
import DatabaseOverviewPanel from "@/features/databases/components/DatabaseOverviewPanel.vue";
import ResourceHistoryPanel from "@/features/monitoring/components/ResourceHistoryPanel.vue";
import RenameDatabaseDialog from "@/features/databases/components/RenameDatabaseDialog.vue";
import ResourceDetailFrame from "@/features/projects/components/ResourceDetailFrame.vue";
import ResourceSettingsTab from "@/features/projects/components/ResourceSettingsTab.vue";
import RuntimeHealthPanel from "@/shared/components/RuntimeHealthPanel.vue";
import {
  databaseDetailKey,
  useDatabaseDetail,
} from "@/features/databases/composables/useDatabaseDetail";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const route = useRoute();
const activeTab = ref(
  route.query.tab === "backups" ? "backups" : "overview",
);

const detail = useDatabaseDetail();

provide(databaseDetailKey, detail);

watch(detail.dbId, () => {
  activeTab.value = route.query.tab === "backups" ? "backups" : "overview";
});
</script>

<template>
  <ResourceDetailFrame
    :loading="detail.pageLoading.value"
    :error="detail.pageError.value"
    :not-found="!detail.pageLoading.value && !detail.database.value && detail.pageError.value === null"
    :not-found-text="t('databases.errors.databaseNotFound')"
    :breadcrumb-label="t('databases.detail.breadcrumb')"
    :fallback-id="detail.shortId.value"
    :breadcrumbs="
      detail.database.value
        ? {
            projectName: detail.database.value.project_name,
            projectId: detail.database.value.project_id,
            environmentName: detail.database.value.environment_name,
            environmentId: detail.database.value.environment_id,
            resourceName: detail.database.value.name,
          }
        : null
    "
  >
    <template #banner>
      <DatabaseProvisioningBanner
        v-if="detail.database.value"
        :status="detail.database.value.status"
        :status-message="detail.database.value.status_message"
      />
    </template>

    <template #header>
      <DatabaseDetailHeader />
    </template>

    <NTabs v-model:value="activeTab" type="line" animated>
      <NTabPane name="overview" :tab="t('databases.detail.tabs.overview')">
        <DatabaseOverviewPanel />
      </NTabPane>

      <NTabPane
        v-if="detail.database.value"
        name="metrics"
        :tab="t('monitoring.resourceHistory.tab')"
      >
        <ResourceHistoryPanel
          resource-type="database"
          :resource-id="detail.database.value.id"
        />
      </NTabPane>

      <NTabPane name="backups" :disabled="detail.isCreating.value">
        <template #tab>
          <NTooltip :disabled="!detail.isCreating.value">
            <template #trigger>
              <span>{{ t("databases.detail.tabs.backups") }}</span>
            </template>
            {{ t("databases.detail.creating.backupsDisabled") }}
          </NTooltip>
        </template>
        <ResourceBackupsTab
          v-if="detail.database.value"
          resource-type="database"
          :resource-id="detail.database.value.id"
          :resource-label="detail.database.value.name"
          :active="activeTab === 'backups'"
        />
      </NTabPane>

      <NTabPane
        v-if="detail.database.value"
        name="logs"
        :tab="t('databases.detail.tabs.logs')"
      >
        <DatabaseLogsTab
          :database="detail.database.value"
          :active="activeTab === 'logs'"
        />
      </NTabPane>

      <NTabPane v-if="detail.canWrite.value" name="settings" :tab="t('databases.detail.tabs.settings')">
        <ResourceSettingsTab
          v-if="detail.database.value"
          :project-id="detail.database.value.project_id"
          :environment-id="detail.database.value.environment_id"
          :server-id="detail.database.value.server_id"
          :saving="detail.moveSaving.value"
          :error="detail.moveError.value"
          server-pinned
          :server-pinned-reason="t('databases.detail.serverPinnedReason')"
          :danger-title="t('databases.detail.deleteTitle')"
          :danger-hint="t('databases.detail.deleteHint')"
          :danger-label="t('common.actions.delete')"
          :danger-confirm="t('databases.detail.deleteConfirm', { volume: detail.database.value.volume })"
          :danger-busy="detail.databasesStore.acting"
          @save="detail.handleMove"
          @delete="() => void detail.handleDelete()"
        >
          <RuntimeHealthPanel
            v-if="detail.database.value"
            :health="detail.database.value.runtime_health"
            :can-write="detail.canWrite.value"
            :saving="detail.monitorSaving.value"
            :error="detail.monitorError.value"
            @save="detail.handleMonitoringSave"
          />
          <NCard :title="t('databases.detail.rename.title')">
            <NSpace vertical :size="12">
              <span>{{ t("databases.detail.rename.hint") }}</span>
              <NSpace>
                <NButton @click="detail.openRename">
                  {{ t("databases.detail.actions.rename") }}
                </NButton>
              </NSpace>
            </NSpace>
          </NCard>
        </ResourceSettingsTab>
      </NTabPane>
    </NTabs>
  </ResourceDetailFrame>

  <RenameDatabaseDialog />
</template>
