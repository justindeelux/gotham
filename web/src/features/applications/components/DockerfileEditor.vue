<script setup lang="ts">
import { NAlert, NButton, NCard, NFormItem, NInput, NSpace, NText } from "naive-ui";
import { toRef } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import BuildArgsEditor from "@/features/applications/components/BuildArgsEditor.vue";
import { useDockerfileEditor } from "@/features/applications/composables/useDockerfileEditor";

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
        <NInput
          v-model:value="editor.content.value"
          type="textarea"
          class="mono"
          :rows="12"
          :placeholder="t('applications.dockerfile.placeholder')"
          :status="editor.contentValid.value ? undefined : 'error'"
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
</style>
