<script setup lang="ts">
import { NButton, NText } from "naive-ui";

import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  step: number;
  renderLoading: boolean;
  hasRender: boolean;
  created: boolean;
  creating: boolean;
  deploying: boolean;
  deployed: boolean;
}

defineProps<Props>();

/**
 * t renders footer copy in the active locale (tracks language switches).
 * Step state and draft ownership never change on a switch.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const emit = defineEmits<{
  back: [];
  next: [];
  close: [];
  create: [];
  deploy: [];
}>();
</script>

<template>
  <div class="wizard__foot">
    <NButton
      v-if="step > 1 && !created"
      size="small"
      @click="emit('back')"
    >
      {{ t("templates.wizard.back") }}
    </NButton>
    <NText depth="3" class="wizard__counter">
      {{ t("templates.wizard.counter", { step }) }}
    </NText>
    <span class="wizard__spacer"></span>
    <NButton size="small" @click="emit('close')">{{ t("templates.wizard.close") }}</NButton>
    <NButton
      v-if="step < 3"
      size="small"
      type="primary"
      :disabled="step === 2 && (renderLoading || !hasRender)"
      @click="emit('next')"
    >
      {{ t("templates.wizard.next") }}
    </NButton>
    <NButton
      v-else-if="!created"
      size="small"
      type="primary"
      :loading="creating"
      :disabled="renderLoading || !hasRender"
      @click="emit('create')"
    >
      {{ t("templates.wizard.create") }}
    </NButton>
    <NButton
      v-else-if="!deployed"
      size="small"
      type="primary"
      :loading="deploying"
      @click="emit('deploy')"
    >
      {{ t("templates.wizard.deploy") }}
    </NButton>
  </div>
</template>

<style scoped>
.wizard__foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  border-top: 1px solid var(--border);
  padding-top: var(--space-3);
  margin-top: var(--space-4);
}

.wizard__counter {
  font-size: var(--text-xs);
}

.wizard__spacer {
  flex: 1 1 auto;
}
</style>
