<script setup lang="ts">
import { NAlert, NButton, NCard, NFormItem, NSpace, NText } from "naive-ui";
import { toRef } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import BuildArgsEditor from "@/features/applications/components/BuildArgsEditor.vue";
import { useDockerfileEditor } from "@/features/applications/composables/useDockerfileEditor";
import CodeEditor from "@/features/files/components/CodeEditor.vue";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const editor = useDockerfileEditor(toRef(props, "application"));

const { t } = useI18n();
</script>

<template>
  <NCard :title="t('applications.dockerfile.title')">
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ t("applications.dockerfile.hint") }}
      </NText>
      <NAlert
        v-if="editor.saveError.value"
        type="error"
        :show-icon="true"
      >
        {{ editor.saveError.value }}
      </NAlert>
      <NFormItem :label="t('applications.dockerfile.content')">
        <CodeEditor
          v-model="editor.content.value"
          class="source-code"
          :class="{ 'source-code--invalid': !editor.contentValid.value }"
          preset="dockerfile"
          height="280px"
          :invalid="!editor.contentValid.value"
          :error="t('applications.dockerfile.contentInvalid')"
          :placeholder="t('applications.dockerfile.placeholder')"
          :aria-label="t('applications.dockerfile.content')"
        />
      </NFormItem>
      <NFormItem :label="t('applications.dockerfile.buildArgs')">
        <BuildArgsEditor v-model="editor.args.value" />
      </NFormItem>
      <NSpace align="center" :size="8" :wrap="false">
        <NButton
          type="primary"
          :disabled="editor.saveDisabled.value"
          :loading="editor.saving.value"
          @click="editor.handleSave"
        >
          {{ t("applications.dockerfile.save") }}
        </NButton>
        <NText depth="3" class="small">
          {{ t("applications.dockerfile.redeployHint") }}
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
</style>
