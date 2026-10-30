<script setup lang="ts">
import {
  NAlert,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NSpace,
  NSpin,
  NText,
} from "naive-ui";
import { computed, onMounted, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import type { Server } from "@/features/servers/api/servers";
import HostHelperGate from "@/features/servers/components/host/HostHelperGate.vue";
import NetworkConfirmCard from "@/features/servers/components/host/NetworkConfirmCard.vue";
import NetworkForm from "@/features/servers/components/host/NetworkForm.vue";
import SystemForm from "@/features/servers/components/host/SystemForm.vue";
import { useNodeHost } from "@/features/servers/composables/useNodeHost";
import { authLabel } from "@/features/servers/utils/serverDetailView";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  server: Server;
}

const props = defineProps<Props>();
const { locale, t } = useI18n();
const authStore = useAuthStore();

const isNarrow = useMediaQuery("(max-width: 640px)");
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));

const { state, loading, forbidden, loadError, load, apply } = useNodeHost(
  () => props.server.id,
);
const pending = computed(() => state.value?.pending ?? null);

function loadIfOwner(): void {
  if (authStore.isOwner && state.value === null) {
    void load();
  }
}

onMounted(loadIfOwner);
watch(() => authStore.isOwner, loadIfOwner);
</script>

<template>
  <NSpace vertical :size="16">
    <NCard :title="t('servers.settings.title')">
      <NText depth="3" style="display: block; margin-bottom: 12px">
        {{ t("servers.settings.identityNote") }}
      </NText>
      <NDescriptions :column="descColumns" bordered label-placement="left">
        <NDescriptionsItem :label="t('servers.settings.name')">
          {{ server.name }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('servers.settings.address')">
          <span class="mono">{{ server.ip }}:{{ server.port }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('servers.settings.sshUser')">
          <span class="mono">{{ server.ssh_user }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('servers.settings.credential')">
          <span class="mono">{{ authLabel(server, locale) }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('servers.settings.registered')">
          {{ relativeTime(server.created_at) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('servers.settings.updated')">
          {{ relativeTime(server.updated_at) }}
        </NDescriptionsItem>
      </NDescriptions>
    </NCard>

    <template v-if="authStore.isOwner">
      <NSpin v-if="loading && !state" />
      <NAlert v-else-if="forbidden" type="warning" :show-icon="true">
        {{ t("servers.settings.host.forbidden") }}
      </NAlert>
      <NAlert v-else-if="loadError" type="error" :show-icon="true">{{ loadError }}</NAlert>

      <template v-else-if="state">
        <NetworkConfirmCard
          v-if="pending"
          :server-id="server.id"
          :pending="pending"
          @settled="apply"
          @expired="load"
        />
        <HostHelperGate :server-id="server.id" :state="state" @saved="apply" />
        <NetworkForm :server-id="server.id" :state="state" @saved="apply" />
        <SystemForm :server-id="server.id" :state="state" @saved="apply" />
      </template>
    </template>
  </NSpace>
</template>
