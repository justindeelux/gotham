<script setup lang="ts">
import { NAlert, NButton, NCard } from "naive-ui";
import { computed, ref, watch } from "vue";

import ResourceScopeSummary from "@/features/projects/components/ResourceScopeSummary.vue";
import ServerPicker from "@/features/projects/components/ServerPicker.vue";
import { activeLocale, i18n } from "@/shared/i18n";

const props = defineProps<{
  projectId: string;
  environmentId: string;
  serverId: string;
  saving: boolean;
  error: string | null;
  serverPinned?: boolean;
  serverPinnedReason?: string;
}>();

const emit = defineEmits<{
  save: [scope: { projectId: string; environmentId: string; serverId: string }];
}>();

const selectedProjectId = ref(props.projectId);
const selectedEnvironmentId = ref(props.environmentId);
const selectedServerId = ref(props.serverId);

watch(
  () => [props.projectId, props.environmentId, props.serverId] as const,
  ([projectId, environmentId, serverId]) => {
    selectedProjectId.value = projectId;
    selectedEnvironmentId.value = environmentId;
    selectedServerId.value = serverId;
  },
);

function t(key: string): string {
  void activeLocale.value;
  return String(i18n.global.t(key));
}

const locatedVerb = computed<string>(() => t("projects.scope.locatedIn"));

const scopeComplete = computed<boolean>(
  () =>
    selectedProjectId.value !== "" &&
    selectedEnvironmentId.value !== "" &&
    selectedServerId.value !== "",
);

const unchanged = computed<boolean>(
  () =>
    selectedProjectId.value === props.projectId &&
    selectedEnvironmentId.value === props.environmentId &&
    selectedServerId.value === props.serverId,
);

const saveDisabled = computed<boolean>(
  () => props.saving || unchanged.value || !scopeComplete.value,
);

function handleSave(): void {
  if (saveDisabled.value) {
    return;
  }
  emit("save", {
    projectId: selectedProjectId.value,
    environmentId: selectedEnvironmentId.value,
    serverId: selectedServerId.value,
  });
}
</script>

<template>
  <NCard :title="t('projects.move.title')" size="small">
    <div class="move-card">
      <ResourceScopeSummary
        :verb="locatedVerb"
        :project-id="selectedProjectId"
        :environment-id="selectedEnvironmentId"
        @update:project-id="(value) => (selectedProjectId = value)"
        @update:environment-id="(value) => (selectedEnvironmentId = value)"
      />
      <ServerPicker v-model="selectedServerId" :disabled="props.serverPinned === true" />
      <NAlert
        v-if="props.serverPinned === true && props.serverPinnedReason"
        type="info"
        :show-icon="true"
      >
        {{ props.serverPinnedReason }}
      </NAlert>
      <NAlert v-if="props.error" type="error" :show-icon="true">
        {{ props.error }}
      </NAlert>
      <p class="hint">
        {{ t("projects.move.hint") }}
      </p>
      <div class="actions">
        <NButton
          type="primary"
          size="small"
          :loading="props.saving"
          :disabled="saveDisabled"
          @click="handleSave"
        >
          {{ t("projects.move.save") }}
        </NButton>
      </div>
    </div>
  </NCard>
</template>

<style scoped>
.move-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  max-width: 640px;
}

.hint {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.actions {
  display: flex;
  justify-content: flex-end;
}
</style>
