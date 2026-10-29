<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NSpace, NTabPane, NTabs } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import ApplicationDeleteDialog from "@/features/applications/components/ApplicationDeleteDialog.vue";
import ApplicationDeploymentsTab from "@/features/applications/components/ApplicationDeploymentsTab.vue";
import ApplicationEnvTab from "@/features/applications/components/ApplicationEnvTab.vue";
import ApplicationHeader from "@/features/applications/components/ApplicationHeader.vue";
import ApplicationImagePanel from "@/features/applications/components/ApplicationImagePanel.vue";
import ApplicationLogsTab from "@/features/applications/components/ApplicationLogsTab.vue";
import ApplicationOverviewTab from "@/features/applications/components/ApplicationOverviewTab.vue";
import ApplicationPreviewsTab from "@/features/applications/components/ApplicationPreviewsTab.vue";
import ApplicationStorageTab from "@/features/applications/components/ApplicationStorageTab.vue";
import ApplicationTerminalTab from "@/features/applications/components/ApplicationTerminalTab.vue";
import DomainEditor from "@/features/applications/components/DomainEditor.vue";
import RollbackDialog from "@/features/applications/components/RollbackDialog.vue";
import { useApplicationDetail } from "@/features/applications/composables/useApplicationDetail";
import ResourceHistoryPanel from "@/features/monitoring/components/ResourceHistoryPanel.vue";
import { useApplicationTerminalAccess } from "@/features/applications/composables/useApplicationTerminalAccess";
import ResourceDetailFrame from "@/features/projects/components/ResourceDetailFrame.vue";
import ResourceSettingsTab from "@/features/projects/components/ResourceSettingsTab.vue";

const detail = useApplicationDetail();

const { t } = useI18n();

const {
  accessKnown,
  accessError,
  retryAccess,
  canWrite,
  showTab: showTerminalTab,
  blocked: blockedTerminal,
  resolveTab,
} = useApplicationTerminalAccess(
  () => detail.application.value !== null,
);

const deploymentsTab = computed<string>(() =>
  String(t("applications.tabs.deployments", { count: detail.deployments.value.length })),
);

const previewsTab = computed<string>(() =>
  detail.previewsLoaded.value
    ? String(t("applications.tabs.previews", { count: detail.previews.value.length }))
    : String(t("applications.tabs.previewsPlain")),
);

const showStorageTab = computed<boolean>(
  () => detail.application.value !== null,
);

const pendingChanges = computed<string[]>(
  () => detail.application.value?.pending_changes ?? [],
);

const pendingChangeLabel = computed<string>(() =>
  pendingChanges.value
    .map((change) => String(t(`applications.detail.pendingChanges.${change}`)))
    .join(", "),
);

const activeTab = computed<string>({
  get: () => {
    const selected =
      detail.activeTab.value === "storage" && !showStorageTab.value
        ? "overview"
        : detail.activeTab.value;
    return resolveTab(selected);
  },
  set: (value: string) => {
    detail.activeTab.value = value;
  },
});
</script>

