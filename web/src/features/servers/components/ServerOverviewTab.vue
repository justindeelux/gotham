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

import type { Server } from "@/features/servers/api/servers";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import {
  authLabel,
  fallback,
  usageText,
} from "@/features/servers/utils/serverDetailView";
import { relativeTime } from "@/shared/utils/format";
import { useI18n } from "vue-i18n";

interface Props {
  server: Server;
  deleting: boolean;
}

defineProps<Props>();
const emit = defineEmits<{
  delete: [];
}>();

const { locale } = useI18n();
/** isNarrow stacks the two-column descriptions on small screens. */
const isNarrow = useMediaQuery("(max-width: 640px)");

/** descColumns renders descriptions in one column below 640px. */
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <NCard :title="$t('servers.overview.nodeInfo')">
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
        <NDescriptionsItem :label="$t('servers.overview.cpuUsage')">
          {{ usageText(server.cpu_usage) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('servers.overview.memUsage')">
          {{ usageText(server.mem_usage) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('servers.overview.diskUsage')">
          {{ usageText(server.disk_usage) }}
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
