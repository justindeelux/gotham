<script setup lang="ts">
import { CircleAlert, RotateCcw, Trash2 } from "@lucide/vue";
import { NAlert, NButton, NModal, NPopconfirm, useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeFilesError, isConflictError } from "@/features/files/api/files";
import type { TrashEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { useTrashStore } from "@/features/files/stores/trash";
import { entryTypeLabel, formatTimestamp } from "@/features/files/utils/files";
import { formatBytes } from "@/shared/utils/format";

interface Props {
  show: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t, locale } = useI18n();
const message = useMessage();
const trash = useTrashStore();
const files = useFilesStore();

/** conflictName is the entry whose restore met an occupied destination (409). */
const conflictName = ref<string | null>(null);

// Load the bin for the browser's current root on every open, so the dialog
// never shows a stale listing from a previous root or team.
watch(
  () => props.show,
  (open) => {
    conflictName.value = null;
    if (open) {
      void trash.load();
    }
  },
  { immediate: true },
);

/**
 * canAct gates restore/purge/empty: the caller must be able to write (owner or
 * admin on a writable root) AND the loaded listing must belong to the current
 * node+root, so a leftover id can never act on a different scope.
 */
const canAct = computed<boolean>(() => files.canWrite && trash.scopeMatches);

/** typeLabel resolves the Type column through the shared files catalog. */
function typeLabel(entry: TrashEntry): string {
  return entryTypeLabel(entry, locale.value);
}

/** emptyConfirmText avoids a specific count when the listing is capped. */
const emptyConfirmText = computed<string>(() =>
  String(
    t(
      trash.truncated ? "files.trash.emptyMessageAll" : "files.trash.emptyMessage",
      { count: trash.entries.length },
    ),
  ),
);

/** restore returns one entry to its original path. A 409 (occupied path) is
 *  surfaced inline with its own clear message instead of the generic conflict
 *  copy, so the user knows to move the occupant and retry. */
async function restore(entry: TrashEntry): Promise<void> {
  if (trash.acting || !canAct.value) {
    return;
  }
  conflictName.value = null;
  try {
    await trash.restore(entry);
    message.success(String(t("files.trash.restored", { name: entry.name })));
  } catch (err) {
    if (isConflictError(err)) {
      conflictName.value = entry.name;
      return;
    }
    message.error(describeFilesError(err));
  }
}

/** purge permanently deletes one entry from the bin. */
async function purge(entry: TrashEntry): Promise<void> {
  if (trash.acting || !canAct.value) {
    return;
  }
  try {
    await trash.purge(entry);
    message.success(String(t("files.trash.purged", { name: entry.name })));
  } catch (err) {
    message.error(describeFilesError(err));
  }
}

/** empty permanently deletes every entry in the root's bin. */
async function empty(): Promise<void> {
  if (trash.acting || !canAct.value) {
    return;
  }
  try {
    const removed = await trash.empty();
    message.success(String(t("files.trash.emptied", { count: removed })));
  } catch (err) {
    message.error(describeFilesError(err));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 760px; max-width: 94vw"
    :title="t('files.trash.title')"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <p class="small muted trash-hint">{{ t("files.trash.hint") }}</p>
    <p v-if="!files.canWrite" class="small muted trash-hint">{{ t("files.trash.writeHint") }}</p>

    <NAlert
      v-if="conflictName"
      class="trash-truncated"
      type="error"
      :title="t('files.trash.restoreConflictTitle')"
    >
      {{ t("files.trash.restoreConflict", { name: conflictName }) }}
    </NAlert>

    <div v-if="trash.loading && trash.entries.length === 0" class="trash-state">
      <span class="trash-spinner" aria-hidden="true"></span>
      <p class="small muted">{{ t("files.browser.loading") }}</p>
    </div>

    <div v-else-if="trash.error && trash.entries.length === 0" class="trash-state">
      <CircleAlert class="trash-danger" :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4 class="trash-danger">{{ t("files.trash.loadErrorTitle") }}</h4>
      <p class="small muted">{{ trash.error }}</p>
      <NButton size="small" secondary @click="() => void trash.load()">
        {{ t("common.actions.retry") }}
      </NButton>
    </div>

    <div v-else-if="trash.entries.length === 0" class="trash-state">
      <Trash2 :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4>{{ t("files.trash.emptyState") }}</h4>
      <p class="small muted">{{ t("files.trash.emptyHint") }}</p>
    </div>

    <template v-else>
      <p class="small muted trash-hint">
        {{ t("files.trash.count", { count: trash.entries.length }) }}
      </p>
      <NAlert v-if="trash.truncated" class="trash-truncated" type="warning">
        {{ t("files.trash.truncated") }}
      </NAlert>

      <div class="trash-table" role="table">
        <div class="trash-row trash-head" role="row">
          <div role="columnheader">{{ t("files.trash.columnName") }}</div>
          <div role="columnheader">{{ t("files.trash.columnDeleted") }}</div>
          <div role="columnheader" class="trash-size">{{ t("files.trash.columnSize") }}</div>
          <div role="columnheader">{{ t("files.trash.columnKind") }}</div>
          <div role="columnheader" class="trash-right">{{ t("files.trash.columnActions") }}</div>
        </div>

        <div v-for="entry in trash.entries" :key="entry.id" class="trash-row" role="row">
          <div role="cell" class="trash-name mono">
            <b :title="entry.name">{{ entry.name }}</b>
          </div>
          <div role="cell" class="trash-when">{{ formatTimestamp(entry.deleted_at) }}</div>
          <div role="cell" class="trash-size">{{ formatBytes(entry.size) }}</div>
          <div role="cell">{{ typeLabel(entry) }}</div>
          <div role="cell" class="trash-right trash-actions">
            <NButton
              size="tiny"
              secondary
              :disabled="!canAct || trash.acting"
              :aria-label="t('files.trash.restoreAria', { name: entry.name })"
              @click="() => void restore(entry)"
            >
              <template #icon>
                <RotateCcw :size="13" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.trash.restore") }}
            </NButton>
            <NPopconfirm
              @positive-click="() => void purge(entry)"
            >
              <template #trigger>
                <NButton
                  size="tiny"
                  type="error"
                  secondary
                  :disabled="!canAct || trash.acting"
                  :aria-label="t('files.trash.purgeAria', { name: entry.name })"
                >
                  <template #icon>
                    <Trash2 :size="13" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </template>
              {{ t("files.trash.purgeMessage", { name: entry.name }) }}
            </NPopconfirm>
          </div>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="trash-foot">
        <NPopconfirm
          v-if="trash.entries.length > 0"
          @positive-click="() => void empty()"
        >
          <template #trigger>
            <NButton
              type="error"
              secondary
              :disabled="!canAct || trash.acting"
              :loading="trash.acting"
            >
              <template #icon>
                <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.trash.empty") }}
            </NButton>
          </template>
          {{ emptyConfirmText }}
        </NPopconfirm>
        <NButton @click="emit('update:show', false)">
          {{ t("common.actions.close") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
/* The modal contract (see main.css): header and footer are fixed,
 * only the entry list scrolls, and the card never exceeds the viewport. */
.trash-hint {
  margin: 0 0 var(--space-2);
}

.trash-truncated {
  margin-bottom: var(--space-3);
}

.trash-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-8) var(--space-4);
  text-align: center;
}

.trash-state h4 {
  margin: 0;
}

.trash-danger {
  color: var(--danger);
}

.trash-spinner {
  width: 22px;
  height: 22px;
  border: 2px solid var(--border-soft);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: trash-spin 0.8s linear infinite;
}

@keyframes trash-spin {
  to {
    transform: rotate(360deg);
  }
}

.trash-table {
  display: flex;
  flex-direction: column;
  max-height: min(52vh, 420px);
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}

.trash-row {
  display: grid;
  grid-template-columns: minmax(160px, 2fr) 130px 84px 80px 160px;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  font-size: var(--text-sm);
}

.trash-row:last-child {
  border-bottom: 0;
}

.trash-head {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--surface-warm);
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--muted);
}

.trash-name b {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: block;
}

.trash-when,
.trash-size {
  font-size: var(--text-xs);
  color: var(--muted);
  white-space: nowrap;
}

.trash-right {
  text-align: right;
}

.trash-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}

.trash-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
