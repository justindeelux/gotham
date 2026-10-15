<script setup lang="ts">
import { Archive, ChevronRight, Copy, Download, Scissors, Trash2, X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { isSelectable } from "@/features/files/utils/files";
import { formatBytes } from "@/shared/utils/format";

/**
 * FilesSelectionBar is the selection count + bulk actions cluster. It is
 * rendered in the sticky header row while a selection exists (list view, where
 * it replaces the column headers, JUS-146) and in the toolbar in grid view
 * (the grid has no column-header row to replace). Both placements share this
 * one component so the count and every action stay in step.
 *
 * The count keeps a hidden reserve sizer beside the live summary so its cell
 * width is content-derived and never depends on the selection (JUS-141); the
 * actions self-hide with `visibility` while nothing is selected, so the row
 * height is stable and selecting never shifts the list (JUS-130/134).
 */
defineProps<{ countReserve: string }>();
const emit = defineEmits<{
  download: [];
  compress: [selection: FilesEntry[]];
  remove: [selection: FilesEntry[]];
  clear: [];
}>();

const files = useFilesStore();
const { t } = useI18n();
/** hasSelection gates the whole cluster. */
const hasSelection = computed<boolean>(() => files.selectedNames.length > 0);
/** hasSelectedFiles gates the bulk download action. */
const hasSelectedFiles = computed<boolean>(() =>
  files.selectedEntries.some((entry) => entry.kind === "file"),
);
/** selection is the current selection reduced to writable-safe entries. */
const selection = computed<FilesEntry[]>(() =>
  files.selectedEntries.filter(isSelectable),
);

/**
 * JUS-148: the chevron is a real affordance only while the action row actually
 * overflows to the right, so it is hidden when the row fits and once the row is
 * scrolled to its end. The metrics are read from the row itself and re-evaluated
 * on mount, on resize, on scroll and whenever the selection appears or clears.
 */
const actionsEl = ref<HTMLElement | null>(null);
const showScrollHint = ref(false);

/** updateScrollHint derives the chevron's visibility from the row's scroll state. */
function updateScrollHint(): void {
  const el = actionsEl.value;
  showScrollHint.value =
    el !== null && el.scrollWidth - el.clientWidth - el.scrollLeft > 1;
}

let scrollHintObserver: ResizeObserver | null = null;
onMounted(async () => {
  await nextTick();
  updateScrollHint();
  window.addEventListener("resize", updateScrollHint);
  if (typeof ResizeObserver !== "undefined" && actionsEl.value !== null) {
    scrollHintObserver = new ResizeObserver(updateScrollHint);
    scrollHintObserver.observe(actionsEl.value);
  }
});
onBeforeUnmount(() => {
  window.removeEventListener("resize", updateScrollHint);
  scrollHintObserver?.disconnect();
  scrollHintObserver = null;
});
watch(hasSelection, async () => {
  await nextTick();
  updateScrollHint();
});
</script>

<template>
  <span class="small muted fm-bulk-count">
    <span class="fm-bulk-count-reserve" aria-hidden="true">{{ countReserve }}</span>
    <span
      class="fm-bulk-count-live"
      :class="{ 'is-hidden': !hasSelection }"
    >
      <template v-if="hasSelection">
        {{ t("files.browser.selectionCount", { count: files.selectedNames.length }) }}
        · {{ formatBytes(files.selectedBytes) }}
      </template>
    </span>
  </span>
  <div
    ref="actionsEl"
    class="fm-selection-actions"
    :class="{ 'is-visible': hasSelection }"
    @scroll="updateScrollHint"
  >
    <NButton
      size="small"
      secondary
      :disabled="!files.canReadContent || !hasSelectedFiles"
      :title="!files.canReadContent ? t('files.browser.fileNeedsRole') : (hasSelectedFiles ? t('files.browser.downloadSelected') : t('files.dialogs.compressSelect'))"
      :aria-label="t('files.browser.downloadSelected')"
      @click="emit('download')"
    >
      <template #icon>
        <Download :size="14" :stroke-width="1.8" aria-hidden="true" />
      </template>
      <span class="fm-btn-label">{{ t("files.browser.downloadSelected") }}</span>
    </NButton>
    <template v-if="files.canWrite">
      <NButton
        size="small"
        secondary
        :disabled="files.acting || selection.length === 0"
        :title="selection.length === 0 ? t('files.dialogs.compressSelect') : t('files.actions.copy')"
        :aria-label="t('files.actions.copy')"
        @click="files.copySelection(selection.map((item) => item.name))"
      >
        <template #icon>
          <Copy :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ t("files.actions.copy") }}</span>
      </NButton>
      <NButton
        size="small"
        secondary
        :disabled="files.acting || selection.length === 0"
        :title="selection.length === 0 ? t('files.dialogs.compressSelect') : t('files.actions.cut')"
        :aria-label="t('files.actions.cut')"
        @click="files.cutSelection(selection.map((item) => item.name))"
      >
        <template #icon>
          <Scissors :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ t("files.actions.cut") }}</span>
      </NButton>
      <NButton
        size="small"
        secondary
        :disabled="files.acting || selection.length === 0"
        :title="selection.length === 0 ? t('files.dialogs.compressSelect') : t('files.actions.compress')"
        :aria-label="t('files.actions.compress')"
        @click="emit('compress', selection)"
      >
        <template #icon>
          <Archive :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ t("files.actions.compress") }}</span>
      </NButton>
      <NButton
        size="small"
        type="error"
        secondary
        :disabled="files.acting || selection.length === 0"
        :title="selection.length === 0 ? t('files.dialogs.compressSelect') : t('common.actions.delete')"
        :aria-label="t('common.actions.delete')"
        @click="emit('remove', selection)"
      >
        <template #icon>
          <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ t("common.actions.delete") }}</span>
      </NButton>
    </template>
    <NButton
      :disabled="!hasSelection"
      size="small"
      quaternary
      :title="t('files.browser.clear')"
      :aria-label="t('files.browser.clear')"
      @click="emit('clear')"
    >
      <template #icon>
        <X :size="14" :stroke-width="1.8" aria-hidden="true" />
      </template>
      <span class="fm-btn-label">{{ t("files.browser.clear") }}</span>
    </NButton>
    <!-- JUS-148: a pinned chevron + fade marks the action row as scrollable, so
         the actions off the right edge (Delete/Clear on a phone) are obvious
         rather than a weak fade. It is decorative (aria-hidden) and is rendered
         only while the row really overflows to the right. -->
    <span v-if="showScrollHint" class="fm-scroll-hint" aria-hidden="true">
      <ChevronRight :size="14" :stroke-width="2" />
    </span>
  </div>
