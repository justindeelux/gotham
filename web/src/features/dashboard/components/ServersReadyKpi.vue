<script setup lang="ts">
import { NCard, NSkeleton, NText } from "naive-ui";

import type { ServerStatus } from "@/features/servers";
import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";

interface Props {
  loading: boolean;
  readyCount: number;
  totalCount: number;
  aggregateStatus: ServerStatus;
  offlineNames: string;
}

defineProps<Props>();
</script>

<template>
  <NCard class="kpi kpi--live" :title="$t('dashboard.kpi.serversReady')" size="small">
    <NSkeleton v-if="loading && totalCount === 0" text :repeat="2" />
    <template v-else>
      <p class="kpi-value num">
        {{ readyCount }}<span class="kpi-unit">/{{ totalCount }}</span>
      </p>
      <p class="kpi-sub">
        <ServerStatusTag v-if="totalCount > 0" :status="aggregateStatus" />
        <NText v-else depth="3">{{ $t("dashboard.kpi.noServers") }}</NText>
        <NText v-if="offlineNames" depth="3">
          {{ $t("dashboard.kpi.unreachable", { names: offlineNames }) }}
        </NText>
      </p>
    </template>
  </NCard>
</template>

<style scoped>
.kpi-value {
  font-size: var(--text-3xl);
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.kpi-unit {
  font-size: var(--text-lg);
  color: var(--muted);
}

.kpi-sub {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin: 0;
  flex-wrap: wrap;
}

.num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
</style>
