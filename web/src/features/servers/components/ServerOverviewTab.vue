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

interface Props {
  server: Server;
  deleting: boolean;
}

defineProps<Props>();
const emit = defineEmits<{
  delete: [];
}>();

/** isNarrow stacks the two-column descriptions on small screens. */
const isNarrow = useMediaQuery("(max-width: 640px)");

/** descColumns renders descriptions in one column below 640px. */
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <NCard title="Node info">
      <NDescriptions :column="descColumns" bordered label-placement="left">
        <NDescriptionsItem label="Name">
          {{ server.name }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Address">
          <span class="mono">{{ server.ip }}:{{ server.port }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="SSH user">
          <span class="mono">{{ server.ssh_user }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Node ID">
          <span class="mono">{{ fallback(server.node_id) }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="OS">
          {{ fallback(server.os) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Architecture">
          {{ fallback(server.arch) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Docker">
          {{ fallback(server.docker_version) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Credential">
          <span class="mono">{{ authLabel(server) }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="CPU usage">
          {{ usageText(server.cpu_usage) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Memory usage">
          {{ usageText(server.mem_usage) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Disk usage">
          {{ usageText(server.disk_usage) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Containers">
          {{ server.container_count ?? "—" }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Last seen">
          {{ relativeTime(server.last_seen) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Registered">
          {{ relativeTime(server.created_at) }}
        </NDescriptionsItem>
      </NDescriptions>
    </NCard>

    <NCard title="Labels">
      <NEmpty description="No labels on this node yet." />
    </NCard>

    <NCard title="Danger zone">
      <NText depth="3">
        Deleting a node removes it from the control plane only.
        Containers, volumes and certificates on the machine are kept.
      </NText>
      <div style="margin-top: 12px">
        <NPopconfirm
          :positive-button-props="{ type: 'error' }"
          @positive-click="emit('delete')"
        >
          <template #trigger>
            <NButton type="error" ghost :loading="deleting">
              Delete node
            </NButton>
          </template>
          Delete server "{{ server.name }}"?
        </NPopconfirm>
      </div>
    </NCard>
  </NSpace>
</template>
