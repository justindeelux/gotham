<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";

import EditorGotoLine from "@/features/files/components/EditorGotoLine.vue";
import EditorSearchBar from "@/features/files/components/EditorSearchBar.vue";
import { useEditorFind } from "@/features/files/composables/useEditorFind";
import { createEditorSession } from "@/features/files/composables/useCodeMirror";
import type { EditorAccessibility } from "@/features/files/composables/useCodeMirror";
import type { EditorSession } from "@/features/files/composables/useCodeMirror";
import { CODE_EDITOR_PRESETS } from "@/features/files/editor/presets";
import type { CodeEditorPreset } from "@/features/files/editor/presets";
import "@/features/files/locales/editorMessages";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import type { EditorLanguageId } from "@/features/files/utils/editor";

interface Props {
  modelValue: string;
  language?: EditorLanguageId;
  preset?: CodeEditorPreset;
  readOnly?: boolean;
  wrap?: boolean;
  height?: string;
  placeholder?: string;
  ariaLabel?: string;
  invalid?: boolean;
  error?: string;
}

const props = withDefaults(defineProps<Props>(), {
  language: undefined,
  preset: undefined,
  readOnly: false,
  wrap: undefined,
  height: "320px",
  placeholder: undefined,
  ariaLabel: undefined,
  invalid: false,
  error: undefined,
});

const emit = defineEmits<{
  "update:modelValue": [value: string];
  save: [value: string];
}>();

const settingsStore = useEditorSettingsStore();
const { t } = useI18n();
const host = ref<HTMLElement | null>(null);
let session: EditorSession | null = null;
let disposed = false;

const {
  findOpen,
  replaceExpanded,
  focusReplace,
  gotoOpen,
  query: findQuery,
  replacement: findReplacement,
  caseSensitive: findCaseSensitive,
  wholeWord: findWholeWord,
  regexp: findRegexp,
  total: findTotal,
  current: findCurrent,
  capped: findCapped,
  gotoCurrent,
  gotoTotal,
  canReplace,
  patternError: findPatternError,
  openFind,
  closeFind,
  toggleReplace,
  setQuery: setFindQuery,
  setReplacement: setFindReplacement,
  toggleCase,
  toggleWord,
  toggleRegex,
  next: nextMatch,
  previous: previousMatch,
  replaceMatch,
  replaceEveryMatch,
  openGotoLine,
  closeGotoLine,
  jumpToLine,
  queueStatus: queueFindStatus,
} = useEditorFind({
  session: () => session,
  tabId: () => "code-editor",
  canReplace: () => !props.readOnly,
});

const errorId = `code-editor-error-${useId()}`;

const ariaLabel = computed<string>(
  () => props.ariaLabel ?? t("files.editor.codeEditorLabel"),
);

const accessibility = computed<EditorAccessibility>(() => ({
  ariaLabel: ariaLabel.value,
  placeholder: props.placeholder,
  invalid: props.invalid,
  describedBy: props.invalid && props.error ? errorId : undefined,
}));

const language = computed<EditorLanguageId>(
  () => props.language ?? (props.preset ? CODE_EDITOR_PRESETS[props.preset] : "plain"),
);

const effectiveSettings = computed<EditorSettings>(() =>
  props.wrap === undefined
    ? settingsStore.settings
    : { ...settingsStore.settings, wrap: props.wrap },
);

onMounted(() => {
  const element = host.value;
  if (!element) {
    return;
  }
  void createEditorSession({
    parent: element,
    doc: props.modelValue,
    language: language.value,
    ariaLabel: accessibility.value.ariaLabel,
    placeholder: accessibility.value.placeholder,
    invalid: accessibility.value.invalid,
    describedBy: accessibility.value.describedBy,
    readOnly: props.readOnly,
    settings: effectiveSettings.value,
    onChange: (doc) => {
      emit("update:modelValue", doc);
      queueFindStatus();
    },
    onSave: () => emit("save", session?.getDoc() ?? props.modelValue),
    onFind: () => openFind(false),
    onReplace: () => openFind(true),
    onGoToLine: () => openGotoLine(),
    onFindNext: () => {
      openFind(false);
      nextMatch();
    },
    onFindPrevious: () => {
      openFind(false);
      previousMatch();
    },
  }).then((created) => {
    if (disposed) {
      created.destroy();
      return;
    }
    session = created;
    created.setDoc(props.modelValue);
    created.setReadOnly(props.readOnly);
    created.setSettings(effectiveSettings.value);
    created.setAccessibility(accessibility.value);
  });
});

watch(
  () => props.modelValue,
  (doc) => session?.setDoc(doc),
);
watch(language, (next) => session?.setLanguage(next));
watch(effectiveSettings, (next) => session?.setSettings(next), { deep: true });
watch(
  () => props.readOnly,
  (readOnly) => session?.setReadOnly(readOnly),
);
watch(accessibility, (next) => session?.setAccessibility(next));

onBeforeUnmount(() => {
  disposed = true;
  session?.destroy();
  session = null;
});
</script>

<template>
  <div class="code-editor" :style="{ height }">
    <EditorSearchBar
      :show="findOpen"
      :expanded="replaceExpanded"
      :can-replace="canReplace"
      :query="findQuery"
      :replacement="findReplacement"
      :case-sensitive="findCaseSensitive"
      :whole-word="findWholeWord"
      :regexp="findRegexp"
      :pattern-error="findPatternError"
      :total="findTotal"
      :capped="findCapped"
      :current="findCurrent"
      :focus-replace="focusReplace"
      @close="closeFind"
      @update:query="setFindQuery"
      @update:replacement="setFindReplacement"
      @toggle:expanded="toggleReplace"
      @toggle:case="toggleCase"
      @toggle:word="toggleWord"
      @toggle:regex="toggleRegex"
      @next="nextMatch"
      @previous="previousMatch"
      @replace="replaceMatch"
      @replace-all="replaceEveryMatch"
    />
    <div ref="host" class="code-editor-host" />
    <p v-if="invalid && error" :id="errorId" class="code-editor-error">
      {{ error }}
    </p>
    <EditorGotoLine
      :show="gotoOpen"
      :current="gotoCurrent"
      :total="gotoTotal"
      :jump="jumpToLine"
      @close="closeGotoLine"
    />
  </div>
</template>

<style scoped>
.code-editor {
  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 120px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-warm);
}

.code-editor-host {
  flex: 1 1 auto;
  min-height: 0;
}

.code-editor-error {
  flex: 0 0 auto;
  margin: 0;
  padding: 6px 10px;
  border-top: 1px solid var(--border);
  color: var(--danger);
  font-size: var(--text-xs);
}
</style>
