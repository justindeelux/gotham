<script setup lang="ts">
import { NAlert, NButton, NEmpty, NTabPane, NTabs } from "naive-ui";
import { provide, ref } from "vue";
import { RouterLink } from "vue-router";

import ResourceDetailFrame from "@/features/projects/components/ResourceDetailFrame.vue";
import ResourceSettingsTab from "@/features/projects/components/ResourceSettingsTab.vue";
import ServiceComposeCard from "@/features/services/components/ServiceComposeCard.vue";
import ServiceContainersCard from "@/features/services/components/ServiceContainersCard.vue";
import ServiceDeployHistoryCard from "@/features/services/components/ServiceDeployHistoryCard.vue";
import ServiceDetailHeader from "@/features/services/components/ServiceDetailHeader.vue";
import ServiceEnvCard from "@/features/services/components/ServiceEnvCard.vue";
import ServiceLogs from "@/features/services/components/ServiceLogs.vue";
import ServiceOverviewCard from "@/features/services/components/ServiceOverviewCard.vue";
import { serviceDetailKey, useServiceDetail } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";

const page = useServiceDetail();
provide(serviceDetailKey, page);

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}
const {
  service,
  serviceId,
  loading,
  error,
  notFound,
  actionError,
  loggableServices,
  canWrite,
  moveSaving,
  moveError,
  handleMove,
  handleDelete,
  busy,
  deploys,
} = page;

const activeTab = ref("overview");
</script>

<template>
  <ResourceDetailFrame
    class="service-detail-page"
    :loading="loading"
    :error="error"
    :not-found="notFound"
    :breadcrumb-label="t('projects.breadcrumb.label')"
    :fallback-id="serviceId.slice(0, 8)"
    :breadcrumbs="
      service
        ? {
            projectName: service.project_name,
            projectId: service.project_id,
            environmentName: service.environment_name,
            environmentId: service.environment_id,
            resourceName: service.name,
          }
        : null
    "
  >
    <template #banner>
      <NAlert v-if="actionError" type="error" :show-icon="true" class="detail-banner">
        {{ actionError }}
      </NAlert>
    </template>

    <template #header>
      <ServiceDetailHeader />
    </template>

    <template #not-found>
      <NEmpty :description="t('services.detail.notFound')">
        <template #extra>
          <RouterLink :to="{ name: 'projects' }">
            <NButton>{{ t("services.detail.backToProjects") }}</NButton>
          </RouterLink>
        </template>
      </NEmpty>
    </template>

    <NTabs v-if="service" v-model:value="activeTab" type="line" animated>
      <NTabPane name="overview" :tab="t('services.tabs.overview')">
        <ServiceOverviewCard />
      </NTabPane>

      <NTabPane name="deployments" :tab="t('services.tabs.deployments')">
        <ServiceDeployHistoryCard />
      </NTabPane>

      <NTabPane name="compose" :tab="t('services.tabs.compose')">
        <ServiceComposeCard />
      </NTabPane>

      <NTabPane name="env" :tab="t('services.tabs.env')">
        <ServiceEnvCard />
      </NTabPane>

      <NTabPane name="containers" :tab="t('services.tabs.containers')">
        <ServiceContainersCard />
      </NTabPane>

      <NTabPane name="logs" :tab="t('services.tabs.logs')">
        <ServiceLogs
          :service-id="serviceId"
          :services="loggableServices"
          :title="service.name"
        />
      </NTabPane>

      <NTabPane v-if="canWrite" name="settings" :tab="t('services.tabs.settings')">
        <ResourceSettingsTab
          :project-id="service.project_id"
          :environment-id="service.environment_id"
          :server-id="service.server_id"
          :saving="moveSaving"
          :error="moveError"
          :server-pinned="deploys.length > 0"
          :server-pinned-reason="t('services.detail.serverPinnedReason')"
          :danger-title="t('services.detail.deleteTitle')"
          :danger-hint="t('services.detail.deleteHint')"
          :danger-label="t('services.header.delete')"
          :danger-confirm="t('services.header.deleteConfirm', { name: service.name })"
          :danger-busy="busy === 'delete'"
          @save="handleMove"
          @delete="() => void handleDelete()"
        />
      </NTabPane>
    </NTabs>
  </ResourceDetailFrame>
</template>

<style scoped>
.detail-banner {
  margin-bottom: var(--space-3);
}
</style>
