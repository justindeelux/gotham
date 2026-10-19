<script setup lang="ts">
import { NAlert, NButton, NCard, NFormItem, NInput, NSelect, NSpace, NText, useMessage } from "naive-ui";
import { toRef } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import { useComposeEditor } from "@/features/applications/composables/useComposeEditor";
import CodeEditor from "@/features/files/components/CodeEditor.vue";

interface Props {
  application: Application;
  /** canWrite gates editing: readers see the document read-only. */
  canWrite: boolean;
}

const props = defineProps<Props>();

const editor = useComposeEditor(toRef(props, "application"));

const { t } = useI18n();
const message = useMessage();

/** save confirms a landed write; the error stays in the banner. */
async function save(): Promise<void> {
  if (await editor.handleSave()) {
    message.success(t("applications.compose.saved"));
  }
}
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
        <CodeEditor
          v-model="editor.content.value"
          class="source-code"
          :class="{ 'source-code--invalid': !editor.contentValid.value }"
          preset="yaml"
          height="280px"
          :read-only="!props.canWrite"
          :invalid="!editor.contentValid.value"
          :error="t('applications.compose.contentInvalid')"
          :placeholder="t('applications.compose.placeholder')"
          :aria-label="t('applications.compose.content')"
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
      <NAlert class="notice-inline" type="warning" :show-icon="false" :bordered="false">
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
          @click="save"
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

.source-code {
  width: 100%;
}

.source-code--invalid {
  border-color: var(--danger);
}

/* NAlert has no size prop: the light inline look overrides its theme vars
 * (subtle tint, tight padding, 13px text) and swaps the filled border box
 * for a left accent bar. */
.notice-inline {
  --n-color: var(--warn-soft) !important;
  --n-padding: 8px 12px !important;
  --n-font-size: 13px !important;
  --n-line-height: 1.5 !important;
  border-left: 3px solid var(--warn);
  border-radius: var(--radius-sm);
}
</style>
