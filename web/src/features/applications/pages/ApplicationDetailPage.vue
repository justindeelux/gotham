<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NSpace, NSpin, NTabPane, NTabs } from "naive-ui";
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
import { useApplicationTerminalAccess } from "@/features/applications/composables/useApplicationTerminalAccess";
import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import ResourceMoveCard from "@/features/projects/components/ResourceMoveCard.vue";

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

      <NAlert
        v-if="blockedTerminal"
        type="info"
        :show-icon="true"
        class="app-blocked-terminal"
      >
        <template #header>
          {{ t("applications.terminal.notAllowedTitle") }}
        </template>
        {{ t("applications.terminal.notAllowedBody") }}
      </NAlert>

      <ApplicationHeader
        :display-name="detail.displayName.value"
        :app-id="detail.appId.value"
        :short-id="detail.shortId.value"
        :brand="detail.brand.value"
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
          <NSpace vertical :size="16">
            <ResourceMoveCard
              v-if="detail.application.value"
              :project-id="detail.application.value.project_id"
              :environment-id="detail.application.value.environment_id"
              :server-id="detail.application.value.server_id ?? ''"
              :saving="detail.moveSaving.value"
              :error="detail.moveError.value"
              @save="detail.handleMove"
            />
            <NCard v-if="detail.application.value" :title="t('applications.delete.title')">
              <NSpace vertical :size="12">
                <span>{{ t("applications.delete.hint") }}</span>
                <NSpace>
                  <NButton type="error" ghost @click="detail.deleteOpen.value = true">
                    {{ t("applications.delete.button") }}
                  </NButton>
                </NSpace>
              </NSpace>
            </NCard>
          </NSpace>
        </NTabPane>
      </NTabs>
    </NSpin>

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

.application-terminal {
  display: flex;
  flex-direction: column;
  min-height: min(60vh, 34rem);
}

.app-blocked-terminal {
  margin-bottom: 12px;
}
</style>
