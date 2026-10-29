<script setup lang="ts">
import { NButton, NCard, NPopconfirm, NSpace } from "naive-ui";

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
  }>(),
  {
    serverPinned: false,
    serverPinnedReason: "",
    dangerConfirm: "",
    dangerBusy: false,
  },
);

const emit = defineEmits<{
  save: [scope: { projectId: string; environmentId: string; serverId: string }];
  delete: [];
}>();

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

    <slot />

    <NCard :title="props.dangerTitle">
      <NSpace vertical :size="12">
        <span>{{ props.dangerHint }}</span>
        <NSpace>
          <slot name="danger-action">
            <NPopconfirm @positive-click="emit('delete')">
              <template #trigger>
                <NButton type="error" ghost :loading="props.dangerBusy">
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
