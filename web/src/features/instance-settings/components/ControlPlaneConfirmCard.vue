<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  confirmControlPlaneURL,
  revertControlPlaneURL,
} from "@/features/instance-settings/api/instance";
import type {
  InstanceState,
  PendingControlPlaneURL,
} from "@/features/instance-settings/api/instance";
import { errorText } from "@/shared/api/errors";
import { useNetworkConfirm } from "@/shared/composables/useNetworkConfirm";

const props = defineProps<{ pending: PendingControlPlaneURL }>();
const emit = defineEmits<{ settled: [state: InstanceState]; expired: [] }>();
const { t } = useI18n();

const busy = ref(false);
const error = ref("");
const deadline = computed(() => props.pending.deadline);
const { label } = useNetworkConfirm(deadline, () => emit("expired"));

const announcement = ref("");
watch(
  deadline,
  () => {
    announcement.value = t("instance-settings.pendingConfirm.countdownStart", {
      time: label.value,
    });
  },
  { immediate: true },
);

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
  <NCard :title="t('instance-settings.pendingConfirm.title')" class="confirm-card">
    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
    <p>
      {{ t("instance-settings.pendingConfirm.body", { url: pending.proposed }) }}
    </p>
    <p class="countdown" role="timer" aria-live="off">{{ label }}</p>
    <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>
    <p class="field-hint">{{ t("instance-settings.pendingConfirm.hint") }}</p>
    <NSpace wrap>
      <NButton type="primary" :loading="busy" @click="settle(confirmControlPlaneURL)">
        {{ t("instance-settings.pendingConfirm.keep") }}
      </NButton>
      <NButton :disabled="busy" @click="settle(revertControlPlaneURL)">
        {{ t("instance-settings.pendingConfirm.revert") }}
      </NButton>
    </NSpace>
  </NCard>
</template>

<style scoped>
.confirm-card {
  margin-bottom: var(--space-4);
}

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
