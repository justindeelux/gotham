<script setup lang="ts">
import {
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NText,
} from "naive-ui";
import { computed } from "vue";

import type { Server } from "@/features/servers/api/servers";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import { authLabel } from "@/features/servers/utils/serverDetailView";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  server: Server;
}

defineProps<Props>();

/** isNarrow stacks the two-column descriptions on small screens. */
const isNarrow = useMediaQuery("(max-width: 640px)");

/** descColumns renders descriptions in one column below 640px. */
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));
</script>

<template>
  <NCard title="Node settings" style="margin-top: 16px">
    <NText depth="3" style="display: block; margin-bottom: 12px">
      Editable settings do not exist in the backend yet. Values below
      are read-only.
    </NText>
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
      <NDescriptionsItem label="Credential">
        <span class="mono">{{ authLabel(server) }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem label="Registered">
        {{ relativeTime(server.created_at) }}
      </NDescriptionsItem>
      <NDescriptionsItem label="Updated">
        {{ relativeTime(server.updated_at) }}
      </NDescriptionsItem>
    </NDescriptions>
  </NCard>
</template>
