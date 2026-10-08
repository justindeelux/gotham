<script setup lang="ts">
import { NAlert, NCard, NEmpty, NSpace, NSpin, NTabPane, NTabs } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import ApplicationDeploymentsTab from "@/features/applications/components/ApplicationDeploymentsTab.vue";
import ApplicationEnvTab from "@/features/applications/components/ApplicationEnvTab.vue";
import ApplicationHeader from "@/features/applications/components/ApplicationHeader.vue";
import ApplicationLogsTab from "@/features/applications/components/ApplicationLogsTab.vue";
import ApplicationImagePanel from "@/features/applications/components/ApplicationImagePanel.vue";
import ApplicationOverviewTab from "@/features/applications/components/ApplicationOverviewTab.vue";
import ApplicationPreviewsTab from "@/features/applications/components/ApplicationPreviewsTab.vue";
import ApplicationStorageTab from "@/features/applications/components/ApplicationStorageTab.vue";
import DomainEditor from "@/features/applications/components/DomainEditor.vue";
import RollbackDialog from "@/features/applications/components/RollbackDialog.vue";
import { useApplicationDetail } from "@/features/applications/composables/useApplicationDetail";
import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import ResourceMoveCard from "@/features/projects/components/ResourceMoveCard.vue";

const detail = useApplicationDetail();

const { t } = useI18n();

/** deploymentsTab names the deployment count; 0/1/many all render. */
const deploymentsTab = computed<string>(() =>
  String(t("applications.tabs.deployments", { count: detail.deployments.value.length })),
);

/** previewsTab names the preview count once loaded; 0/1/many all render. */
const previewsTab = computed<string>(() =>
  detail.previewsLoaded.value
    ? String(t("applications.tabs.previews", { count: detail.previews.value.length }))
    : String(t("applications.tabs.previewsPlain")),
);
</script>

<template>
  <NSpace vertical :size="16">
    <ProjectBreadcrumb
      v-if="detail.application.value"
      :project-name="detail.application.value.project_name"
      :project-id="detail.application.value.project_id"
      :environment-name="detail.application.value.environment_name"
      :environment-id="detail.application.value.environment_id"
      :resource-name="detail.application.value.name"
    />
    <nav v-else class="breadcrumb" :aria-label="t('applications.page.breadcrumb')">
      <span class="muted mono">{{ detail.shortId.value || detail.appId.value }}</span>
    </nav>

    <NSpin :show="detail.appsStore.loading">
      <NAlert
        v-if="detail.appsStore.error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ detail.appsStore.error }}
      </NAlert>

      <ApplicationHeader
        :display-name="detail.displayName.value"
        :app-id="detail.appId.value"
        :short-id="detail.shortId.value"
        :initials="detail.initials.value"
        :latest="detail.latest.value"
        :container-stopped="detail.containerStopped.value"
        :active-deploying="detail.active.value !== null"
        :can-rollback="detail.runningDeployments.value.length > 0"
        :acting="detail.appsStore.acting"
        :control-hint="detail.controlHint.value"
        :container-is-running="detail.containerIsRunning.value"
        @deploy="detail.handleDeploy"
        @rollback="detail.openRollback"
        @stop="detail.handleStop"
        @start="detail.handleStart"
      />

      <NTabs v-model:value="detail.activeTab.value" type="line" animated>
        <NTabPane name="overview" :tab="t('applications.tabs.overview')">
          <ApplicationOverviewTab
            :application="detail.application.value"
            :latest="detail.latest.value"
            :deployments="detail.deployments.value"
            :pipeline-steps="detail.pipelineSteps.value"
            :desc-columns="detail.descColumns.value"
            :acting="detail.appsStore.acting"
            @deploy="detail.handleDeploy"
            @view-all="detail.activeTab.value = 'deployments'"
            @show-logs="detail.showLogsFor"
            @open-rollback="detail.openRollbackFor"
          />
          <ApplicationImagePanel
            v-if="detail.application.value && detail.application.value.source_type === 'image'"
            :application="detail.application.value"
          />
        </NTabPane>

        <NTabPane name="deployments" :tab="deploymentsTab">
          <ApplicationDeploymentsTab
            :deployments="detail.deployments.value"
            :loading="detail.appsStore.loading"
            @show-logs="detail.showLogsFor"
            @open-rollback="detail.openRollbackFor"
          />
        </NTabPane>

        <NTabPane name="logs" :tab="t('applications.tabs.logs')">
          <ApplicationLogsTab
            :log-server-id="detail.logServerId.value"
            :log-deployment-id="detail.logDeploymentId.value"
            :server-options="detail.serverOptions.value"
            :deployment-options="detail.deploymentOptions.value"
            :active-deployment-id="detail.active.value?.id ?? ''"
            :log-target="detail.logTarget.value"
            :effective-log-server-id="detail.effectiveLogServerId.value"
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

        <NTabPane name="storage" :tab="t('applications.tabs.storage')">
          <ApplicationStorageTab
            :storages-draft="detail.storagesDraft.value"
            :storages-loading="detail.storagesLoading.value"
            :storages-error="detail.storagesError.value"
            :save-disabled="detail.storagesLoading.value || detail.storagesLoadedFor.value !== detail.appId.value"
            :saving="detail.appsStore.savingStorages"
            @update:draft="detail.storagesDraft.value = $event"
            @save="detail.handleSaveStorages"
            @retry="detail.loadStorages()"
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
          <ResourceMoveCard
            v-if="detail.application.value"
            :project-id="detail.application.value.project_id"
            :environment-id="detail.application.value.environment_id"
            :server-id="detail.application.value.server_id ?? ''"
            :saving="detail.moveSaving.value"
            :error="detail.moveError.value"
            @save="detail.handleMove"
          />
        </NTabPane>
      </NTabs>
    </NSpin>

    <RollbackDialog
      :show="detail.rollbackOpen.value"
      :rolling-back="detail.rollingBack.value"
      :rollback-target="detail.rollbackTarget.value"
      :running-deployments="detail.runningDeployments.value"
      @update:show="detail.rollbackOpen.value = $event"
      @update:target="detail.rollbackTarget.value = $event"
      @confirm="detail.handleRollback"
    />
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
