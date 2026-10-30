<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { confirmNodeNetwork, revertNodeNetwork } from "@/features/servers/api/host";
import type { NodeHostState, PendingNetwork } from "@/features/servers/api/host";
import { useNetworkConfirm } from "@/shared/composables/useNetworkConfirm";
import { errorText } from "@/shared/api/errors";

const props = defineProps<{ serverId: string; pending: PendingNetwork }>();
const emit = defineEmits<{ settled: [state: NodeHostState]; expired: [] }>();
const { t } = useI18n();

const busy = ref(false);
const error = ref("");
const deadline = computed(() => props.pending.deadline);
const { label } = useNetworkConfirm(deadline, () => emit("expired"));

async function settle(action: () => Promise<NodeHostState>): Promise<void> {
  busy.value = true;
  error.value = "";
  try {
    emit("settled", await action());
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <NCard :title="t('servers.settings.host.confirm.title')" class="confirm-card">
    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
    <p>{{ t("servers.settings.host.confirm.body") }}</p>
    <p class="countdown" aria-live="polite">{{ label }}</p>
    <NSpace>
      <NButton
        type="primary"
        :loading="busy"
        @click="settle(() => confirmNodeNetwork(serverId))"
      >
        {{ t("servers.settings.host.confirm.keep") }}
      </NButton>
      <NButton :disabled="busy" @click="settle(() => revertNodeNetwork(serverId))">
        {{ t("servers.settings.host.confirm.revert") }}
      </NButton>
    </NSpace>
  </NCard>
</template>

<style scoped>
.countdown {
  font-family: var(--font-mono, monospace);
  font-size: var(--text-lg, 20px);
}
</style>
