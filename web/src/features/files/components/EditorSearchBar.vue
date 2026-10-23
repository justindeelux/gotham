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

import type { SearchPatternError } from "@/features/files/editor/searchPattern";

interface Props {
  show: boolean;
  expanded: boolean;
  canReplace: boolean;
  query: string;
  replacement: string;
  caseSensitive: boolean;
  wholeWord: boolean;
  regexp: boolean;
  patternError: SearchPatternError | null;
  total: number;
  capped: boolean;
  current: number;
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
const errorId = `ed-find-regex-${Math.random().toString(36).slice(2, 9)}`;

const counterText = computed<string>(() => {
  if (!props.query || props.patternError) {
    return "";
  }
  if (props.total === 0) {
    return String(t("files.editor.searchBar.noResults"));
  }
  if (props.capped) {
    return String(t("files.editor.searchBar.resultsCapped", { total: props.total }));
  }
  return String(
    t("files.editor.searchBar.results", {
      current: props.current,
      total: props.total,
    }),
  );
});

const errorText = computed<string>(() => {
  if (props.patternError === "complexity") {
    return String(t("files.editor.searchBar.complexPattern"));
  }
  if (props.patternError === "syntax") {
    return String(t("files.editor.searchBar.invalidRegex"));
  }
  return "";
});

const queryProps = computed<Record<string, string | undefined>>(() => ({
  "aria-label": String(t("files.editor.searchBar.find")),
  "aria-invalid": props.patternError ? "true" : undefined,
  "aria-describedby": props.patternError ? errorId : undefined,
}));

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

    <p v-if="props.patternError" :id="errorId" class="ed-find-error" role="alert">
      {{ errorText }}
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
