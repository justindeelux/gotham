<script setup lang="ts">
import { NButton, NModal, NRadio, NRadioGroup } from "naive-ui";
import { useI18n } from "vue-i18n";

import {
  editorEncodings,
  encodingLabel,
} from "@/features/files/utils/editor";
import type { EditorEncoding } from "@/features/files/utils/editor";

interface Props {
  show: boolean;
  /** encoding is the tab's current save charset. */
  encoding: EditorEncoding;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [show: boolean];
  select: [encoding: EditorEncoding];
}>();

const { t } = useI18n();

/** close dismisses the dialog without changing the encoding. */
function close(): void {
  emit("update:show", false);
}

/** pick applies one charset and closes; the caller performs the re-save. */
function pick(value: string | number): void {
  if (value !== props.encoding) {
    emit("select", value as EditorEncoding);
  }
  close();
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 380px; max-width: 94vw"
    :title="t('files.editor.encodingTitle')"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <p class="small muted">{{ t("files.editor.encodingMessage") }}</p>
    <NRadioGroup
      class="enc-options"
      :value="props.encoding"
      :aria-label="t('files.editor.encodingTitle')"
      @update:value="pick"
    >
      <NRadio
        v-for="option in editorEncodings"
        :key="option"
        :value="option"
        :label="encodingLabel(option)"
      />
    </NRadioGroup>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="close">{{ t("common.actions.cancel") }}</NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.enc-options {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-top: var(--space-3);
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
