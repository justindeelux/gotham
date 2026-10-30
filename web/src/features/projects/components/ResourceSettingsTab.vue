<script setup lang="ts">
import { NButton, NCard, NPopconfirm, NSpace } from "naive-ui";
import { computed, useSlots } from "vue";

import ResourceMoveCard from "@/features/projects/components/ResourceMoveCard.vue";

const props = withDefaults(
  defineProps<{
    projectId: string;
    environmentId: string;
    serverId: string;
    saving: boolean;
    error: string | null;
    serverPinned?: boolean;
    serverPinnedReason?: string;
    dangerTitle: string;
    dangerHint: string;
    dangerLabel: string;
    dangerConfirm?: string;
    dangerBusy?: boolean;
    dangerDisabled?: boolean;
  }>(),
  {
    serverPinned: false,
    serverPinnedReason: "",
    dangerConfirm: "",
    dangerBusy: false,
    dangerDisabled: false,
  },
);

const emit = defineEmits<{
  save: [scope: { projectId: string; environmentId: string; serverId: string }];
  delete: [];
}>();

const slots = useSlots();
const hasDefaultSlot = computed(() => Boolean(slots.default));

function handleSave(scope: {
  projectId: string;
  environmentId: string;
  serverId: string;
}): void {
  emit("save", scope);
}
</script>

<template>
  <NSpace vertical :size="16">
    <div
      class="settings-grid"
      :class="{ 'settings-grid--single': !hasDefaultSlot }"
    >
      <ResourceMoveCard
        :project-id="props.projectId"
        :environment-id="props.environmentId"
        :server-id="props.serverId"
        :saving="props.saving"
        :error="props.error"
        :server-pinned="props.serverPinned"
        :server-pinned-reason="props.serverPinnedReason"
        @save="handleSave"
      />

      <div v-if="hasDefaultSlot" class="settings-grid__slot">
        <slot />
      </div>
    </div>

    <NCard :title="props.dangerTitle">
      <NSpace vertical :size="12">
        <span>{{ props.dangerHint }}</span>
        <NSpace>
          <slot name="danger-action">
            <NPopconfirm @positive-click="emit('delete')">
              <template #trigger>
                <NButton
                  type="error"
                  ghost
                  :loading="props.dangerBusy"
                  :disabled="props.dangerDisabled"
                >
                  {{ props.dangerLabel }}
                </NButton>
              </template>
              {{ props.dangerConfirm }}
            </NPopconfirm>
          </slot>
        </NSpace>
      </NSpace>
    </NCard>
  </NSpace>
</template>

<style scoped>
.settings-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--space-4);
  align-items: stretch;
}

.settings-grid > :deep(.n-card) {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.settings-grid > :deep(.n-card > .n-card-content) {
  display: flex;
  flex: 1;
  flex-direction: column;
}

.settings-grid :deep(.move-card) {
  flex: 1;
}

.settings-grid :deep(.move-card > .actions) {
  margin-top: auto;
}

.settings-grid__slot {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.settings-grid__slot > :deep(.n-card:only-child) {
  height: 100%;
}

@media (min-width: 769px) {
  .settings-grid:not(.settings-grid--single) {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
