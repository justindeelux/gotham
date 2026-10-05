<script setup lang="ts">
import { NCard, NDescriptions, NDescriptionsItem, NText } from "naive-ui";

import { useServiceDetailContext } from "@/features/services/composables/useServiceDetail";
import { relativeTime } from "@/shared/utils/format";

/** ServiceOverviewCard renders the service facts and the timeline stub. */
const { service, serverName } = useServiceDetailContext();
</script>

<template>
  <NCard title="Overview">
    <NDescriptions :column="2" label-placement="top" bordered size="small">
      <NDescriptionsItem label="Node">{{ serverName }}</NDescriptionsItem>
      <NDescriptionsItem label="Status">
        {{ service?.status }}
      </NDescriptionsItem>
      <NDescriptionsItem label="Compose project">
        <span class="mono">{{ service?.compose_project }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem label="Service id">
        <span class="mono">{{ service?.id }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem label="Created">
        {{ relativeTime(service?.created_at ?? "") }}
      </NDescriptionsItem>
      <NDescriptionsItem label="Updated">
        {{ relativeTime(service?.updated_at ?? "") }}
      </NDescriptionsItem>
      <NDescriptionsItem label="Domains" :span="2">
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
          No routed domain. A compose service is routed by the
          <span class="mono">gotham.domain</span> label.
        </NText>
      </NDescriptionsItem>
    </NDescriptions>

    <div class="stub">
      <h4>Deploy step timeline — backend pending</h4>
      <p>
        The API records one row per deploy (state, error, timestamps) and
        exposes no per-step progress, so the history below is the whole
        picture. Service TLS/certificate status is out of scope for this
        page.
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
