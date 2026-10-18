<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { useI18n } from "vue-i18n";

import { encodingLabel } from "@/features/files/utils/editor";
import type { EditorEncoding } from "@/features/files/utils/editor";

interface Props {
  show: boolean;
  /** name is the tab's display name. */
  name: string;
  /** encoding is the charset the save was blocked by. */
  encoding: EditorEncoding;
  /** characters lists the distinct characters that cannot be represented. */
  characters: string[];
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [show: boolean];
  useUtf8: [];
}>();

const { t } = useI18n();
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 460px; max-width: 94vw"
    :title="t('files.editor.encodingUnsupportedTitle', { encoding: encodingLabel(props.encoding) })"
    :mask-closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <p class="small">
      {{
        t("files.editor.encodingUnsupportedMessage", {
          name: props.name,
          encoding: encodingLabel(props.encoding),
        })
      }}
    </p>
    <ul
      v-if="props.characters.length > 0"
      class="enc-chars"
      :aria-label="t('files.editor.encodingUnsupportedTitle', { encoding: encodingLabel(props.encoding) })"
    >
      <li v-for="character in props.characters" :key="character" class="mono">
        {{ character }}
      </li>
    </ul>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton type="primary" @click="emit('useUtf8')">
          {{ t("files.editor.encodingUseUtf8") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.enc-chars {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
  margin: var(--space-3) 0 0;
  padding: 0;
  list-style: none;
}

.enc-chars li {
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--bg-2);
  font-size: var(--text-sm);
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
