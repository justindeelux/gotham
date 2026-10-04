<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NIcon, NInput, NSpace, NSpin, NText } from "naive-ui";

import type { Application } from "@/features/applications/api/applications";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

interface Props {
  knownApps: Application[];
  listLoading: boolean;
  listError: string | null;
  openById: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:openById": [value: string];
  open: [id: string];
  create: [];
}>();
</script>

<template>
  <NCard title="Applications">
    <NAlert
      v-if="props.listError"
      type="warning"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ props.listError }} Create an application to get started, or open one by ID below.
    </NAlert>

    <NSpace v-if="props.knownApps.length > 0" vertical :size="8">
      <div
        v-for="app in props.knownApps"
        :key="app.id"
        class="provider-row"
      >
        <GothamIcon name="box" />
        <NText strong class="mono">{{ app.name }}</NText>
        <NText depth="3" class="mono">{{ app.id.slice(0, 8) }}</NText>
        <NText depth="3" class="mono">{{ app.branch || "—" }}</NText>
        <NButton
          quaternary
          size="small"
          @click="emit('open', app.id)"
        >
          Open
        </NButton>
      </div>
    </NSpace>

    <!-- Render the initial load as a spinner instead of flashing the empty
         state before the first response lands (C4-16). -->
    <div v-else-if="props.listLoading" class="apps-loading">
      <NSpin size="small" />
      <NText depth="3">Loading applications…</NText>
    </div>

    <NEmpty
      v-else
      class="apps-empty"
      description="No applications yet"
    >
      <template #icon>
        <NIcon>
          <GothamIcon name="box" />
        </NIcon>
      </template>
      <template #extra>
        <p class="apps-empty-hint">
          Create the first application to get a deployment pipeline with
          environment, volumes and rollback per release.
        </p>
        <NSpace justify="center" :size="8">
          <NButton type="primary" @click="emit('create')">
            Create application
          </NButton>
        </NSpace>
        <div class="open-by-id">
          <NInput
            :value="props.openById"
            class="mono"
            placeholder="Paste an application ID to open it"
            aria-label="Application ID"
            @update:value="emit('update:openById', $event)"
            @keyup.enter="emit('open', props.openById.trim())"
          />
          <NButton secondary :disabled="props.openById.trim() === ''" @click="emit('open', props.openById.trim())">
            Open
          </NButton>
        </div>
      </template>
    </NEmpty>
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

.apps-empty {
  padding: var(--space-8) 0;
}

.apps-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-8) 0;
}

.apps-empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
  max-width: 62ch;
}

.open-by-id {
  display: flex;
  gap: var(--space-2);
  max-width: 520px;
  margin: var(--space-4) auto 0;
}
</style>
