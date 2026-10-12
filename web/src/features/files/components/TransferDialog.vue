<script setup lang="ts">
import { ArrowUp, ChevronRight, Folder } from "@lucide/vue";
import { NButton, NModal, NSpin } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { listEntries } from "@/features/files/api/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import {
  breadcrumbSegments,
  joinPath,
  parentPath,
} from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

interface Props {
  show: boolean;
  mode: "move" | "copy";
  entries: FilesEntry[];
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t } = useI18n();
const message = useMessage();
const files = useFilesStore();
const teams = useTeamsStore();

const pickerPath = ref("");
const directoryEntries = ref<FilesEntry[]>([]);
const pickerLoading = ref(false);
const pickerError = ref<string | null>(null);

/** crumbs are the root, then each directory of the picker's current path. */
const crumbs = computed(() =>
  breadcrumbSegments(files.rootName, pickerPath.value),
);

/** canPick is false while a listing is in flight, so a race cannot confirm. */
const canPick = computed<boolean>(() => !pickerLoading.value && !pickerError.value);

/** loadPicker reads only the directories of the picker's current path. */
async function loadPicker(): Promise<void> {
  pickerLoading.value = true;
  pickerError.value = null;
  try {
    const listing = await listEntries(files.serverId, teams.activeTeamId, {
      root: files.rootName,
      path: pickerPath.value,
    });
    directoryEntries.value = listing.entries.filter(
      (entry) => entry.kind === "directory",
    );
  } catch {
    directoryEntries.value = [];
    pickerError.value = String(t("files.dialogs.transferPickerError"));
  } finally {
    pickerLoading.value = false;
  }
}

// Reset to the source directory whenever the dialog opens.
watch(
  () => props.show,
  (open) => {
    if (open) {
      pickerPath.value = files.path;
      void loadPicker();
    }
  },
);

/** openDirectory descends into a subdirectory. */
function openDirectory(entry: FilesEntry): void {
  pickerPath.value = joinPath(pickerPath.value, entry.name);
  void loadPicker();
}

/** goUp moves to the parent directory (a no-op at the root). */
function goUp(): void {
  if (pickerPath.value === "") {
    return;
  }
  pickerPath.value = parentPath(pickerPath.value);
  void loadPicker();
}

/** goToPath jumps to one breadcrumb. */
function goToPath(path: string): void {
  if (path === pickerPath.value) {
    return;
  }
  pickerPath.value = path;
  void loadPicker();
}

/** confirm transfers the selected entries into the picked directory. */
async function confirm(): Promise<void> {
  if (!canPick.value) {
    return;
  }
  const names = props.entries.map((entry) => entry.name);
  try {
    const summary = await files.promptTransfer(
      props.mode,
      files.path,
      pickerPath.value,
      names,
    );
    if (summary.failed.length > 0) {
      message.warning(
        String(
          t("files.dialogs.transferPartial", {
            failed: summary.failed.length,
            error: summary.failed[0]?.error ?? "",
          }),
        ),
      );
    } else {
      message.success(
        String(
          t(
            props.mode === "move"
              ? "files.dialogs.moved"
              : "files.dialogs.copied",
            { count: summary.done.length },
          ),
        ),
      );
    }
    emit("update:show", false);
  } catch {
    message.error(String(t("common.errors.unexpected")));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 520px; max-width: 94vw"
    :title="
      props.mode === 'move'
        ? t('files.dialogs.transferTitleMove')
        : t('files.dialogs.transferTitleCopy')
    "
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <p class="small muted">{{ t("files.dialogs.transferHint") }}</p>

    <nav class="picker-path" :aria-label="t('files.dialogs.transferDestination')">
      <button
        type="button"
        class="crumb"
        :disabled="pickerPath === ''"
        :aria-label="t('files.browser.parent')"
        @click="goUp"
      >
        <ArrowUp :size="13" :stroke-width="1.8" aria-hidden="true" />
      </button>
      <template v-for="(crumb, index) in crumbs" :key="crumb.path">
        <span v-if="index > 0" class="sep" aria-hidden="true">/</span>
        <button
          type="button"
          class="crumb"
          :class="{ 'is-current': index === crumbs.length - 1 }"
          @click="goToPath(crumb.path)"
        >
          {{ crumb.name }}
        </button>
      </template>
    </nav>

    <div class="picker-list">
      <NSpin v-if="pickerLoading" size="small" />
      <p v-else-if="pickerError" class="small picker-error">{{ pickerError }}</p>
      <p v-else-if="directoryEntries.length === 0" class="small muted picker-empty">
        {{ t("files.dialogs.transferNoSubfolders") }}
      </p>
      <button
        v-for="entry in directoryEntries"
        v-else
        :key="entry.name"
        type="button"
        class="picker-row"
        @click="openDirectory(entry)"
      >
        <Folder :size="15" :stroke-width="1.7" aria-hidden="true" />
        <span class="grow">{{ entry.name }}</span>
        <ChevronRight :size="14" :stroke-width="1.8" aria-hidden="true" />
      </button>
    </div>

    <template #footer>
      <div class="dialog-foot">
        <span class="grow small muted">
          {{ t("files.dialogs.transferSelected", { count: props.entries.length }) }}
        </span>
        <NButton @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="files.acting"
          :disabled="!canPick"
          @click="() => void confirm()"
        >
          {{ props.mode === "move" ? t("files.actions.move") : t("files.actions.copy") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  margin: 0;
}

.picker-path {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-wrap: wrap;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 3px;
  margin: var(--space-3) 0;
  min-height: 32px;
}

.crumb {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border: 0;
  background: none;
  border-radius: var(--radius-sm);
  font-size: var(--text-xs);
  color: var(--muted);
  cursor: pointer;
}

.crumb:hover:not(:disabled) {
  background: var(--hover-row);
  color: var(--fg-2);
}

.crumb:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.crumb.is-current {
  background: var(--selected-row);
  color: var(--fg-2);
  font-weight: 600;
}

.sep {
  color: var(--meta);
}

.picker-list {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  max-height: 240px;
  min-height: 120px;
  overflow-y: auto;
  padding: var(--space-1);
}

.picker-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: 6px var(--space-2);
  border: 0;
  background: none;
  border-radius: var(--radius-sm);
  color: var(--fg-2);
  font-size: var(--text-sm);
  text-align: left;
  cursor: pointer;
}

.picker-row:hover {
  background: var(--hover-row);
}

.picker-row .grow {
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.picker-error {
  color: var(--danger-ink);
  padding: var(--space-2);
}

.picker-empty {
  padding: var(--space-2);
}

.dialog-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.dialog-foot .grow {
  flex: 1 1 auto;
}
</style>
