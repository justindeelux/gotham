<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { confirmNodeNetwork, revertNodeNetwork } from "@/features/servers/api/host";
import type { NodeHostState, PendingNetwork } from "@/features/servers/api/host";
import { useNetworkConfirm } from "@/shared/composables/useNetworkConfirm";
import { errorText } from "@/shared/api/errors";

const props = defineProps<{ serverId: string; pending: PendingNetwork }>();
const emit = defineEmits<{ settled: [serverId: string, state: NodeHostState]; expired: [] }>();
const { t } = useI18n();

const busy = ref(false);
const error = ref("");
const deadline = computed(() => props.pending.deadline);
const { label, secondsLeft } = useNetworkConfirm(deadline, () => emit("expired"));

const announcement = ref("");
const seenStart = ref(false);
const warned30 = ref(false);
const warned10 = ref(false);

watch(
  secondsLeft,
  (left) => {
    if (left <= 10) {
      if (!warned10.value) {
        warned10.value = true;
        announcement.value = t("servers.settings.host.confirm.countdown10");
      }
      return;
    }
    if (left <= 30) {
      if (!warned30.value) {
        warned30.value = true;
        announcement.value = t("servers.settings.host.confirm.countdown30");
      }
      return;
    }
    if (!seenStart.value) {
      seenStart.value = true;
      announcement.value = t("servers.settings.host.confirm.countdownStart", {
        time: label.value,
      });
    }
  },
  { immediate: true },
);

async function settle(action: () => Promise<NodeHostState>): Promise<void> {
  busy.value = true;
  error.value = "";
  try {
    emit("settled", props.serverId, await action());
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
    <p class="countdown" role="timer" aria-live="off">{{ label }}</p>
    <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>
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

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
