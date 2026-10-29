<script setup lang="ts">
import { NAlert, NCard, NDescriptions, NDescriptionsItem, NText } from "naive-ui";

import { serviceStatusLabel } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";
import { relativeTime } from "@/shared/utils/format";

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const { service, serverName, failedDeploy } = useServiceDetailContext();
</script>

<template>
  <NCard :title="t('services.overview.title')">
    <NAlert
      v-if="failedDeploy"
      type="error"
      :show-icon="true"
      :title="t('services.detail.deployFailedTitle')"
      class="deploy-failure"
      data-testid="overview-deploy-failed"
    >
      {{ failedDeploy.error }}
    </NAlert>
    <NDescriptions :column="2" label-placement="top" bordered size="small">
      <NDescriptionsItem :label="t('services.overview.node')">{{ serverName }}</NDescriptionsItem>
      <NDescriptionsItem :label="t('services.overview.status')">
        {{ service ? serviceStatusLabel(service.status) : "" }}
      </NDescriptionsItem>
      <NDescriptionsItem :label="t('services.overview.composeProject')">
        <span class="mono">{{ service?.compose_project }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem :label="t('services.overview.serviceId')">
        <span class="mono">{{ service?.id }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem :label="t('services.overview.created')">
        {{ relativeTime(service?.created_at ?? "") }}
      </NDescriptionsItem>
      <NDescriptionsItem :label="t('services.overview.updated')">
        {{ relativeTime(service?.updated_at ?? "") }}
      </NDescriptionsItem>
      <NDescriptionsItem :label="t('services.overview.domains')" :span="2">
        <template v-if="(service?.domains.length ?? 0) > 0">
          <span
            v-for="route in service?.domains ?? []"
            :key="route.domain"
            class="mono domain-chip"
          >
            {{ route.service }} → {{ route.domain }}:{{ route.port }}
          </span>
        </template>
        <NText v-else depth="3">
          {{ t("services.overview.noDomains", { label: "gotham.domain" }) }}
        </NText>
      </NDescriptionsItem>
    </NDescriptions>
  </NCard>
</template>

<style scoped>
.deploy-failure {
  margin-bottom: var(--space-3);
}

.domain-chip {
  display: inline-block;
  margin-right: var(--space-3);
}

.mono {
  font-family: var(--font-mono);
}
</style>
