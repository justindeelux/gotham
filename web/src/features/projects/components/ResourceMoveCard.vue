<script setup lang="ts">
import { NAlert, NButton, NCard } from "naive-ui";
import { computed, ref, watch } from "vue";

import ResourceScopeSummary from "@/features/projects/components/ResourceScopeSummary.vue";
import ServerPicker from "@/features/projects/components/ServerPicker.vue";

const props = defineProps<{
  projectId: string;
  environmentId: string;
  serverId: string;
  /** Saving state owned by the parent (the PATCH is its call). */
  saving: boolean;
  /** Server refusal rendered inline (the contract's exact 409 text). */
  error: string | null;
  /**
   * Pinned node: the backend refuses a server change for this resource
   * (a deployed service, a created database), so the select is disabled
   * with the reason up front instead of after a failed Save.
   */
  serverPinned?: boolean;
  /** Explanation shown when the node select is pinned. */
  serverPinnedReason?: string;
}>();

const emit = defineEmits<{
  save: [scope: { projectId: string; environmentId: string; serverId: string }];
}>();

/**
 * Server and environment settings shared by the three resource detail pages
 * (PE-5, Linear JUS-34). The selects are owned here; the PATCH stays with
 * the parent, which passes its saving state and the refusal back in. One
 * submit guard: Save stays disabled while nothing changed, while a save is
 * in flight, or while the scope is incomplete.
 */
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

/** scopeComplete gates Save: the move needs a full target scope and a node. */
const scopeComplete = computed<boolean>(
  () =>
    selectedProjectId.value !== "" &&
    selectedEnvironmentId.value !== "" &&
    selectedServerId.value !== "",
);

/** unchanged reports whether the selects still match the stored resource. */
const unchanged = computed<boolean>(
  () =>
    selectedProjectId.value === props.projectId &&
    selectedEnvironmentId.value === props.environmentId &&
    selectedServerId.value === props.serverId,
);

/** saveDisabled is the single submit guard for the settings form. */
const saveDisabled = computed<boolean>(
  () => props.saving || unchanged.value || !scopeComplete.value,
);

/** handleSave emits the chosen scope; the parent PATCHes and reports back. */
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
  <NCard title="Server and environment" size="small">
    <div class="move-card">
      <ResourceScopeSummary
        verb="Located in"
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
        Moving to an environment that already holds this name is refused, as
        is changing the node while a deploy runs.
      </p>
      <div class="actions">
        <NButton
          type="primary"
          size="small"
          :loading="props.saving"
          :disabled="saveDisabled"
          @click="handleSave"
        >
          Save location
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
