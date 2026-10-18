<script setup lang="ts">
import {
  ArrowDown,
  ArrowUp,
  CaseSensitive,
  ChevronDown,
  ChevronRight,
  Regex,
  Replace,
  ReplaceAll,
  WholeWord,
  X,
} from "@lucide/vue";
import { NButton, NInput } from "naive-ui";
import type { InputInst } from "naive-ui";
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

/**
 * Custom find/replace bar (JUS-231). It is presentation only: the parent owns
 * the query and the CodeMirror search calls, so the same bar serves the file
 * editor, the history diff and the embedded code editor.
 */
interface Props {
  show: boolean;
  /** expanded shows the replacement row. */
  expanded: boolean;
  /** canReplace is false on read-only tabs. */
  canReplace: boolean;
  query: string;
  replacement: string;
  caseSensitive: boolean;
  wholeWord: boolean;
  regexp: boolean;
  invalidRegex: boolean;
  total: number;
  current: number;
  /** focusReplace puts the caret in the replacement field on open. */
  focusReplace: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  close: [];
  "update:query": [value: string];
  "update:replacement": [value: string];
  "toggle:expanded": [];
  "toggle:case": [];
  "toggle:word": [];
  "toggle:regex": [];
  next: [];
  previous: [];
  replace: [];
  "replace-all": [];
}>();

const { t } = useI18n();
const queryInput = ref<InputInst | null>(null);
const replaceInput = ref<InputInst | null>(null);
// A stable id links the regex error to the query field. One bar per editor
// surface, but the files page and a config dialog can each render one.
const errorId = `ed-find-regex-${Math.random().toString(36).slice(2, 9)}`;

/** counterText is the aria-live match count, empty without a query. */
const counterText = computed<string>(() => {
  if (!props.query || props.invalidRegex) {
    return "";
  }
  if (props.total === 0) {
    return String(t("files.editor.searchBar.noResults"));
  }
  return String(
    t("files.editor.searchBar.results", {
      current: props.current,
      total: props.total,
    }),
  );
});

/** queryProps names the field and links the inline regex error. */
const queryProps = computed<Record<string, string | undefined>>(() => ({
  "aria-label": String(t("files.editor.searchBar.find")),
  "aria-invalid": props.invalidRegex ? "true" : undefined,
  "aria-describedby": props.invalidRegex ? errorId : undefined,
}));

// Opening moves focus into the bar; Mod-H lands on the replacement field.
watch(
  () => [props.show, props.focusReplace] as const,
  ([show]) => {
    if (!show) {
      return;
    }
    void nextTick(() => {
      if (props.focusReplace && props.canReplace) {
        replaceInput.value?.focus();
        return;
      }
      queryInput.value?.focus();
    });
  },
  { immediate: true },
);

/** onQueryEnter walks the matches, Shift reverses (IME-safe). */
function onQueryEnter(event: KeyboardEvent): void {
  if (event.isComposing) {
    return;
  }
  event.preventDefault();
  if (event.shiftKey) {
    emit("previous");
    return;
  }
  emit("next");
}

/** onReplaceEnter replaces one match, Mod+Enter replaces every match. */
function onReplaceEnter(event: KeyboardEvent): void {
  if (event.isComposing) {
    return;
  }
  event.preventDefault();
  if (event.metaKey || event.ctrlKey) {
    emit("replace-all");
    return;
  }
  emit("replace");
}
</script>

