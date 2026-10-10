<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace } from "naive-ui";
import { computed, ref, toRef } from "vue";
import { useI18n } from "vue-i18n";

import { confirmNetwork, errorText, revertNetwork } from "@/features/instance-settings/api/instance";
import type { InstanceState, PendingNetwork } from "@/features/instance-settings/api/instance";
import { useNetworkConfirm } from "@/features/instance-settings/composables/useNetworkConfirm";

const props = defineProps<{ pending: PendingNetwork }>();
const emit = defineEmits<{ settled: [state: InstanceState]; expired: [] }>();
const { t } = useI18n();

const busy = ref(false);
const error = ref("");
const deadline = computed(() => props.pending.deadline);
const { label } = useNetworkConfirm(toRef(deadline), () => emit("expired"));

async function settle(action: () => Promise<InstanceState>): Promise<void> {
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
  <NCard :title="t('instance-settings.confirm.title')" class="confirm-card">
    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
    <p>{{ t("instance-settings.confirm.body") }}</p>
    <p class="countdown" aria-live="polite">{{ label }}</p>
    <NSpace>
      <NButton type="primary" :loading="busy" @click="settle(confirmNetwork)">
        {{ t("instance-settings.confirm.keep") }}
      </NButton>
      <NButton :disabled="busy" @click="settle(revertNetwork)">
        {{ t("instance-settings.confirm.revert") }}
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
