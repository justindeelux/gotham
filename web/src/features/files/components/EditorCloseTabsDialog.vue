<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

interface Props {
  show: boolean;
  /** paths are the root-relative paths of the dirty tabs being closed. */
  paths: string[];
  /** canSave is false when the caller has no write access to the root. */
  canSave: boolean;
  /** saving blocks every choice while the dirty buffers are being written. */
  saving: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  save: [];
  discard: [];
  cancel: [];
}>();

const { t } = useI18n();

const saveLabel = computed<string>(() =>
  props.paths.length > 1
    ? String(t("files.editor.closeDirtySaveAll"))
    : String(t("files.editor.closeDirtySave")),
);

/** onShow closes the prompt only when it is dismissed while idle. */
function onShow(value: boolean): void {
  if (!value && !props.saving) {
    emit("cancel");
  }
}
</script>

<template>
  <NModal
    class="app-modal files-editor-close-modal"
    :show="show"
    preset="card"
    style="width: 440px; max-width: 94vw"
    :title="t('files.editor.closeDirtyTitle')"
    :mask-closable="false"
    :close-on-esc="false"
    @update:show="onShow"
  >
    <p class="small">{{ t("files.editor.closeDirtyMessage") }}</p>
    <ul class="ed-unsaved" :aria-label="t('files.editor.closeDirtyList')">
      <li v-for="path in paths" :key="path" class="mono">{{ path }}</li>
    </ul>
    <template #footer>
      <div class="dialog-foot">
        <NButton :disabled="saving" @click="emit('cancel')">
          {{ t("files.editor.closeAllCancel") }}
        </NButton>
        <NButton type="error" :disabled="saving" @click="emit('discard')">
          {{ t("files.editor.closeDirtyDiscard") }}
        </NButton>
        <NButton
          type="primary"
          :loading="saving"
          :disabled="!canSave"
          @click="emit('save')"
        >
          {{ saveLabel }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}

.ed-unsaved {
  margin: var(--space-2) 0 0;
  padding-left: var(--space-4);
  max-height: 40vh;
  overflow-y: auto;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.ed-unsaved li {
  overflow-wrap: anywhere;
}

.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-sm);
}
</style>
