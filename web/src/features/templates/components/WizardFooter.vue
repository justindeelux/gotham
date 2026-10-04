<script setup lang="ts">
import { NButton, NText } from "naive-ui";

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
      Back
    </NButton>
    <NText depth="3" class="wizard__counter">
      Step {{ step }} / 3
    </NText>
    <span class="wizard__spacer"></span>
    <NButton size="small" @click="emit('close')">Close</NButton>
    <NButton
      v-if="step < 3"
      size="small"
      type="primary"
      :disabled="step === 2 && (renderLoading || !hasRender)"
      @click="emit('next')"
    >
      Next
    </NButton>
    <NButton
      v-else-if="!created"
      size="small"
      type="primary"
      :loading="creating"
      :disabled="renderLoading || !hasRender"
      @click="emit('create')"
    >
      Create service
    </NButton>
    <NButton
      v-else-if="!deployed"
      size="small"
      type="primary"
      :loading="deploying"
      @click="emit('deploy')"
    >
      Deploy now
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
