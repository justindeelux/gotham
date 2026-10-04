<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NText } from "naive-ui";

import type { EnvVar } from "@/features/applications/api/applications";
import EnvEditor from "@/features/applications/components/EnvEditor.vue";

interface Props {
  envDraft: EnvVar[];
  envLoading: boolean;
  envError: string | null;
  saveDisabled: boolean;
  saving: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:draft": [value: EnvVar[]];
  save: [];
  retry: [];
}>();
</script>

<template>
  <NCard style="margin-top: 16px" title="Environment variables">
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
        v-if="props.envError"
        type="error"
        :show-icon="true"
      >
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.envError }}</span>
          <NButton size="small" @click="emit('retry')">Retry</NButton>
        </NSpace>
      </NAlert>
      <NSpin :show="props.envLoading">
        <EnvEditor :model-value="props.envDraft" @update:model-value="emit('update:draft', $event)" />
      </NSpin>
    </NSpace>
    <template #footer>
      <NText depth="3">
        Saving replaces the whole collection. Sealed secrets stay
        sealed, and new variables apply to the next deploy.
      </NText>
    </template>
  </NCard>
</template>
