<script setup lang="ts">
import {
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NPopconfirm,
  NSpace,
  NText,
} from "naive-ui";
import { computed } from "vue";
import { RouterLink } from "vue-router";

import { useAuthStore } from "@/features/auth";
import type { Server } from "@/features/servers/api/servers";
import ServerPressureCard from "@/features/monitoring/components/ServerPressureCard.vue";
import ServerUsageSegments from "@/features/servers/components/ServerUsageSegments.vue";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import {
  authLabel,
  fallback,
  isUsageStale,
} from "@/features/servers/utils/serverDetailView";
import { relativeTime, toPercent, usageLevel } from "@/shared/utils/format";
import { useI18n } from "vue-i18n";

interface Props {
  server: Server;
  deleting: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  delete: [];
}>();

const { locale } = useI18n();
const authStore = useAuthStore();
const isNarrow = useMediaQuery("(max-width: 640px)");
const diskHigh = computed(() => usageLevel(toPercent(props.server.disk_usage)) !== "ok");

const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <ServerPressureCard :server-id="server.id" />
    <div class="overview-grid">
      <NCard :title="$t('servers.overview.usage')" class="usage-panel">
        <div class="usage-rows">
          <ServerUsageSegments
            :label="$t('servers.overview.cpuUsage')"
            :value="server.cpu_usage"
            :stale="isUsageStale(server.last_seen)"
          />
          <ServerUsageSegments
            :label="$t('servers.overview.memUsage')"
            :value="server.mem_usage"
            :stale="isUsageStale(server.last_seen)"
          />
          <div class="usage-disk">
            <ServerUsageSegments
              :label="$t('servers.overview.diskUsage')"
              :value="server.disk_usage"
              :stale="isUsageStale(server.last_seen)"
            />
            <RouterLink
              v-if="authStore.isOwner && diskHigh"
              class="usage-cleanup-link"
              :to="{ name: 'cleanup', query: { server: server.id } }"
            >
              {{ $t("servers.overview.cleanUp") }}
            </RouterLink>
          </div>
        </div>
        <NText v-if="isUsageStale(server.last_seen)" depth="3" class="usage-stale">
          {{ $t("servers.overview.usageStale") }}
        </NText>
      </NCard>

      <NCard :title="$t('servers.overview.nodeInfo')" class="node-info-panel">
        <NDescriptions :column="descColumns" bordered label-placement="left">
          <NDescriptionsItem :label="$t('servers.overview.name')">
            {{ server.name }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.address')">
            <span class="mono">{{ server.ip }}:{{ server.port }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.sshUser')">
            <span class="mono">{{ server.ssh_user }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.nodeId')">
            <span class="mono">{{ fallback(server.node_id) }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.os')">
            {{ fallback(server.os) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.arch')">
            {{ fallback(server.arch) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.docker')">
            {{ fallback(server.docker_version) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.credential')">
            <span class="mono">{{ authLabel(server, locale) }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.containers')">
            {{ server.container_count ?? "—" }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.lastSeen')">
            {{ relativeTime(server.last_seen) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('servers.overview.registered')">
            {{ relativeTime(server.created_at) }}
          </NDescriptionsItem>
        </NDescriptions>
      </NCard>
    </div>

    <NCard :title="$t('servers.overview.labels')">
      <NEmpty :description="$t('servers.overview.labelsEmpty')" />
    </NCard>

    <NCard :title="$t('servers.overview.dangerZone')">
      <NText depth="3">
        {{ $t("servers.overview.dangerText") }}
      </NText>
      <div style="margin-top: 12px">
        <NPopconfirm
          :positive-button-props="{ type: 'error' }"
          @positive-click="emit('delete')"
        >
          <template #trigger>
            <NButton type="error" ghost :loading="deleting">
              {{ $t("servers.overview.deleteNode") }}
            </NButton>
          </template>
          {{ $t("servers.overview.deleteConfirm", { name: server.name }) }}
        </NPopconfirm>
      </div>
    </NCard>
  </NSpace>
</template>

<style scoped>
.overview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: var(--space-4);
  align-items: start;
}

.usage-rows {
  display: grid;
  gap: var(--space-4);
}

.usage-disk {
  display: grid;
  gap: var(--space-1);
  min-width: 0;
}

.usage-cleanup-link {
  justify-self: end;
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--warn-ink, var(--warn));
  text-decoration: none;
}

.usage-cleanup-link:hover,
.usage-cleanup-link:focus-visible {
  text-decoration: underline;
}

.usage-stale {
  display: block;
  margin-top: var(--space-3);
  font-size: var(--text-xs);
}

@media (max-width: 1024px) {
  .overview-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>

