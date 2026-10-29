<script setup lang="ts">
import { NAlert, NButton, NCard, NDescriptions, NDescriptionsItem, NText } from "naive-ui";
import { computed, ref, watch } from "vue";

import { listServiceDomains, serviceStatusLabel } from "@/features/services/api/services";
import type { ServiceDomain } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";
import { relativeTime } from "@/shared/utils/format";

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const { service, serverName, failedDeploy, canWrite } = useServiceDetailContext();

const emit = defineEmits<{
  "open-domains": [];
  "add-domain": [];
}>();

const listed = ref<ServiceDomain[]>([]);

watch(
  () => service.value?.id,
  async (id) => {
    if (!id) {
      return;
    }
    try {
      listed.value = await listServiceDomains(id);
    } catch {
      // The rendered routes on the service still name the compose hosts.
      listed.value = [];
    }
  },
  { immediate: true },
);

const domains = computed<string[]>(() => {
  if (listed.value.length > 0) {
    return listed.value.map((row) => row.domain);
  }
  return (service.value?.domains ?? []).map((route) => route.domain);
});
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
        <div v-if="domains.length > 0" class="domain-links">
          <div v-for="host in domains" :key="host" class="domain-link">
            <span class="mono domain-chip">{{ host }}</span>
            <NButton
              text
              size="tiny"
              class="domain-open"
              @click="emit('open-domains')"
            >
              {{ t("services.overview.open") }}
            </NButton>
          </div>
        </div>
        <div v-else class="domain-empty">
          <NText depth="3">{{ t("services.overview.noDomains") }}</NText>
          <NButton
            v-if="canWrite"
            text
            type="primary"
            size="small"
            class="domain-add-overview"
            @click="emit('add-domain')"
          >
            {{ t("services.domains.add") }}
          </NButton>
        </div>
      </NDescriptionsItem>
    </NDescriptions>
  </NCard>
</template>

<style scoped>
.deploy-failure {
  margin-bottom: var(--space-3);
}

.domain-links {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.domain-link {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.domain-empty {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.mono {
  font-family: var(--font-mono);
}
</style>
