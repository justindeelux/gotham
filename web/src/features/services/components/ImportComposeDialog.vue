<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSpace,
  NSpin,
  NText,
} from "naive-ui";
import { toRef } from "vue";

import { useImportService } from "@/features/services/composables/useImportService";
import type { Service } from "@/features/services/api/services";
import ResourceScopeSummary from "@/features/projects/components/ResourceScopeSummary.vue";
import ServerPicker from "@/features/projects/components/ServerPicker.vue";

interface Props {
  show: boolean;
  /** Project the service is created in (the route's, changeable). */
  projectId?: string;
  /** Environment the service is created in (the route's, changeable). */
  environmentId?: string;
}

const props = withDefaults(defineProps<Props>(), { projectId: "", environmentId: "" });

const emit = defineEmits<{
  "update:show": [value: boolean];
  created: [service: Service];
}>();

/**
 * ImportComposeDialog renders the compose import modal and its form. Hosted
 * by the environment page with the route's scope (PE-5, Linear JUS-34).
 */
const dialog = useImportService(
  toRef(props, "show"),
  { projectId: toRef(props, "projectId"), environmentId: toRef(props, "environmentId") },
  (service) => emit("created", service),
);
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="Import compose"
    style="width: 640px; max-width: 96vw"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpin :show="dialog.importing.value">
      <ResourceScopeSummary
        :project-id="dialog.scopeProjectId.value"
        :environment-id="dialog.scopeEnvironmentId.value"
        @update:project-id="(value) => (dialog.scopeProjectId.value = value)"
        @update:environment-id="(value) => (dialog.scopeEnvironmentId.value = value)"
      />
      <NAlert v-if="dialog.scopeError.value" type="error" :show-icon="true" class="mb-3">
        {{ dialog.scopeError.value }}
      </NAlert>
      <NAlert v-if="dialog.importError.value" type="error" :show-icon="true" class="mb-3">
        {{ dialog.importError.value }}
      </NAlert>
      <p class="small muted mb-3">
        Paste an existing compose document. The control plane stores it
        verbatim in <span class="mono">services.compose_yaml</span> and
        validates it before saving.
      </p>
      <NForm label-placement="top">
        <div class="import-grid">
          <NFormItem
            label="Service name"
            required
            :feedback="dialog.nameError.value"
            :validation-status="dialog.nameError.value ? 'error' : undefined"
            class="field-import-name"
          >
            <NInput
              v-model:value="dialog.name.value"
              placeholder="blog-staging"
              aria-label="Service name"
            />
          </NFormItem>
          <ServerPicker
            v-model="dialog.serverId.value"
            label="Node"
            :feedback="dialog.nodeError.value"
          />
        </div>
        <NFormItem label="compose.yaml" class="field-import-yaml">
          <NInput
            v-model:value="dialog.yaml.value"
            type="textarea"
            class="mono"
            spellcheck="false"
            :autosize="{ minRows: 10, maxRows: 24 }"
            placeholder="services:&#10;  web:&#10;    image: nginx:1.27-alpine"
            aria-label="compose.yaml"
          />
        </NFormItem>
      </NForm>
      <NText depth="3" class="small">
        <span class="mono">{{ dialog.envReference }}</span> references are substituted from
        the environment before the agent validates the document. A compose
        service is routed by the
        <span class="mono">gotham.domain</span> label. Files larger than
        1&nbsp;MiB are rejected by the API.
      </NText>
    </NSpin>
    <template #footer>
      <NSpace :size="8" justify="end">
        <NButton @click="emit('update:show', false)">Cancel</NButton>
        <NButton
          type="primary"
          :loading="dialog.importing.value"
          @click="dialog.handleImport"
        >
          Import service
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.import-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--form-item-gap) var(--space-4);
}

.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mb-3 {
  margin-bottom: var(--space-3);
}

@media (max-width: 860px) {
  .import-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
