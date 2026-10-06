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
import { activeLocale, i18n } from "@/shared/i18n";
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
/**
 * t renders dialog copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without losing the typed draft.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

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
    :title="t('services.import.title')"
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
        {{ t("services.import.intro", { field: "services.compose_yaml" }) }}
      </p>
      <NForm label-placement="top">
        <div class="import-grid">
          <NFormItem
            :label="t('services.import.nameLabel')"
            required
            :feedback="dialog.nameError.value"
            :validation-status="dialog.nameError.value ? 'error' : undefined"
            class="field-import-name"
          >
            <NInput
              v-model:value="dialog.name.value"
              :placeholder="t('services.import.namePlaceholder')"
              :aria-label="t('services.import.nameLabel')"
            />
          </NFormItem>
          <ServerPicker
            v-model="dialog.serverId.value"
            :label="t('services.import.nodeLabel')"
            :feedback="dialog.nodeError.value"
          />
        </div>
        <NFormItem :label="t('services.import.yamlLabel')" class="field-import-yaml">
          <NInput
            v-model:value="dialog.yaml.value"
            type="textarea"
            class="mono"
            spellcheck="false"
            :autosize="{ minRows: 10, maxRows: 24 }"
            :placeholder="t('services.import.yamlExample')"
            :aria-label="t('services.import.yamlLabel')"
          />
        </NFormItem>
      </NForm>
      <NText depth="3" class="small">
        {{
          t("services.import.footnote", {
            env: dialog.envReference,
            label: "gotham.domain",
          })
        }}
      </NText>
    </NSpin>
    <template #footer>
      <NSpace :size="8" justify="end">
        <NButton @click="emit('update:show', false)">{{ t("common.actions.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="dialog.importing.value"
          @click="dialog.handleImport"
        >
          {{ t("services.import.submit") }}
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
