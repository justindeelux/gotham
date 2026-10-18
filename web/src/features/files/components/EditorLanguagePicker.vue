<script setup lang="ts">
import { NPopselect } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import {
  editorLanguages,
  languageLabel,
} from "@/features/files/utils/editor";
import type { EditorLanguageId } from "@/features/files/utils/editor";

interface Props {
  /** language is the tab's current (detected or manually picked) mode. */
  language: EditorLanguageId;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  select: [language: EditorLanguageId];
}>();

const { t } = useI18n();

/** options lists every shipped language under its full name. */
const options = computed(() =>
  editorLanguages.map((language) => ({
    label: language.name,
    value: language.id,
  })),
);

/** selectLanguage forwards the picker value to the tab store. */
function selectLanguage(value: string | number): void {
  emit("select", value as EditorLanguageId);
}
</script>

<template>
  <NPopselect
    size="small"
    :value="props.language"
    :options="options"
    @update:value="selectLanguage"
  >
    <button
      type="button"
      class="ed-language"
      :aria-label="`${t('files.editor.language')}: ${languageLabel(props.language)}`"
      :title="t('files.editor.language')"
    >
      {{ languageLabel(props.language) }}
    </button>
  </NPopselect>
</template>

<style scoped>
.ed-language {
  padding: 0 var(--space-1);
  border: 0;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  cursor: pointer;
}

.ed-language:hover,
.ed-language:focus-visible {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
}
</style>
