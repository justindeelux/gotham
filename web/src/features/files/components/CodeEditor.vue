<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

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
  /** ariaLabel overrides the localized generic editor name. */
  ariaLabel?: string;
}

const props = withDefaults(defineProps<Props>(), {
  language: undefined,
  preset: undefined,
  readOnly: false,
  wrap: undefined,
  height: "320px",
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
    readOnly: props.readOnly,
    settings: effectiveSettings.value,
    onChange: (doc) => emit("update:modelValue", doc),
    onSave: () => emit("save", session?.getDoc() ?? props.modelValue),
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
  <div ref="host" class="code-editor" :style="{ height }" />
</template>

<style scoped>
.code-editor {
  min-height: 120px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-warm);
}
</style>
