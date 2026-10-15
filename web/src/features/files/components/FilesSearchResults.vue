<script setup lang="ts">
import { Search, X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import FileTypeIcon from "@/features/files/components/FileTypeIcon.vue";
import type { FilesSearchHit } from "@/features/files/schemas/files";
import { entryIconKind, parentPath } from "@/features/files/utils/files";

interface Props {
  /** results are the name-search hits with their root-relative paths. */
  results: FilesSearchHit[];
  /** truncated reports a capped search (depth/result/scan cap hit). */
  truncated: boolean;
  loading: boolean;
  /** query is the submitted search text. */
  query: string;
  /** path is the directory the search ran under ("" = the root itself). */
  path: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  open: [hit: FilesSearchHit];
  clear: [];
}>();

/** folderOf is the containing folder of one hit, for the row's location line. */
function folderOf(hit: FilesSearchHit): string {
  return parentPath(hit.path) || "/";
}

const scope = computed<string>(() => props.path || "/");

/**
 * selectedPath is the row a single click (or focus) left active. Like the normal
 * list, a single click only selects — it never opens — so a real double-click
 * cannot fire its second click on the listing the first click navigated to
 * (JUS-163 round 3). Opening is the double-click or Enter/Space.
 */
const selectedPath = ref<string | null>(null);

/** selectHit marks one row as the current one without opening it. */
function selectHit(hit: FilesSearchHit): void {
  selectedPath.value = hit.path;
}

/** openHit opens one hit's location (double-click / Enter / Space). */
function openHit(hit: FilesSearchHit): void {
  emit("open", hit);
}

const { t } = useI18n();

/**
 * liveMessage is the single polite announcement for the whole search: searching,
 * the result count (with the truncated note appended), or the empty state. It
 * stays mounted across loading → done, so a screen reader hears the outcome
 * after Enter even though focus never leaves the input.
 */
const liveMessage = computed<string>(() => {
  if (props.loading) {
    return String(t("files.search.loading"));
  }
  if (props.results.length === 0) {
    return String(t("files.search.empty"));
  }
  const count = String(
    t("files.search.count", { count: props.results.length }),
  );
  return props.truncated
    ? `${count}. ${String(t("files.search.truncated"))}`
    : count;
});
</script>

<template>
  <div
    class="fm-search-results"
    role="region"
    :aria-label="$t('files.search.heading')"
  >
    <div class="fm-search-head">
      <div class="fm-search-head-text">
        <p class="fm-search-title">
          {{ $t("files.search.resultsFor", { query }) }}
        </p>
        <p class="fm-search-scope">
          {{ $t("files.search.scope", { path: scope }) }}
        </p>
      </div>
      <NButton size="small" secondary class="fm-search-clear" @click="emit('clear')">
        <template #icon>
          <X :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ $t("files.search.clear") }}
      </NButton>
    </div>

    <p
      class="fm-search-live"
      role="status"
      aria-live="polite"
    >
      {{ liveMessage }}
    </p>

    <p v-if="truncated" class="fm-search-truncated">
      {{ $t("files.search.truncated") }}
    </p>

    <div v-if="loading" class="fm-search-state">
      <h4>{{ $t("files.search.loading") }}</h4>
    </div>

    <div v-else-if="results.length === 0" class="fm-search-state">
      <Search :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4>{{ $t("files.search.empty") }}</h4>
      <p>{{ $t("files.search.emptyHint", { path: scope, query }) }}</p>
    </div>

    <ul v-else class="fm-search-list" role="list">
      <li v-for="hit in results" :key="hit.path" role="listitem">
        <button
          type="button"
          class="fm-search-row"
          :class="{ 'is-selected': selectedPath === hit.path }"
          :aria-current="selectedPath === hit.path ? 'true' : undefined"
          @click="selectHit(hit)"
          @dblclick="openHit(hit)"
          @keydown.enter.prevent="openHit(hit)"
          @keydown.space.prevent="openHit(hit)"
        >
          <FileTypeIcon :kind="entryIconKind(hit.entry)" />
          <span class="fm-search-name">{{ hit.entry.name }}</span>
          <span class="fm-search-folder">
            {{ $t("files.search.inFolder", { path: folderOf(hit) }) }}
          </span>
        </button>
      </li>
    </ul>
  </div>
</template>

<style scoped>
/* The results take over the panel's content region while a search is active
   (JUS-163). Tokens only — no hard-coded colors or pixel sizes. */
.fm-search-results {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.fm-search-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
}

.fm-search-head-text {
  flex: 1 1 auto;
  min-width: 0;
}

.fm-search-title {
  margin: 0;
  font-weight: 600;
}

.fm-search-scope {
  margin: var(--space-1) 0 0;
  color: var(--muted);
  font-size: var(--text-xs);
}

.fm-search-clear {
  flex: 0 0 auto;
}

/* The single polite live region: clipped out of view but in the a11y tree, so
   the count / empty / searching outcome is announced without a visible line. */
.fm-search-live {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.fm-search-truncated {
  margin: 0;
  padding: var(--space-1) var(--space-3);
  color: var(--warn-ink);
  font-size: var(--text-xs);
}

.fm-search-state {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: var(--space-2);
  padding: var(--space-8) var(--space-4);
  color: var(--muted);
  text-align: center;
}

.fm-search-state h4 {
  margin: 0;
}

.fm-search-state p {
  margin: 0;
  font-size: var(--text-xs);
}

.fm-search-list {
  list-style: none;
  margin: 0;
  padding: var(--space-1) 0;
  overflow-y: auto;
  flex: 1 1 auto;
  min-height: 0;
}

.fm-search-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-1) var(--space-3);
  border: 0;
  background: none;
  color: var(--fg-2);
  text-align: left;
  cursor: pointer;
}

.fm-search-row:hover {
  background: var(--hover-row);
}

.fm-search-row.is-selected {
  background: var(--selected-row);
  color: var(--fg-2);
}

/* The shared ring is outward and would be clipped by the list's edge; keep it
   inside the row, like the tab strip does. */
.fm-search-row:focus-visible {
  outline-offset: -2px;
}

.fm-search-name {
  flex: 0 0 auto;
  font-weight: 500;
}

.fm-search-folder {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}
</style>
