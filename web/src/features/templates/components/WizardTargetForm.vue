<script setup lang="ts">
import { NAlert, NForm, NFormItem, NInput } from "naive-ui";

import ResourceScopeSummary from "@/features/projects/components/ResourceScopeSummary.vue";
import ServerPicker from "@/features/projects/components/ServerPicker.vue";

interface Props {
  name: string;
  projectId: string;
  environmentId: string;
  serverId: string;
  nameError: string;
  scopeError: string;
  nodeError: string;
  createError: string | null;
}

defineProps<Props>();

const emit = defineEmits<{
  "update:name": [value: string];
  "update:projectId": [value: string];
  "update:environmentId": [value: string];
  "update:serverId": [value: string];
}>();
</script>

<template>
  <ResourceScopeSummary
    :project-id="projectId"
    :environment-id="environmentId"
    @update:project-id="(value) => emit('update:projectId', value)"
    @update:environment-id="(value) => emit('update:environmentId', value)"
  />
  <NAlert v-if="scopeError" type="error" :show-icon="true">
    {{ scopeError }}
  </NAlert>
  <NForm label-placement="top" class="wizard__metaform">
    <NFormItem
      label="Service name"
      required
      :feedback="nameError"
      :validation-status="nameError ? 'error' : undefined"
      class="field-service-name"
    >
      <NInput
        :value="name"
        aria-label="Service name"
        @update:value="(value) => emit('update:name', value)"
      />
    </NFormItem>
    <ServerPicker
      :model-value="serverId"
      :feedback="nodeError"
      @update:model-value="(value) => emit('update:serverId', value)"
    />
  </NForm>
  <NAlert v-if="createError" type="error" :show-icon="true">
    {{ createError }}
  </NAlert>
  <p class="wizard__desc">
    Creating stores the rendered document and its environment
    (<span class="mono">POST /api/v1/services</span>); a service runs
    on exactly one node. Deploy sends the project to that node's agent
    and shows what the agent reports back.
  </p>
</template>

<style scoped>
.wizard__metaform {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 var(--space-4);
}

.wizard__desc {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 720px) {
  .wizard__metaform {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