</template>

<style scoped>
/* Selection count (JUS-128/141/146): the cell stacks a hidden reserve sizer and
   the live summary in one grid area, so its width is max(reserve, live) in
   every selection state and never depends on the selection (JUS-141). */
.fm-bulk-count {
  display: inline-grid;
  align-items: center;
  justify-items: start;
  min-height: 22px;
  flex: 0 0 auto;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.fm-bulk-count > * {
  grid-area: 1 / 1;
}

.fm-bulk-count-reserve {
  visibility: hidden;
}

.fm-bulk-count-live.is-hidden {
  visibility: hidden;
}

/* JUS-134/146/148: the bulk actions stay mounted and are hidden with visibility
   (not display) while nothing is selected, so the row height is fixed and the
   first click of a double-click can never shift the list (JUS-130). They never
   wrap — the row scrolls instead, with the pinned chevron (below) marking it as
   scrollable. */
.fm-selection-actions {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  flex-wrap: nowrap;
  gap: var(--space-2);
  visibility: hidden;
  flex: 1 1 auto;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: thin;
}

.fm-selection-actions.is-visible {
  visibility: visible;
}

/* JUS-148: the pinned scroll affordance — a fade with a chevron that rides the
   scrollport's right edge (pointer-events: none, so it never eats a tap or a
   hit test). It replaces the old bare gradient, which read as a weak hint. */
.fm-scroll-hint {
  position: sticky;
  right: 0;
  flex: 0 0 auto;
  align-self: stretch;
  display: grid;
  place-items: center;
  width: 28px;
  margin-left: -28px;
  pointer-events: none;
  color: var(--fg-2);
  background: linear-gradient(to right, transparent, var(--surface) 55%);
}

@media (max-width: 640px) {
  /* The bulk actions are icon-only on a phone, like the rest of the toolbar
     (each button keeps its accessible name through aria-label + title). */
  .fm-btn-label {
    display: none;
  }

  /* Every selection action is a 44px touch target on a phone. */
  .fm-selection-actions :deep(.n-button) {
    min-width: 44px;
    height: 44px;
  }
}
</style>
