<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NText } from "naive-ui";
import { useI18n } from "vue-i18n";

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

const { t } = useI18n();
</script>

<template>
  <NCard style="margin-top: 16px" :title="t('applications.storageTab.title')">
    <template #header-extra>
      <NButton
        type="primary"
        size="small"
        :loading="props.saving"
        :disabled="props.saveDisabled"
        @click="emit('save')"
      >
        {{ t("common.actions.save") }}
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
          <NButton size="small" @click="emit('retry')">{{ t("common.actions.retry") }}</NButton>
        </NSpace>
      </NAlert>
      <NSpin :show="props.storagesLoading">
        <StorageEditor :model-value="props.storagesDraft" @update:model-value="emit('update:draft', $event)" />
      </NSpin>
    </NSpace>
    <template #footer>
      <NText depth="3">
        {{ t("applications.storageTab.footer") }}
      </NText>
    </template>
  </NCard>
</template>
