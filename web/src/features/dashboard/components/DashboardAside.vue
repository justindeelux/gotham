<script setup lang="ts">
import { NAlert, NCard, NEmpty, NSpace, NTag, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { Server } from "@/features/servers";
import { useServersStore } from "@/features/servers";
import { relativeTime } from "@/shared/utils/format";

const serversStore = useServersStore();
const { t } = useI18n();

/** Bound the widgets so a large fleet is not re-diffed in full every 5s (B4-14). */
const heartbeatWidgetLimit = 8;
const visibleHeartbeats = computed<Server[]>(() =>
  serversStore.servers.slice(0, heartbeatWidgetLimit),
);
const hiddenHeartbeatCount = computed<number>(() =>
  Math.max(0, serversStore.servers.length - heartbeatWidgetLimit),
);
const totalCount = computed<number>(() => serversStore.servers.length);
const offlineServers = computed<Server[]>(() =>
  serversStore.servers.filter(
    (server) => server.status === "offline" || server.status === "error",
  ),
);

/** moreNodesText renders the overflow count in the display locale. */
const moreNodesText = computed<string>(() =>
  hiddenHeartbeatCount.value === 1
    ? t("dashboard.aside.moreOne", { count: 1 })
    : t("dashboard.aside.moreOther", { count: hiddenHeartbeatCount.value }),
);
</script>

<template>
  <NCard size="small" :title="$t('dashboard.aside.heartbeat')" class="aside-card">
    <template #header-extra>
      <NTag size="small" type="success" :bordered="false">
        <span class="pulse-dot" aria-hidden="true" />{{ $t("dashboard.aside.live") }}
      </NTag>
    </template>
    <NEmpty
      v-if="totalCount === 0"
      size="small"
      :description="$t('dashboard.aside.noHeartbeats')"
    />
    <NSpace v-else vertical :size="8">
      <div
        v-for="server in visibleHeartbeats"
        :key="server.id"
        class="heartbeat-row"
      >
        <NText depth="2">{{ server.name }}</NText>
        <NText depth="3" class="mono">{{ relativeTime(server.last_seen) }}</NText>
      </div>
      <NText v-if="hiddenHeartbeatCount > 0" depth="3" class="meta">
        {{ moreNodesText }}
      </NText>
    </NSpace>
    <template #footer>
      <NText depth="3" class="mono meta">10s cycle · Heartbeat(stream) in agent.v1</NText>
    </template>
  </NCard>

  <NCard size="small" :title="$t('dashboard.aside.components')" class="aside-card">
    <NEmpty
      size="small"
      :description="$t('dashboard.aside.noTelemetry')"
    >
      <template #extra>
        <NText depth="3">
          {{ $t("dashboard.aside.telemetryNote") }}
        </NText>
      </template>
    </NEmpty>
  </NCard>

  <NCard size="small" :title="$t('dashboard.aside.teamActivity')" class="aside-card">
    <NEmpty
      size="small"
      :description="$t('dashboard.aside.noActivity')"
    />
  </NCard>

  <NCard size="small" :title="$t('dashboard.aside.alerts')" class="aside-card">
    <NSpace v-if="offlineServers.length > 0" vertical :size="8">
      <NAlert
        v-for="server in offlineServers"
        :key="server.id"
        type="error"
        :show-icon="true"
        :title="$t('dashboard.aside.unreachableTitle', { name: server.name })"
      >
        {{ $t("dashboard.aside.heartbeatLost", { time: relativeTime(server.last_seen) }) }}
      </NAlert>
    </NSpace>
    <NEmpty v-else size="small" :description="$t('dashboard.aside.noAlerts')" />
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.meta {
  font-size: var(--text-xs);
  color: var(--muted);
}

.heartbeat-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  font-size: var(--text-sm);
}

.pulse-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  background: var(--success);
  margin-right: 6px;
  animation: dash-pulse var(--motion-base) var(--ease-standard) infinite alternate;
}

@keyframes dash-pulse {
  from {
    opacity: 1;
  }
  to {
    opacity: 0.45;
  }
}
</style>
