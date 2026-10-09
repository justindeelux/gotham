<script setup lang="ts">
import { NAlert, NButton, NCard, NFormItem, NInput, NSelect, NSpace, NText } from "naive-ui";
import { toRef } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import { useComposeEditor } from "@/features/applications/composables/useComposeEditor";

interface Props {
  application: Application;
  /** canWrite gates editing: readers see the document read-only. */
  canWrite: boolean;
}

const props = defineProps<Props>();

const editor = useComposeEditor(toRef(props, "application"));

const { t } = useI18n();
</script>

<template>
  <NCard :title="t('applications.compose.title')">
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ t("applications.compose.hint") }}
      </NText>
      <NAlert
        v-if="editor.saveError.value"
        type="error"
        :show-icon="true"
      >
        {{ editor.saveError.value }}
      </NAlert>
      <NFormItem
        v-if="!editor.isRepoMode.value"
        :label="t('applications.compose.content')"
      >
        <NInput
          v-model:value="editor.content.value"
          type="textarea"
          class="mono"
          :rows="12"
          :placeholder="t('applications.compose.placeholder')"
          :disabled="!props.canWrite"
          :status="editor.contentValid.value ? undefined : 'error'"
        />
      </NFormItem>
      <NFormItem
        v-else
        :label="t('applications.compose.file')"
      >
        <NInput
          v-model:value="editor.file.value"
          class="mono"
          :placeholder="t('applications.compose.filePlaceholder')"
          :disabled="!props.canWrite"
          :status="editor.fileValid.value ? undefined : 'error'"
        />
      </NFormItem>
      <NAlert class="notice-inline" type="warning" :show-icon="false">
        {{ t("applications.compose.secretsHint") }}
      </NAlert>
      <NFormItem :label="t('applications.compose.service')">
        <NSelect
          v-model:value="editor.service.value"
          :options="editor.serviceOptions.value"
          :placeholder="t('applications.compose.servicePlaceholder')"
          :disabled="!props.canWrite"
          filterable
          tag
        />
      </NFormItem>
      <NSpace v-if="props.canWrite" align="center" :size="8" :wrap="false">
        <NButton
          type="primary"
          :disabled="editor.saveDisabled.value"
          :loading="editor.saving.value"
          @click="editor.handleSave"
        >
          {{ t("applications.compose.save") }}
        </NButton>
        <NText depth="3" class="small">
          {{ t("applications.compose.redeployHint") }}
        </NText>
      </NSpace>
    </NSpace>
  </NCard>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

/* NAlert has no size prop: the compact inline look is tighter padding. */
.notice-inline {
  padding: 6px 10px;
  font-size: var(--text-xs);
}
</style>
