<script setup lang="ts">
import { X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { ref } from "vue";
import { useI18n } from "vue-i18n";

import type { QuickCommandSuggestion } from "@/features/terminal/utils/quickCommandEngine";

/**
 * Suggestion strip (JUS-249). It is a normal flex sibling ABOVE the xterm
 * viewport, so it can never cover the prompt line being typed. The list is a
 * valid listbox owning one option directly (no ul/li wrappers). Keys reach the
 * PTY unless the user explicitly focuses the list with Ctrl+Shift+Space; then
 * Enter accepts (without Enter reaching the shell), Escape dismisses and Tab
 * returns focus.
 */
interface Props {
  suggestion: QuickCommandSuggestion;
}

const props = defineProps<Props>();
const emit = defineEmits<{ accept: []; dismiss: []; returnFocus: [] }>();
const { t } = useI18n();
const option = ref<HTMLButtonElement | null>(null);

/** focusOption is called by the surface's Ctrl+Shift+Space shortcut. */
function focusOption(): void {
  option.value?.focus();
}

defineExpose({ focusOption });

/** onOptionKeydown only runs while the option itself is focused. */
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
    <p class="term-sr" role="status" aria-live="polite">
      {{
        t("terminal.suggestions.announce", {
          abbreviation: props.suggestion.abbreviation,
          label: props.suggestion.label,
        })
      }}
    </p>
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
    <div class="term-suggest-foot">
      <span>{{ t("terminal.suggestions.hint") }}</span>
      <NButton
        size="tiny"
        secondary
        class="term-suggest-choose"
        :aria-label="t('terminal.suggestions.choose')"
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
    </div>
  </div>
</template>

<style scoped>
.term-suggest {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--surface);
  box-shadow: var(--elev-raised);
}

.term-suggest-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
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
  max-width: 50%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-suggest-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--meta);
  font-size: var(--text-xs);
}

.term-suggest-foot > span {
  flex: 1 1 auto;
}
</style>
