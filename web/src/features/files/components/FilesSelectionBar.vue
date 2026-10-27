<script setup lang="ts">
import { Archive, ChevronRight, Copy, Download, Scissors, Trash2, X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesPane } from "@/features/files/stores/filesPane";
import { isSelectable } from "@/features/files/utils/files";
import { formatBytes } from "@/shared/utils/format";

defineProps<{ countReserve: string }>();
const emit = defineEmits<{
  download: [];
  compress: [selection: FilesEntry[]];
  remove: [selection: FilesEntry[]];
  clear: [];
}>();

const files = useFilesPane().files;
const { t } = useI18n();
const hasSelection = computed<boolean>(() => files.selectedNames.length > 0);
const selection = computed<FilesEntry[]>(() =>
  files.selectedEntries.filter(isSelectable),
);

const canBulkDownload = computed<boolean>(
  () =>
    files.canReadContent &&
    files.selectedEntries.some((entry) => entry.kind === "file"),
);
const canBulkAct = computed<boolean>(
  () => files.canWrite && !files.acting && selection.value.length > 0,
);

function onBulkDownload(): void {
  if (canBulkDownload.value) {
    emit("download");
  }
}
function onBulkCopy(): void {
  if (canBulkAct.value) {
    files.copySelection(selection.value.map((item) => item.name));
  }
}
function onBulkCut(): void {
  if (canBulkAct.value) {
    files.cutSelection(selection.value.map((item) => item.name));
  }
}
function onBulkCompress(): void {
  if (canBulkAct.value) {
    emit("compress", selection.value);
  }
}
function onBulkDelete(): void {
  if (canBulkAct.value) {
    emit("remove", selection.value);
  }
}

const actionsEl = ref<HTMLElement | null>(null);
const showScrollHint = ref(false);

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
      :aria-disabled="!canBulkDownload"
      :aria-label="t('files.browser.downloadSelected')"
      @click="onBulkDownload"
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
        :aria-disabled="!canBulkAct"
        :aria-label="t('files.actions.copy')"
        @click="onBulkCopy"
      >
        <template #icon>
          <Copy :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ t("files.actions.copy") }}</span>
      </NButton>
      <NButton
        size="small"
        secondary
        :aria-disabled="!canBulkAct"
        :aria-label="t('files.actions.cut')"
        @click="onBulkCut"
      >
        <template #icon>
          <Scissors :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ t("files.actions.cut") }}</span>
      </NButton>
      <NButton
        size="small"
        secondary
        :aria-disabled="!canBulkAct"
        :aria-label="t('files.actions.compress')"
        @click="onBulkCompress"
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
        :aria-disabled="!canBulkAct"
        :aria-label="t('common.actions.delete')"
        @click="onBulkDelete"
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
      :aria-label="t('files.browser.clear')"
      @click="emit('clear')"
    >
      <template #icon>
        <X :size="14" :stroke-width="1.8" aria-hidden="true" />
      </template>
      <span class="fm-btn-label">{{ t("files.browser.clear") }}</span>
    </NButton>
    <span v-if="showScrollHint" class="fm-scroll-hint" aria-hidden="true">
      <ChevronRight :size="14" :stroke-width="2" />
    </span>
  </div>
</template>

<style scoped>
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

.fm-selection-actions :deep(.n-button[aria-disabled="true"]) {
  opacity: var(--n-opacity-disabled, 0.5);
  cursor: not-allowed;
}

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
  .fm-btn-label {
    display: none;
  }

  .fm-selection-actions :deep(.n-button) {
    min-width: 44px;
    height: 44px;
  }

  .fm-grid-bulk .fm-selection-actions :deep(.n-button) {
    min-width: auto;
    height: var(--space-8);
  }
}
</style>
