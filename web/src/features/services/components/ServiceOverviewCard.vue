<script setup lang="ts">
import { NCard, NDescriptions, NDescriptionsItem, NText } from "naive-ui";

import { serviceStatusLabel } from "@/features/services/api/services";
import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { activeLocale, i18n } from "@/shared/i18n";
import { relativeTime } from "@/shared/utils/format";

/** ServiceOverviewCard renders the service facts and the timeline stub. */

/**
 * t renders card copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without reloading the service.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const { service, serverName } = useServiceDetailContext();
</script>

<template>
  <NCard :title="t('services.overview.title')">
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

    <div class="stub">
      <h4>{{ t("services.overview.stubTitle") }}</h4>
      <p>
        {{ t("services.overview.stubBody") }}
      </p>
    </div>
  </NCard>
</template>

<style scoped>
.domain-chip {
  display: inline-block;
  margin-right: var(--space-3);
}

.stub {
  margin-top: var(--space-4);
  border-left: 4px solid var(--warn);
  background: var(--surface);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}

.stub h4 {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.stub p {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
  max-width: 80ch;
}

.mono {
  font-family: var(--font-mono);
}
</style>
