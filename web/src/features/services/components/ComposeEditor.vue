<script setup lang="ts">
import { NButton, NInput, NSpace } from "naive-ui";
import { computed, ref, watch } from "vue";

import { activeLocale, i18n } from "@/shared/i18n";

/**
 * Monospace compose document viewer/editor.
 *
 * The component owns the edit draft; the parent owns persistence. `save`
 * carries the draft text and the editor stays in edit mode until the parent
 * flips `editing` back to false (so a failed save never loses the draft).
 */

interface Props {
  modelValue: string;
  /** Two-way with v-model:editing. */
  editing?: boolean;
  saving?: boolean;
  readonly?: boolean;
  label?: string;
  minRows?: number;
  /** Placeholder shown when the stored document is empty. */
  emptyText?: string;
}

/**
 * t renders editor copy in the active locale (tracks language switches).
 * Called during render, so labels refresh without losing the edit draft.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const props = withDefaults(defineProps<Props>(), {
  editing: false,
  saving: false,
  readonly: false,
  label: "",
  minRows: 16,
  emptyText: "",
});

const emit = defineEmits<{
  "update:modelValue": [value: string];
  "update:editing": [value: boolean];
  save: [value: string];
}>();

const draft = ref<string>(props.modelValue);

watch(
  () => props.editing,
  (editing) => {
    if (editing) {
      draft.value = props.modelValue;
    }
  },
);

watch(
  () => props.modelValue,
  (value) => {
    if (!props.editing) {
      draft.value = value;
    }
  },
);

const isDirty = computed<boolean>(() => draft.value !== props.modelValue);

/** labelText is the caller override or the localized compose wording. */
const labelText = computed<string>(() => props.label || t("services.compose.defaultLabel"));

/** emptyLabel is the caller override or the localized empty wording. */
const emptyLabel = computed<string>(() => props.emptyText || t("services.compose.emptyText"));

/** startEdit opens the editor seeded with the stored document. */
function startEdit(): void {
  draft.value = props.modelValue;
  emit("update:editing", true);
}

/** cancel discards the draft and returns to the read view. */
function cancel(): void {
  draft.value = props.modelValue;
  emit("update:editing", false);
}

/** save hands the draft to the parent; the parent persists and closes. */
function save(): void {
  emit("save", draft.value);
}
</script>

<template>
  <section class="compose-editor">
    <header class="compose-editor__head">
      <span class="compose-editor__label mono">{{ labelText }}</span>
      <NSpace :size="8" align="center">
        <template v-if="editing">
          <NButton size="small" :disabled="saving" @click="cancel">{{ t("common.actions.cancel") }}</NButton>
          <NButton
            size="small"
            type="primary"
            :loading="saving"
            :disabled="!isDirty"
            @click="save"
          >
            {{ t("common.actions.save") }}
          </NButton>
        </template>
        <NButton v-else size="small" :disabled="readonly" @click="startEdit">
          {{ t("common.actions.edit") }}
        </NButton>
      </NSpace>
    </header>

    <NInput
      v-if="editing"
      v-model:value="draft"
      type="textarea"
      class="compose-editor__input mono"
      spellcheck="false"
      :autosize="{ minRows, maxRows: 40 }"
    />
    <pre v-else class="compose-editor__view mono">{{
      modelValue || emptyLabel
    }}</pre>

    <p class="compose-editor__hint">
      {{ t("services.compose.hint") }}
    </p>
  </section>
</template>

<style scoped>
.compose-editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.compose-editor__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.compose-editor__label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.compose-editor__view {
  margin: 0;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  font-size: var(--text-xs);
  line-height: 1.6;
  color: var(--fg);
  overflow: auto;
  max-height: 56vh;
  white-space: pre-wrap;
  word-break: break-word;
}

.compose-editor__input :deep(textarea) {
  background: var(--surface-warm);
  font-size: var(--text-xs);
  line-height: 1.6;
}

.compose-editor__hint {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