<template>
  <ResourceDetailFrame
    :loading="detail.appsStore.loading"
    :error="detail.appsStore.error"
    :breadcrumb-label="t('applications.page.breadcrumb')"
    :fallback-id="detail.shortId.value || detail.appId.value"
    :breadcrumbs="
      detail.application.value
        ? {
            projectName: detail.application.value.project_name,
            projectId: detail.application.value.project_id,
            environmentName: detail.application.value.environment_name,
            environmentId: detail.application.value.environment_id,
            resourceName: detail.application.value.name,
          }
        : null
    "
  >
    <template #banner>
      <NAlert
        v-if="blockedTerminal"
        type="info"
        :show-icon="true"
      >
        <template #header>
          {{ t("applications.terminal.notAllowedTitle") }}
        </template>
        {{ t("applications.terminal.notAllowedBody") }}
      </NAlert>

      <NAlert
        v-if="pendingChanges.length > 0"
        type="warning"
        :show-icon="true"
      >
        <template #header>
          {{ t("applications.detail.redeployTitle") }}
        </template>
        <NSpace align="center" justify="space-between" :size="12">
          <span>
            {{ t("applications.detail.redeployBody", { changes: pendingChangeLabel }) }}
          </span>
          <NButton
            v-if="detail.canWrite.value"
            size="small"
            type="primary"
            :loading="detail.appsStore.acting"
            @click="detail.handleDeploy"
          >
            {{ t("applications.detail.deployNow") }}
          </NButton>
        </NSpace>
      </NAlert>
    </template>

    <template #header>
      <ApplicationHeader
        :display-name="detail.displayName.value"
        :app-id="detail.appId.value"
        :brand="detail.brand.value"
        :latest="detail.latest.value"
        :container-stopped="detail.containerStopped.value"
        :active-deploying="detail.active.value !== null"
        :can-rollback="detail.runningDeployments.value.length > 0"
        :acting="detail.appsStore.acting"
        :control-hint="detail.controlHint.value"
        :container-is-running="detail.containerIsRunning.value"
        :can-write="detail.canWrite.value"
        @deploy="detail.handleDeploy"
        @rollback="detail.openRollback"
        @stop="detail.handleStop"
        @start="detail.handleStart"
      />
    </template>

    <NTabs v-model:value="activeTab" type="line" animated>
      <NTabPane name="overview" :tab="t('applications.tabs.overview')">
        <ApplicationOverviewTab
          :application="detail.application.value"
          :latest="detail.latest.value"
          :deployments="detail.deployments.value"
          :pipeline-steps="detail.pipelineSteps.value"
          :desc-columns="detail.descColumns.value"
          :acting="detail.appsStore.acting"
          :can-write="detail.canWrite.value"
          :monitor-saving="detail.monitorSaving.value"
          :monitor-error="detail.monitorError.value"
          @save-monitoring="detail.handleMonitoringSave"
          @view-logs="detail.showLogsFor(detail.latest.value?.id ?? '')"
          @view-deployments="activeTab = 'deployments'"
          @deploy="detail.handleDeploy"
          @view-all="activeTab = 'deployments'"
          @show-logs="detail.showLogsFor"
          @open-rollback="detail.openRollbackFor"
        />
        <ApplicationImagePanel
          v-if="detail.application.value && detail.application.value.source_type === 'image' && detail.canWrite.value"
          :application="detail.application.value"
        />
      </NTabPane>

      <NTabPane
        v-if="detail.application.value"
        name="metrics"
        :tab="t('monitoring.resourceHistory.tab')"
      >
        <ResourceHistoryPanel
          resource-type="application"
          :resource-id="detail.application.value.id"
        />
      </NTabPane>

      <NTabPane name="deployments" :tab="deploymentsTab">
        <ApplicationDeploymentsTab
          :deployments="detail.deployments.value"
          :loading="detail.appsStore.loading"
          :is-compose="detail.application.value?.source_type === 'compose'"
          @show-logs="detail.showLogsFor"
          @open-rollback="detail.openRollbackFor"
        />
      </NTabPane>

      <NTabPane name="logs" :tab="t('applications.tabs.logs')">
        <ApplicationLogsTab
          :log-server-id="detail.displayedLogServerId.value"
          :log-deployment-id="detail.displayedLogDeploymentId.value"
          :server-options="detail.serverOptions.value"
          :deployment-options="detail.deploymentOptions.value"
          :active-deployment-id="detail.active.value?.id ?? ''"
          :log-target="detail.logTarget.value"
          :effective-log-server-id="detail.effectiveLogServerId.value"
          :application="detail.application.value"
          :runtime-deployment="detail.runtimeDeployment.value"
          @update:log-server-id="detail.logServerId.value = $event"
          @update:log-deployment-id="detail.logDeploymentId.value = $event"
        />
      </NTabPane>

      <NTabPane name="env" :tab="t('applications.tabs.env')">
        <ApplicationEnvTab
          :env-draft="detail.envDraft.value"
          :env-loading="detail.envLoading.value"
          :env-error="detail.envError.value"
          :save-disabled="detail.envLoading.value || detail.envLoadedFor.value !== detail.appId.value"
          :saving="detail.appsStore.savingEnv"
          :inherited="detail.inheritedReady.value ? detail.inheritedVars.value : []"
          :inherited-loading="detail.inheritedLoading.value"
          @update:draft="detail.envDraft.value = $event"
          @save="detail.handleSaveEnv"
          @retry="detail.loadEnv()"
        />
      </NTabPane>

      <NTabPane
        v-if="showTerminalTab"
        name="terminal"
        :tab="t('applications.terminal.tab')"
        display-directive="show"
      >
        <ApplicationTerminalTab
          v-if="detail.application.value"
          class="application-terminal"
          :application-id="detail.application.value.id"
          :application-name="detail.application.value.name"
          :access-known="accessKnown"
          :access-error="accessError"
          :can-write="canWrite"
          :active="activeTab === 'terminal'"
          @retry-access="retryAccess"
        />
      </NTabPane>

      <NTabPane v-if="showStorageTab" name="storage" :tab="t('applications.tabs.storage')">
        <ApplicationStorageTab
          v-if="detail.application.value"
          :server-id="detail.application.value.server_id"
          :source-type="detail.application.value.source_type"
          :volumes="detail.volumes.value"
          :volumes-loading="detail.volumesLoading.value"
          :volumes-error="detail.volumesError.value"
          :storages-draft="detail.storagesDraft.value"
          :storages-loading="detail.storagesLoading.value"
          :storages-error="detail.storagesError.value"
          :save-disabled="detail.storagesLoading.value || detail.storagesLoadedFor.value !== detail.appId.value"
          :saving="detail.appsStore.savingStorages"
          @update:draft="detail.storagesDraft.value = $event"
          @save="detail.handleSaveStorages"
          @retry="detail.loadStorages()"
          @refresh="detail.loadVolumes()"
        />
      </NTabPane>

      <NTabPane name="domains" :tab="t('applications.tabs.domains')">
        <div style="margin-top: 16px">
          <DomainEditor v-if="detail.application.value" :application="detail.application.value" />
          <NCard v-else>
            <NEmpty :description="t('applications.page.loadingApp')" />
          </NCard>
        </div>
      </NTabPane>

      <NTabPane
        v-if="detail.previewsAvailable.value"
        name="previews"
        :tab="previewsTab"
      >
        <ApplicationPreviewsTab
          :previews="detail.previews.value"
          :previews-loading="detail.previewsLoading.value"
          :previews-loaded="detail.previewsLoaded.value"
          :previews-error="detail.previewsError.value"
          :branch="detail.application.value?.branch ?? ''"
          @refresh="detail.loadPreviews()"
        />
      </NTabPane>

      <NTabPane v-if="detail.canWrite.value" name="settings" :tab="t('applications.tabs.settings')">
        <ResourceSettingsTab
          v-if="detail.application.value"
          :project-id="detail.application.value.project_id"
          :environment-id="detail.application.value.environment_id"
          :server-id="detail.application.value.server_id ?? ''"
          :saving="detail.moveSaving.value"
          :error="detail.moveError.value"
          :danger-title="t('applications.delete.title')"
          :danger-hint="t('applications.delete.hint')"
          :danger-label="t('applications.delete.button')"
          @save="detail.handleMove"
        >
          <template #danger-action>
            <NButton type="error" ghost @click="detail.deleteOpen.value = true">
              {{ t("applications.delete.button") }}
            </NButton>
          </template>
        </ResourceSettingsTab>
      </NTabPane>
    </NTabs>
  </ResourceDetailFrame>

  <RollbackDialog
    :show="detail.rollbackOpen.value"
    :rolling-back="detail.rollingBack.value"
    :rollback-target="detail.rollbackTarget.value"
    :running-deployments="detail.runningDeployments.value"
    :is-compose="detail.application.value?.source_type === 'compose'"
    @update:show="detail.rollbackOpen.value = $event"
    @update:target="detail.rollbackTarget.value = $event"
    @confirm="detail.handleRollback"
  />
  <ApplicationDeleteDialog
    :show="detail.deleteOpen.value"
    :app-name="detail.application.value?.name ?? ''"
    :deleting="detail.deleting.value"
    @update:show="detail.deleteOpen.value = $event"
    @confirm="detail.handleDelete"
  />
</template>

<style scoped>
.application-terminal {
  display: flex;
  flex-direction: column;
  min-height: min(60vh, 34rem);
}
</style>
