<script setup lang="ts">
import { NPopselect } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import EditorLanguagePicker from "@/features/files/components/EditorLanguagePicker.vue";
import { EDITOR_TAB_SIZES } from "@/features/files/schemas/editorSettings";
import type { EditorTab } from "@/features/files/stores/editor";
import {
  encodingLabel,
  languageLabel,
} from "@/features/files/utils/editor";
import type { EditorEol, EditorLanguageId } from "@/features/files/utils/editor";
import { formatBytes } from "@/shared/utils/format";

interface Props {
  tab: EditorTab;
  cursor: { line: number; column: number };
  historyCount: number;
  canWrite: boolean;
  tabSize: number;
  dirty: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  selectLanguage: [language: EditorLanguageId];
  setEol: [eol: EditorEol];
  setTabSize: [size: number];
  openEncoding: [];
  openHistory: [];
}>();

const { t } = useI18n();

const isEditable = computed(
  () => props.tab.kind === "file" && props.tab.status === "ready",
);

const isReadOnly = computed(
  () =>
    props.tab.kind === "diff" ||
    props.tab.status === "mixed" ||
    props.tab.status === "large" ||
    props.tab.status === "binary" ||
    props.tab.truncated ||
    (isEditable.value && !props.canWrite),
);

const canEdit = computed(
  () => isEditable.value && props.canWrite && !props.tab.truncated,
);

const fullPath = computed(() =>
  props.tab.root ? `${props.tab.root}/${props.tab.path}` : props.tab.path,
);

const eol = computed<string>(() => (props.tab.eol === "\r\n" ? "CRLF" : "LF"));

const eolOptions = [
  { label: "LF", value: "\n" },
  { label: "CRLF", value: "\r\n" },
];

const tabSizeOptions = EDITOR_TAB_SIZES.map((size) => ({
  label: String(size),
  value: size,
}));

function pickEol(value: string | number): void {
  emit("setEol", value as EditorEol);
}

function pickTabSize(value: string | number): void {
  emit("setTabSize", Number(value));
}
</script>

<template>
  <div class="ed-foot">
    <span class="ed-path mono" :title="fullPath">{{ fullPath }}</span>
    <span class="grow">{{ props.dirty ? t("files.editor.unsaved") : "" }}</span>
    <span v-if="isReadOnly" class="ed-readonly">{{ t("files.editor.readOnly") }}</span>
    <button
      v-if="canEdit"
      type="button"
      class="ed-item mono"
      :title="t('files.editor.historyShow', { count: props.historyCount })"
      @click="emit('openHistory')"
    >
      {{ t("files.editor.historyCount", { count: props.historyCount }) }}
    </button>
    <span v-else-if="isEditable" class="mono">
      {{ t("files.editor.historyCount", { count: props.historyCount }) }}
    </span>
    <span v-if="isEditable" class="mono">
      {{ t("files.editor.lineColumn", { line: props.cursor.line, column: props.cursor.column }) }}
    </span>
    <button
      v-if="canEdit"
      type="button"
      class="ed-item mono"
      :aria-label="`${t('files.editor.encodingTitle')}: ${encodingLabel(props.tab.encoding)}`"
      :title="t('files.editor.encodingTitle')"
      @click="emit('openEncoding')"
    >
      {{ encodingLabel(props.tab.encoding) }}
    </button>
    <span v-else-if="isEditable" class="mono">
      {{ encodingLabel(props.tab.encoding) }}
    </span>
    <NPopselect
      v-if="canEdit"
      size="small"
      :value="props.tab.eol"
      :options="eolOptions"
      @update:value="pickEol"
    >
      <button
        type="button"
        class="ed-item mono"
        :aria-label="`${t('files.editor.lineEndingTitle')}: ${eol}`"
        :title="t('files.editor.lineEndingTitle')"
      >
        {{ eol }}
      </button>
    </NPopselect>
    <span v-else-if="isEditable" class="mono">{{ eol }}</span>
    <NPopselect
      v-if="isEditable"
      size="small"
      :value="props.tabSize"
      :options="tabSizeOptions"
      @update:value="pickTabSize"
    >
      <button
        type="button"
        class="ed-item mono"
        :title="t('files.editor.settings.tabSize')"
      >
        {{ t("files.editor.tabSizeValue", { size: props.tabSize }) }}
      </button>
    </NPopselect>
    <EditorLanguagePicker
      v-if="isEditable"
      :language="props.tab.language"
      @select="(language: EditorLanguageId) => emit('selectLanguage', language)"
    />
    <span v-else-if="props.tab.kind === 'diff' && props.tab.status === 'ready'" class="mono">
      {{ languageLabel(props.tab.language) }}
    </span>
    <span class="mono">{{ formatBytes(props.tab.size) }}</span>
  </div>
</template>

<style scoped>
.ed-foot {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: nowrap;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  padding-top: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
}

.ed-foot > * {
  flex: 0 0 auto;
}

.ed-foot .grow {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ed-path {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ed-readonly {
  padding: 0 var(--space-1);
  border-radius: var(--radius-sm);
  background: var(--accent-softer);
  color: var(--fg);
}

.ed-item {
  padding: 0 var(--space-1);
  border: 0;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  cursor: pointer;
}

.ed-item:hover,
.ed-item:focus-visible {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
}
</style>
