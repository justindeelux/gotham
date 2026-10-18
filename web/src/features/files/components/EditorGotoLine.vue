<script setup lang="ts">
import { X } from "@lucide/vue";
import { NButton, NInput } from "naive-ui";
import type { InputInst } from "naive-ui";
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { parseGotoLine } from "@/features/files/editor/goto";
import type { GotoLineError } from "@/features/files/editor/goto";

/**
 * Custom go-to-line popover (JUS-231). It accepts `line` or `line:column`,
 * shows the cursor line and the document's line count, and rejects zero,
 * out-of-range and non-numeric input inline instead of clamping it.
 */
interface Props {
  show: boolean;
  /** current is the cursor line when the popover opens. */
  current: number;
  /** total is the document line count. */
  total: number;
  /** jump performs the move; a rejected dimension is shown inline. */
  jump: (_line: number, _column: number) => GotoLineError | null;
}

const props = defineProps<Props>();

const emit = defineEmits<{ close: [] }>();

const { t } = useI18n();
const input = ref<InputInst | null>(null);
const value = ref("");
const error = ref<GotoLineError | null>(null);
const errorId = `ed-goto-error-${Math.random().toString(36).slice(2, 9)}`;

/** errorText explains the rejected dimension in place. */
const errorText = computed<string>(() => {
  if (error.value === "format") {
    return String(t("files.editor.goTo.invalidFormat"));
  }
  if (error.value === "line") {
    return String(t("files.editor.goTo.invalidLine", { total: props.total }));
  }
  if (error.value === "column") {
    return String(t("files.editor.goTo.invalidColumn"));
  }
  return "";
});

// Opening focuses the field with its text selected, so a retype replaces it.
watch(
  () => props.show,
  (show) => {
    if (!show) {
      return;
    }
    value.value = "";
    error.value = null;
    void nextTick(() => {
      input.value?.focus();
      input.value?.select();
    });
  },
  { immediate: true },
);

/** submit validates the syntax and lets the editor check the column bound. */
function submit(): void {
  const parsed = parseGotoLine(value.value, props.total);
  if ("error" in parsed) {
    error.value = parsed.error;
    return;
  }
  const failure = props.jump(parsed.target.line, parsed.target.column);
  if (failure) {
    error.value = failure;
    return;
  }
  emit("close");
}

/** onSubmit ignores Enter while an IME composition is active. */
function onSubmit(event: KeyboardEvent): void {
  if (event.isComposing) {
    return;
  }
  event.preventDefault();
  submit();
}
</script>

<template>
  <div
    v-if="props.show"
    class="ed-goto"
    role="dialog"
    :aria-label="t('files.editor.goTo.label')"
    @keydown.esc.stop.prevent="emit('close')"
  >
    <div class="ed-goto-row">
      <NInput
        ref="input"
        class="ed-goto-input"
        size="tiny"
        :value="value"
        :placeholder="t('files.editor.goTo.placeholder')"
        :input-props="{
          'aria-label': t('files.editor.goTo.label'),
          'aria-invalid': error ? 'true' : undefined,
          'aria-describedby': error ? errorId : undefined,
        }"
        @update:value="(next: string) => (value = next)"
        @keydown.enter="onSubmit"
      />
      <NButton size="tiny" type="primary" @click="submit">
        {{ t("files.editor.goTo.confirm") }}
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :aria-label="t('files.editor.goTo.close')"
        :title="t('files.editor.goTo.close')"
        @click="emit('close')"
      >
        <template #icon>
          <X :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </div>
    <span class="ed-goto-hint">
      {{ t("files.editor.goTo.current", { line: props.current, total: props.total }) }}
    </span>
    <p v-if="error" :id="errorId" class="ed-goto-error" role="alert">{{ errorText }}</p>
  </div>
</template>

<style scoped>
.ed-goto {
  position: absolute;
  top: var(--space-1);
  right: var(--space-1);
  z-index: 5;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  width: min(320px, calc(100% - var(--space-2)));
  padding: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  box-shadow: var(--elev-raised);
}

.ed-goto-row {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
}

.ed-goto-input {
  flex: 1 1 auto;
  min-width: 0;
}

.ed-goto-hint {
  color: var(--muted);
  font-size: var(--text-xs);
}

.ed-goto-error {
  margin: 0;
  color: var(--danger-ink);
  font-size: var(--text-xs);
}
</style>
