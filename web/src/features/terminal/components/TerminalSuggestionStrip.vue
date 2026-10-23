<script setup lang="ts">
import { X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import type { QuickCommandSuggestion } from "@/features/terminal/utils/quickCommandEngine";

interface Props {
  suggestion: QuickCommandSuggestion | null;
  notice?: string;
  noPromptMarksHint?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  notice: "",
  noPromptMarksHint: false,
});
const emit = defineEmits<{
  accept: [];
  dismiss: [];
  returnFocus: [];
  dismissNotice: [];
}>();
const { t } = useI18n();
const option = ref<HTMLButtonElement | null>(null);

const announcement = computed<string>(() => {
  if (!props.suggestion) {
    return "";
  }
  return t("terminal.suggestions.announce", {
    abbreviation: props.suggestion.abbreviation,
    label: props.suggestion.label,
  });
});

function focusOption(): void {
  option.value?.focus();
}

defineExpose({ focusOption });

function onOptionKeydown(event: KeyboardEvent): void {
  switch (event.key) {
    case "Escape":
      event.preventDefault();
      event.stopPropagation();
      emit("dismiss");
      return;
    case "Enter":
      event.preventDefault();
      event.stopPropagation();
      emit("accept");
      return;
    case "Tab":
      event.preventDefault();
      event.stopPropagation();
      emit("returnFocus");
      return;
    default:
      return;
  }
}
</script>

<template>
  <div class="term-suggest">
    <p class="term-sr" role="status" aria-live="polite">{{ announcement }}</p>
    <template v-if="props.suggestion">
      <div
        class="term-suggest-list"
        role="listbox"
        :aria-label="t('terminal.suggestions.label')"
      >
        <button
          ref="option"
          type="button"
          role="option"
          aria-selected="true"
          class="term-suggest-option"
          @mousedown.prevent
          @click.stop="emit('accept')"
          @keydown="onOptionKeydown"
        >
          <span class="term-suggest-abbr mono">{{ props.suggestion.abbreviation }}</span>
          <span class="term-suggest-arrow" aria-hidden="true">→</span>
          <span class="term-suggest-label">{{ props.suggestion.label }}</span>
          <code class="term-suggest-command mono">{{ props.suggestion.command }}</code>
        </button>
      </div>
      <NButton
        size="tiny"
        secondary
        class="term-suggest-choose"
        :aria-label="t('terminal.suggestions.choose')"
        :title="t('terminal.suggestions.hint')"
        @click.stop="focusOption"
      >
        {{ t("terminal.suggestions.choose") }}
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :aria-label="t('terminal.suggestions.dismiss')"
        @click.stop="emit('dismiss')"
      >
        <template #icon>
          <X :size="12" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </template>
    <template v-else-if="props.notice">
      <span class="term-suggest-placeholder" role="status">{{ props.notice }}</span>
      <NButton
        quaternary
        size="tiny"
        :aria-label="t('terminal.quickCommands.dismissNotice')"
        @click.stop="emit('dismissNotice')"
      >
        <template #icon>
          <X :size="12" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </template>
    <span v-else class="term-suggest-placeholder" role="status">
      {{ t("terminal.suggestions.noPromptMarks") }}
    </span>
  </div>
</template>

<style scoped>
.term-suggest {
  position: absolute;
  left: 0;
  right: 0;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  height: var(--space-12);
  min-height: var(--space-12);
  max-height: var(--space-12);
  overflow: hidden;
  padding: 0 var(--space-3);
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  background: var(--surface);
  box-shadow: var(--elev-raised);
  pointer-events: none;
}

.term-suggest > * {
  pointer-events: auto;
}

.term-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}

.term-suggest-list {
  display: flex;
  flex: 1 1 auto;
  min-width: 0;
}

.term-suggest-option {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  width: 100%;
  min-width: 0;
  padding: var(--space-1) var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
  color: var(--fg);
  text-align: left;
  cursor: pointer;
}

.term-suggest-option:hover,
.term-suggest-option:focus-visible {
  border-color: var(--accent);
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.term-suggest-abbr {
  flex: 0 0 auto;
  color: var(--accent);
  font-size: var(--text-sm);
}

.term-suggest-arrow {
  flex: 0 0 auto;
  color: var(--meta);
}

.term-suggest-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg-2);
  font-size: var(--text-sm);
}

.term-suggest-command {
  margin-left: auto;
  max-width: 42%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-suggest-placeholder {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--meta);
  font-size: var(--text-xs);
}

.term-suggest-choose {
  flex: 0 0 auto;
}

@media (max-width: 480px) {
  .term-suggest-command {
    display: none;
  }
}
</style>
