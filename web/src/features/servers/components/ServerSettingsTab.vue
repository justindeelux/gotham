<script setup lang="ts">
import { NAlert, NSpace, NSpin } from "naive-ui";
import { computed, onMounted, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useAuthStore } from "@/features/auth";
import type { Server } from "@/features/servers/api/servers";
import type { NodeHostState } from "@/features/servers/api/host";
import AgentRootForm from "@/features/servers/components/host/AgentRootForm.vue";
import HostHelperGate from "@/features/servers/components/host/HostHelperGate.vue";
import NetworkConfirmCard from "@/features/servers/components/host/NetworkConfirmCard.vue";
import NetworkForm from "@/features/servers/components/host/NetworkForm.vue";
import SSHForm from "@/features/servers/components/host/SSHForm.vue";
import SystemForm from "@/features/servers/components/host/SystemForm.vue";
import { useNodeHost } from "@/features/servers/composables/useNodeHost";

interface Props {
  server: Server;
}

const props = defineProps<Props>();
const emit = defineEmits<{ serverChanged: [] }>();
const { t } = useI18n();
const authStore = useAuthStore();

const { state, loading, forbidden, loadError, load, apply } = useNodeHost(
  () => props.server.id,
);
const pending = computed(() => state.value?.pending ?? null);

function handleSSHSaved(origin: string, next: NodeHostState): void {
  apply(origin, next);
  emit("serverChanged");
}

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
        <div class="settings-grid">
          <NetworkForm :server-id="server.id" :state="state" @saved="apply" />
          <SystemForm :server-id="server.id" :state="state" @saved="apply" />
          <SSHForm :server-id="server.id" :state="state" @saved="handleSSHSaved" />
          <AgentRootForm :server-id="server.id" :state="state" @saved="apply" />
        </div>
      </template>
    </template>
  </NSpace>
</template>

<style scoped>
.settings-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--space-4);
  align-items: stretch;
}

.settings-grid > :deep(.n-card) {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.settings-grid > :deep(.n-card > .n-card-content) {
  display: flex;
  flex: 1;
  flex-direction: column;
}

.settings-grid :deep(.host-form) {
  display: flex;
  flex: 1;
  flex-direction: column;
}

.settings-grid :deep(.host-form > .n-button) {
  margin-top: auto;
  align-self: flex-start;
}

@media (min-width: 769px) {
  .settings-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
