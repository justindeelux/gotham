<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NText } from "naive-ui";

import type { StorageMapping } from "@/features/applications/api/applications";
import StorageEditor from "@/features/applications/components/StorageEditor.vue";

interface Props {
  storagesDraft: StorageMapping[];
  storagesLoading: boolean;
  storagesError: string | null;
  saveDisabled: boolean;
  saving: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:draft": [value: StorageMapping[]];
  save: [];
  retry: [];
}>();
</script>

<template>
  <NCard style="margin-top: 16px" title="Volumes">
    <template #header-extra>
      <NButton
        type="primary"
        size="small"
        :loading="props.saving"
        :disabled="props.saveDisabled"
        @click="emit('save')"
      >
        Save
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NAlert
        v-if="props.storagesError"
        type="error"
        :show-icon="true"
      >
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.storagesError }}</span>
          <NButton size="small" @click="emit('retry')">Retry</NButton>
        </NSpace>
      </NAlert>
      <NSpin :show="props.storagesLoading">
        <StorageEditor :model-value="props.storagesDraft" @update:model-value="emit('update:draft', $event)" />
      </NSpin>
    </NSpace>
    <template #footer>
      <NText depth="3">
        Saving replaces the whole collection. Volumes live on the node,
        so data survives redeploys and rollbacks.
      </NText>
    </template>
  </NCard>
</template>
