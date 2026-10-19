<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import EditorGotoLine from "@/features/files/components/EditorGotoLine.vue";
import EditorSearchBar from "@/features/files/components/EditorSearchBar.vue";
import { useEditorFind } from "@/features/files/composables/useEditorFind";
import { createEditorSession } from "@/features/files/composables/useCodeMirror";
import type { EditorSession } from "@/features/files/composables/useCodeMirror";
import { CODE_EDITOR_PRESETS } from "@/features/files/editor/presets";
import type { CodeEditorPreset } from "@/features/files/editor/presets";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import type { EditorLanguageId } from "@/features/files/utils/editor";

/**
 * Embedded code editor (JUS-230). A thin owner of one CodeMirror session for
 * config dialogs: it shares the file editor's settings store, shortcut table
 * and lazy chunk, but never touches the server — Ctrl/Cmd+S emits `save` and
 * the parent decides what that means.
 */
interface Props {
  /** modelValue is the document text (v-model). */
  modelValue: string;
  /** language wins over preset when both are given. */
  language?: EditorLanguageId;
  /** preset selects a grammar for a known config format. */
  preset?: CodeEditorPreset;
  readOnly?: boolean;
  /** wrap overrides the shared word-wrap preference for this instance only. */
  wrap?: boolean;
  /** height is any CSS length; the editor fills the box. */
  height?: string;
  /** placeholder is shown while the document is empty. */
  placeholder?: string;
  /** ariaLabel overrides the localized generic editor name. */
  ariaLabel?: string;
}

const props = withDefaults(defineProps<Props>(), {
  language: undefined,
  preset: undefined,
  readOnly: false,
  wrap: undefined,
  height: "320px",
  placeholder: undefined,
  ariaLabel: undefined,
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

// The embedded editor gets the same custom find/replace and go-to-line
// surfaces as the file editor (JUS-231); its read-only flag also gates replace.
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

/** ariaLabel is the caller label, else the localized generic editor name. */
const ariaLabel = computed<string>(
  () => props.ariaLabel ?? t("files.editor.codeEditorLabel"),
);

/** language resolves the explicit prop, else the preset, else plain text. */
const language = computed<EditorLanguageId>(
  () => props.language ?? (props.preset ? CODE_EDITOR_PRESETS[props.preset] : "plain"),
);

/** effectiveSettings applies the per-instance wrap override. */
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
    ariaLabel: ariaLabel.value,
    placeholder: props.placeholder,
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
    // The chunk is lazy: a dialog can close before it resolves, and that
    // instance must not outlive the component.
    if (disposed) {
      created.destroy();
      return;
    }
    session = created;
    // Prop/store changes that landed during the chunk load happened before the
    // session existed, so replay the current values onto it.
    created.setDoc(props.modelValue);
    created.setReadOnly(props.readOnly);
    created.setSettings(effectiveSettings.value);
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
</style>