<template>
  <div
    v-if="props.show"
    class="ed-find"
    role="search"
    :aria-label="t('files.editor.searchBar.label')"
    @keydown.esc.stop.prevent="emit('close')"
  >
    <div class="ed-find-row">
      <NButton
        quaternary
        size="tiny"
        class="ed-find-toggle"
        :aria-expanded="props.expanded"
        :aria-label="t('files.editor.searchBar.toggleReplace')"
        :title="t('files.editor.searchBar.toggleReplace')"
        @click="emit('toggle:expanded')"
      >
        <template #icon>
          <ChevronDown v-if="props.expanded" :size="14" aria-hidden="true" />
          <ChevronRight v-else :size="14" aria-hidden="true" />
        </template>
      </NButton>
      <NInput
        ref="queryInput"
        class="ed-find-input"
        size="tiny"
        :value="props.query"
        :placeholder="t('files.editor.searchBar.findPlaceholder')"
        :input-props="queryProps"
        @update:value="(value: string) => emit('update:query', value)"
        @keydown.enter="onQueryEnter"
      />
      <NButton
        quaternary
        size="tiny"
        :type="props.caseSensitive ? 'primary' : 'default'"
        :aria-pressed="props.caseSensitive"
        :aria-label="t('files.editor.searchBar.matchCase')"
        :title="t('files.editor.searchBar.matchCase')"
        @click="emit('toggle:case')"
      >
        <template #icon>
          <CaseSensitive :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :type="props.wholeWord ? 'primary' : 'default'"
        :aria-pressed="props.wholeWord"
        :aria-label="t('files.editor.searchBar.wholeWord')"
        :title="t('files.editor.searchBar.wholeWord')"
        @click="emit('toggle:word')"
      >
        <template #icon>
          <WholeWord :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :type="props.regexp ? 'primary' : 'default'"
        :aria-pressed="props.regexp"
        :aria-label="t('files.editor.searchBar.regex')"
        :title="t('files.editor.searchBar.regex')"
        @click="emit('toggle:regex')"
      >
        <template #icon>
          <Regex :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <span class="ed-find-count" aria-live="polite" aria-atomic="true">
        {{ counterText }}
      </span>
      <NButton
        quaternary
        size="tiny"
        :disabled="props.total === 0"
        :aria-label="t('files.editor.searchBar.previous')"
        :title="t('files.editor.searchBar.previous')"
        @click="emit('previous')"
      >
        <template #icon>
          <ArrowUp :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :disabled="props.total === 0"
        :aria-label="t('files.editor.searchBar.next')"
        :title="t('files.editor.searchBar.next')"
        @click="emit('next')"
      >
        <template #icon>
          <ArrowDown :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :aria-label="t('files.editor.searchBar.close')"
        :title="t('files.editor.searchBar.close')"
        @click="emit('close')"
      >
        <template #icon>
          <X :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </div>

    <p v-if="props.invalidRegex" :id="errorId" class="ed-find-error" role="alert">
      {{ t("files.editor.searchBar.invalidRegex") }}
    </p>

    <div v-if="props.expanded" class="ed-find-row">
      <span class="ed-find-offset" aria-hidden="true" />
      <NInput
        ref="replaceInput"
        class="ed-find-input"
        size="tiny"
        :value="props.replacement"
        :disabled="!props.canReplace"
        :placeholder="t('files.editor.searchBar.replacePlaceholder')"
        :input-props="{ 'aria-label': t('files.editor.searchBar.replaceField') }"
        @update:value="(value: string) => emit('update:replacement', value)"
        @keydown.enter="onReplaceEnter"
      />
      <NButton
        size="tiny"
        secondary
        :disabled="!props.canReplace || props.total === 0"
        @click="emit('replace')"
      >
        <template #icon>
          <Replace :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ t("files.editor.searchBar.replaceOne") }}
      </NButton>
      <NButton
        size="tiny"
        secondary
        :disabled="!props.canReplace || props.total === 0"
        @click="emit('replace-all')"
      >
        <template #icon>
          <ReplaceAll :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ t("files.editor.searchBar.replaceEvery") }}
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.ed-find {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
  margin-bottom: var(--space-2);
  padding: var(--space-1) var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
}

.ed-find-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
}

.ed-find-input {
  flex: 1 1 140px;
  min-width: 88px;
}

.ed-find-offset {
  flex: 0 0 24px;
}

.ed-find-count {
  min-width: 56px;
  color: var(--muted);
  font-size: var(--text-xs);
  font-variant-numeric: tabular-nums;
  text-align: center;
  white-space: nowrap;
}

.ed-find-error {
  margin: 0;
  color: var(--danger-ink);
  font-size: var(--text-xs);
}
</style>
