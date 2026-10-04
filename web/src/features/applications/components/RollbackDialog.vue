<script setup lang="ts">
import { NButton, NModal, NPopconfirm, NRadio, NRadioGroup, NSpace, NText } from "naive-ui";

import type { Deployment } from "@/features/applications/api/applications";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  show: boolean;
  rollingBack: boolean;
  rollbackTarget: string;
  runningDeployments: Deployment[];
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  "update:target": [value: string];
  confirm: [];
}>();
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="Rollback to a previous release"
    style="width: 560px; max-width: 94vw"
    @update:show="emit('update:show', $event)"
  >
    <NSpace vertical :size="12">
      <NText depth="3">
        The control plane switches the image tag and restarts the container.
        Environment and volumes stay unchanged.
      </NText>
      <NRadioGroup :value="props.rollbackTarget" @update:value="emit('update:target', $event)">
        <NSpace vertical :size="8">
          <NRadio
            v-for="item in props.runningDeployments"
            :key="item.id"
            :value="item.id"
          >
            <span class="mono">{{ item.id.slice(0, 8) }}</span>
            ·
            <span class="mono">{{ item.image_tag || "untagged" }}</span>
            · {{ relativeTime(item.created_at) }}
          </NRadio>
        </NSpace>
      </NRadioGroup>
      <NPopconfirm
        :positive-button-props="{ type: 'primary' }"
        @positive-click="emit('confirm')"
      >
        <template #trigger>
          <NButton
            type="primary"
            :loading="props.rollingBack"
            :disabled="props.rollbackTarget === ''"
          >
            Rollback
          </NButton>
        </template>
        Roll back to {{ props.rollbackTarget.slice(0, 8) }}? The current container
        is kept for a roll-forward.
      </NPopconfirm>
    </NSpace>
  </NModal>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
