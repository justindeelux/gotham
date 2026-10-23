<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { EditorDraftPrompt } from "@/features/files/composables/useEditorDrafts";
import { formatDateTime } from "@/shared/utils/format";

interface Props {
  prompt: EditorDraftPrompt | null;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  restore: [];
  discard: [];
}>();

const { t } = useI18n();

const savedAt = computed<string>(() =>
  props.prompt
    ? formatDateTime(new Date(props.prompt.savedAt).toISOString())
    : "",
);
</script>

<template>
  <NModal
    class="app-modal"
    :show="prompt !== null"
    preset="card"
    style="width: 460px; max-width: 94vw"
    :title="t('files.editor.draftTitle')"
    :mask-closable="false"
    :close-on-esc="false"
  >
    <template v-if="prompt">
      <p class="small">
        {{ t("files.editor.draftMessage", { name: prompt.name, time: savedAt }) }}
      </p>
      <p class="mono draft-path">{{ prompt.path }}</p>
      <p v-if="prompt.conflict" class="draft-conflict" role="status">
        {{ t("files.editor.draftConflict") }}
      </p>
    </template>
    <template #footer>
      <div class="dialog-foot">
        <NButton type="error" secondary @click="emit('discard')">
          {{ t("files.editor.draftDiscard") }}
        </NButton>
        <NButton type="primary" @click="emit('restore')">
          {{ t("files.editor.draftRestore") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  font-size: var(--text-sm);
}

.mono {
  font-family: var(--font-mono);
}

.draft-path {
  margin: var(--space-2) 0 0;
  overflow-wrap: anywhere;
  color: var(--muted);
  font-size: var(--text-xs);
}

.draft-conflict {
  margin: var(--space-3) 0 0;
  color: var(--warn-ink);
  font-size: var(--text-sm);
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
