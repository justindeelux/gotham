<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NText } from "naive-ui";
import { provide } from "vue";
import { RouterLink } from "vue-router";

import ServiceComposeCard from "@/features/services/components/ServiceComposeCard.vue";
import ServiceContainersCard from "@/features/services/components/ServiceContainersCard.vue";
import ServiceDeployHistoryCard from "@/features/services/components/ServiceDeployHistoryCard.vue";
import ServiceDetailHeader from "@/features/services/components/ServiceDetailHeader.vue";
import ServiceEnvCard from "@/features/services/components/ServiceEnvCard.vue";
import ServiceLogs from "@/features/services/components/ServiceLogs.vue";
import ServiceOverviewCard from "@/features/services/components/ServiceOverviewCard.vue";
import { serviceDetailKey, useServiceDetail } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";
import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import ResourceMoveCard from "@/features/projects/components/ResourceMoveCard.vue";

/**
 * One compose service: the stored document (view/edit → PATCH), its
 * environment, the node's containers and logs, the lifecycle actions and the
 * deploy history.
 *
 * Containers and logs are loaded on demand: both dial the node agent, which
 * answers 502 when no agent is connected, so a page render never probes the
 * node on its own. The per-deploy step timeline has no API and is rendered as
 * an explicit backend-pending stub.
 *
 * Thin route component: state lives in `useServiceDetail` (provided to the
 * cards below), sections render through them.
 */
const page = useServiceDetail();
provide(serviceDetailKey, page);

/**
 * t renders page copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without losing drafts.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}
const {
  service,
  serviceId,
  error,
  notFound,
  actionError,
  loggableServices,
  canWrite,
  moveSaving,
  moveError,
  handleMove,
  deploys,
} = page;
</script>

<template>
  <div class="service-detail-page">
    <ProjectBreadcrumb
      v-if="service"
      :project-name="service.project_name"
      :project-id="service.project_id"
      :environment-name="service.environment_name"
      :environment-id="service.environment_id"
      :resource-name="service.name"
    />
    <ServiceDetailHeader />

    <NAlert v-if="error" type="error" :show-icon="true">
      {{ error }}
    </NAlert>

    <NEmpty
      v-if="notFound"
      :description="t('services.detail.notFound')"
    >
      <template #extra>
        <RouterLink :to="{ name: 'projects' }">
          <NButton>{{ t("services.detail.backToProjects") }}</NButton>
        </RouterLink>
      </template>
    </NEmpty>

    <template v-else-if="service">
      <NAlert v-if="actionError" type="error" :show-icon="true">
        {{ actionError }}
      </NAlert>

      <ServiceOverviewCard />

      <ResourceMoveCard
        v-if="canWrite"
        :project-id="service.project_id"
        :environment-id="service.environment_id"
        :server-id="service.server_id"
        :saving="moveSaving"
        :error="moveError"
        :server-pinned="deploys.length > 0"
        :server-pinned-reason="t('services.detail.serverPinnedReason')"
        @save="handleMove"
      />

      <ServiceComposeCard />

      <ServiceEnvCard />

      <ServiceContainersCard />

      <NCard :title="t('services.detail.logsTitle')">
        <template #header-extra>
          <NText depth="3" class="small">
            {{ t("services.detail.logsNote") }}
          </NText>
        </template>
        <ServiceLogs
          :service-id="serviceId"
          :services="loggableServices"
          :title="service.name"
        />
      </NCard>

      <ServiceDeployHistoryCard />
    </template>
  </div>
</template>

<style scoped>
.service-detail-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.small {
  font-size: var(--text-xs);
}
</style>
