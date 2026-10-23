<script setup lang="ts">
import { NButton, NSpace } from "naive-ui";
import { computed, ref, watch } from "vue";

import CodeEditor from "@/features/files/components/CodeEditor.vue";
import { activeLocale, i18n } from "@/shared/i18n";

interface Props {
  modelValue: string;
  editing?: boolean;
  saving?: boolean;
  readonly?: boolean;
  label?: string;
  emptyText?: string;
}

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const props = withDefaults(defineProps<Props>(), {
  editing: false,
  saving: false,
  readonly: false,
  label: "",
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

const labelText = computed<string>(() => props.label || t("services.compose.defaultLabel"));

const emptyLabel = computed<string>(() => props.emptyText || t("services.compose.emptyText"));

function startEdit(): void {
  draft.value = props.modelValue;
  emit("update:editing", true);
}

function cancel(): void {
  draft.value = props.modelValue;
  emit("update:editing", false);
}

function save(): void {
  if (!isDirty.value) {
    return;
  }
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

    <CodeEditor
      v-if="editing"
      v-model="draft"
      class="compose-editor__input"
      preset="yaml"
      height="46vh"
      :read-only="readonly"
      :aria-label="labelText"
      @save="save"
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

.compose-editor__hint {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
