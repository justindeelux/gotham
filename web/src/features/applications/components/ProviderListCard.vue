<script setup lang="ts">
import { NAlert, NCard, NEmpty, NSpace, NTag, NText } from "naive-ui";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { useProvidersStore } from "@/features/applications/stores/providers";

const providersStore = useProvidersStore();
</script>

<template>
  <NCard title="Source providers">
    <template #header-extra>
      <NText depth="3">Each provider uses its own OAuth app</NText>
    </template>
    <NAlert
      v-if="providersStore.error"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ providersStore.error }}
    </NAlert>
    <NSpace vertical :size="8">
      <div
        v-for="provider in providersStore.providers"
        :key="provider.id"
        class="provider-row"
      >
        <GothamIcon name="box" />
        <NText strong>{{ provider.provider }}</NText>
        <NTag :type="provider.connected ? 'success' : 'warning'" size="small" round>
          {{ provider.connected ? "Connected" : "Not connected" }}
        </NTag>
        <NText depth="3" class="mono">{{ provider.base_url || "—" }}</NText>
      </div>
      <NEmpty
        v-if="!providersStore.loading && providersStore.providers.length === 0"
        description="No source providers connected yet."
      />
    </NSpace>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.provider-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--border);
}

.provider-row:last-child {
  border-bottom: 0;
}
</style>
