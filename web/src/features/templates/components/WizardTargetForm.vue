<script setup lang="ts">
import { NAlert, NForm, NFormItem, NInput } from "naive-ui";

import ResourceScopeSummary from "@/features/projects/components/ResourceScopeSummary.vue";
import ServerPicker from "@/features/projects/components/ServerPicker.vue";
import { activeLocale, i18n } from "@/shared/i18n";

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

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

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
  <NForm label-placement="top" class="wizard__metaform form--grid">
    <NFormItem
      :label="t('templates.target.nameLabel')"
      required
      :feedback="nameError"
      :validation-status="nameError ? 'error' : undefined"
      class="field-service-name"
    >
      <NInput
        :value="name"
        :aria-label="t('templates.target.nameLabel')"
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
</template>

<style scoped>
.wizard__metaform {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--form-item-gap) var(--space-4);
  align-items: start;
}

@media (max-width: 720px) {
  .wizard__metaform {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
