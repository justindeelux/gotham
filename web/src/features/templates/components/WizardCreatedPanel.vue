<script setup lang="ts">
import { NAlert, NButton, NSpace, NTag } from "naive-ui";
import { RouterLink } from "vue-router";

import type { Service } from "@/features/services";
import ServiceLogs from "@/features/services/components/ServiceLogs.vue";

interface Props {
  created: Service;
  deployed: boolean;
  deployError: string | null;
  logServices: string[];
  logTitle: string;
}

defineProps<Props>();

const emit = defineEmits<{
  close: [];
}>();
</script>

<template>
  <div class="wizard__success" data-testid="wizard-created">
    <h4>{{ created.name }} created</h4>
    <NSpace :size="8" align="center">
      <NTag size="small" type="warning">{{ created.status }}</NTag>
      <NTag size="small" class="mono">{{ created.project_name }}</NTag>
      <NTag
        v-for="route in created.domains"
        :key="route.domain"
        size="small"
        class="mono"
      >
        {{ route.domain }}
      </NTag>
    </NSpace>
    <p class="wizard__desc">
      The service row exists; nothing runs yet. Deploy renders the
      document again on the node and records one deploy row.
    </p>
    <NAlert v-if="deployError" type="error" :show-icon="true">
      {{ deployError }}
    </NAlert>
    <NSpace :size="8" align="center">
      <RouterLink
        :to="{ name: 'service-detail', params: { id: created.id } }"
        @click="emit('close')"
      >
        <NButton size="small">Open service detail</NButton>
      </RouterLink>
      <NTag v-if="deployed" size="small" type="success">deployed</NTag>
    </NSpace>
    <div class="wizard__stub">
      <h5>Deploy step timeline — backend pending</h5>
      <p>
        The API records one row per deploy (state, error, timestamps)
        and exposes no per-step progress, so no step timeline is shown
        here. The detail page lists the history instead.
      </p>
    </div>
    <ServiceLogs
      v-if="deployed"
      :service-id="created.id"
      :services="logServices"
      :title="logTitle"
    />
  </div>
</template>

<style scoped>
.wizard__success {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.wizard__success h4 {
  font-size: var(--text-base);
}

.wizard__desc {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.wizard__stub {
  border-left: 4px solid var(--warn);
  background: var(--surface);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}

.wizard__stub h5 {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.wizard__stub p {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}
</style>
