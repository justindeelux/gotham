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
import { useI18n } from "vue-i18n";

interface Props {
  server: Server;
}

defineProps<Props>();

const { locale } = useI18n();

/** isNarrow stacks the two-column descriptions on small screens. */
const isNarrow = useMediaQuery("(max-width: 640px)");

/** descColumns renders descriptions in one column below 640px. */
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));
</script>

<template>
  <NCard :title="$t('servers.settings.title')" style="margin-top: 16px">
    <NText depth="3" style="display: block; margin-bottom: 12px">
      {{ $t("servers.settings.readOnlyNote") }}
    </NText>
    <NDescriptions :column="descColumns" bordered label-placement="left">
      <NDescriptionsItem :label="$t('servers.settings.name')">
        {{ server.name }}
      </NDescriptionsItem>
      <NDescriptionsItem :label="$t('servers.settings.address')">
        <span class="mono">{{ server.ip }}:{{ server.port }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem :label="$t('servers.settings.sshUser')">
        <span class="mono">{{ server.ssh_user }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem :label="$t('servers.settings.credential')">
        <span class="mono">{{ authLabel(server, locale) }}</span>
      </NDescriptionsItem>
      <NDescriptionsItem :label="$t('servers.settings.registered')">
        {{ relativeTime(server.created_at) }}
      </NDescriptionsItem>
      <NDescriptionsItem :label="$t('servers.settings.updated')">
        {{ relativeTime(server.updated_at) }}
      </NDescriptionsItem>
    </NDescriptions>
  </NCard>
</template>
