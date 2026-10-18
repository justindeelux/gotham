<script setup lang="ts">
import {
  AlertCircle,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  FilePlus,
  FolderOpen,
  FolderPlus,
  MoreHorizontal,
  RefreshCw,
  Search,
} from "@lucide/vue";
import {
  NButton,
  NCheckbox,
  NDropdown,
  NInput,
  NSpin,
  useMessage,
} from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useElementSize } from "@vueuse/core";

import EditorExplorerDialog from "@/features/files/components/EditorExplorerDialog.vue";
import FileTypeIcon from "@/features/files/components/FileTypeIcon.vue";
import { describeFilesError } from "@/features/files/api/files";
import "@/features/files/locales/editorMessages";
import { ExplorerDirtyError, useExplorerStore } from "@/features/files/stores/explorer";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";
import type {
  ExplorerDialogMode,
  ExplorerEntryRow,
  ExplorerRow,
} from "@/features/files/utils/explorer";
import {
  firstEntryIndex,
  flattenTree,
  lastEntryIndex,
  nextEntryIndex,
} from "@/features/files/utils/explorer";
import {
  entryIconKind,
  parentPath,
} from "@/features/files/utils/files";
import { windowRange as virtualWindow } from "@/features/files/utils/virtual";

interface Props {
  serverId: string;
  /** width is the panel's current drag width in px (200–480). */
  width?: number;
}

const props = withDefaults(defineProps<Props>(), { width: 260 });
const emit = defineEmits<{ resize: [width: number] }>();

const { t } = useI18n();
const message = useMessage();
const explorer = useExplorerStore();
const editor = useEditorStore();
const files = useFilesStore();
const teams = useTeamsStore();

/** ROW_HEIGHT mirrors the `.ex-row` height so the window maths stays exact. */
const ROW_HEIGHT = 26;
const MIN_WIDTH = 200;
const MAX_WIDTH = 480;

// ── Virtualized tree ─────────────────────────────────────────────────────
const scrollEl = ref<HTMLElement | null>(null);
const treeEl = ref<HTMLElement | null>(null);
const scrollTop = ref(0);
const { height: viewportHeight } = useElementSize(scrollEl);
/** activeIndex is the roving tree row (the container owns DOM focus). */
const activeIndex = ref(0);

const rows = computed<ExplorerRow[]>(() => {
  if (explorer.searchActive) {
    return explorer.searchHits.map((hit) => ({
      type: "entry" as const,
      path: hit.path,
      name: hit.entry.name,
      kind: hit.entry.kind,
      depth: 0,
      expanded: false,
      entry: hit.entry,
    }));
  }
  return flattenTree(explorer.rootPath, explorer.dirs, explorer.isExpanded);
});

/** rowKey is the structural identity the active-index sync watches. */
const rowKey = computed<string>(() => rows.value.map((row) => row.path).join("\n"));

const windowRange = computed(() =>
  virtualWindow(
    rows.value.length,
    ROW_HEIGHT,
    scrollTop.value,
    viewportHeight.value > 0 ? viewportHeight.value : 360,
    0,
    8,
  ),
);

/** windowRows carries each row's stable flat index for the ARIA/active lookup. */
const windowRows = computed(() =>
  rows.value
    .slice(windowRange.value.start, windowRange.value.end)
    .map((row, offset) => ({ row, index: windowRange.value.start + offset })),
);

/** activeRow is the row the tree's aria-activedescendant points at. */
const activeRow = computed<ExplorerRow | null>(() => rows.value[activeIndex.value] ?? null);
const activeDescendant = computed<string | undefined>(() =>
  activeRow.value?.type === "entry" ? `ex-row-${activeIndex.value}` : undefined,
);

// ── Panel root / selection helpers ───────────────────────────────────────
const rootLabel = computed<string>(() =>
  explorer.rootPath === ""
    ? explorer.rootName
    : `${explorer.rootName} / ${explorer.rootPath}`,
);

/** selectedEntry resolves the focused row when it is a file or folder. */
const selectedEntry = computed<ExplorerEntryRow | null>(() => {
  const row = rows.value.find(
    (item) => item.type === "entry" && item.path === explorer.selectedPath,
  );
  return row?.type === "entry" ? row : null;
});

/** selectedDirectory is the folder a "set as root" click would open. */
const selectedDirectory = computed<string>(() => {
  const row = selectedEntry.value;
  if (row === null) {
    return "";
  }
  return row.kind === "directory" ? row.path : parentPath(row.path);
});

/** activePath is the open editor tab's path, highlighted in the tree. */
const activePath = computed<string>(() => {
  const tab = editor.activeTab;
  return tab && tab.root === explorer.rootName ? tab.path : "";
});

/** openPaths are every open tab, so the tree can mark them subtly. */
const openPaths = computed<Set<string>>(() => {
  const paths = new Set<string>();
  for (const tab of editor.tabs) {
    if (tab.root === explorer.rootName) {
      paths.add(tab.path);
    }
  }
  return paths;
});

// ── Loading / mount ──────────────────────────────────────────────────────
function bindExplorer(): void {
  // The tree starts at the file browser's current folder, like aaPanel's
  // editor panel; later browser navigation does not re-root it.
  explorer.bind(props.serverId, files.rootName, files.path);
  void explorer.init();
}

onMounted(() => {
  bindExplorer();
});

onBeforeUnmount(() => {
  if (searchTimer !== null) {
    clearTimeout(searchTimer);
    searchTimer = null;
  }
});

watch(
  () => props.serverId,
  () => bindExplorer(),
);

// ── Keyboard / active-row navigation ─────────────────────────────────────
/** syncActive keeps the roving row on a real entry after a structural change. */
async function syncActive(): Promise<void> {
  const list = rows.value;
  if (list.length === 0) {
    activeIndex.value = 0;
    return;
  }
  const selected = list.findIndex(
    (row) => row.type === "entry" && row.path === explorer.selectedPath,
  );
  if (selected >= 0) {
    activeIndex.value = selected;
    await nextTick();
    revealIndex(selected);
    return;
  }
  if (activeIndex.value >= list.length || list[activeIndex.value]?.type !== "entry") {
    const first = firstEntryIndex(list);
    activeIndex.value = first >= 0 ? first : Math.min(activeIndex.value, list.length - 1);
  }
}

watch(rowKey, () => void syncActive(), { flush: "post" });

/** revealIndex scrolls one row into the virtual window. */
function revealIndex(index: number): void {
  const el = scrollEl.value;
  if (!el) {
    return;
  }
  const top = index * ROW_HEIGHT;
  const height = el.clientHeight || (viewportHeight.value > 0 ? viewportHeight.value : 360);
  if (top < el.scrollTop) {
    el.scrollTop = top;
  } else if (top + ROW_HEIGHT > el.scrollTop + height) {
    el.scrollTop = top + ROW_HEIGHT - height;
  }
  scrollTop.value = el.scrollTop;
}

/** setActive moves the roving row and keeps it rendered. */
function setActive(index: number): void {
  if (index < 0 || index >= rows.value.length) {
    return;
  }
  activeIndex.value = index;
  const row = rows.value[index];
  if (row?.type === "entry") {
    explorer.select(row.path);
  }
  revealIndex(index);
}

/** onScroll drives the window from the scrollport. */
function onScroll(event: Event): void {
  scrollTop.value = (event.currentTarget as HTMLElement).scrollTop;
}

/** onTreeFocus lands the roving row on the first entry when focus arrives. */
function onTreeFocus(): void {
  if (activeRow.value?.type === "entry") {
    return;
  }
  const first = firstEntryIndex(rows.value);
  if (first >= 0) {
    activeIndex.value = first;
    revealIndex(first);
  }
}

/** statusDir resolves the folder an inline status row belongs to. */
function statusDir(row: ExplorerRow): string {
  return row.type === "status" ? row.dir : "";
}

/** onTreeKeydown implements the ARIA tree keyboard model. */
function onTreeKeydown(event: KeyboardEvent): void {
  const list = rows.value;
  if (list.length === 0) {
    return;
  }
  const index = Math.min(Math.max(activeIndex.value, 0), list.length - 1);
  const row = list[index];

  switch (event.key) {
    case "ArrowDown": {
      event.preventDefault();
      const next = nextEntryIndex(list, index + 1, 1);
      if (next >= 0) {
        setActive(next);
      }
      break;
    }
    case "ArrowUp": {
      event.preventDefault();
      const prev = nextEntryIndex(list, index - 1, -1);
      if (prev >= 0) {
        setActive(prev);
      }
      break;
    }
    case "ArrowRight": {
      if (row.type !== "entry" || row.kind !== "directory") {
        break;
      }
      event.preventDefault();
      if (!row.expanded) {
        void explorer.expand(row.path).then(() => void nextTick(() => revealIndex(index)));
      } else {
        const child = nextEntryIndex(list, index + 1, 1);
        if (child >= 0 && list[child].type === "entry" && list[child].depth > row.depth) {
          setActive(child);
        }
      }
      break;
    }
    case "ArrowLeft": {
      if (row.type !== "entry") {
        break;
      }
      if (row.kind === "directory" && row.expanded) {
        event.preventDefault();
        explorer.collapse(row.path);
        break;
      }
      const parent = parentPath(row.path);
      const parentIndex = list.findIndex(
        (item) => item.type === "entry" && item.path === parent,
      );
      if (parentIndex >= 0) {
        event.preventDefault();
        setActive(parentIndex);
      }
      break;
    }
    case "Home": {
      event.preventDefault();
      const first = firstEntryIndex(list);
      if (first >= 0) {
        setActive(first);
      }
      break;
    }
    case "End": {
      event.preventDefault();
      const last = lastEntryIndex(list);
      if (last >= 0) {
        setActive(last);
      }
      break;
    }
    case "Enter": {
      if (row.type === "status") {
        if (row.status === "error") {
          event.preventDefault();
          void explorer.load(row.dir, true);
        }
        break;
      }
      event.preventDefault();
      void activateRow(row);
      break;
    }
    case " ": {
      if (row.type === "entry" && row.kind === "directory") {
        event.preventDefault();
        void explorer.toggle(row.path);
      }
      break;
    }
    case "F2": {
      if (row.type === "entry" && explorer.canWrite) {
        event.preventDefault();
        beginRename(row);
      }
      break;
    }
    case "Delete": {
      if (row.type === "entry" && explorer.canWrite) {
        event.preventDefault();
        beginDelete(row);
      }
      break;
    }
    default:
      break;
  }
}

/** activateRow opens a file in a tab or toggles a folder. */
async function activateRow(row: ExplorerEntryRow): Promise<void> {
  if (row.kind === "directory") {
    if (explorer.searchActive) {
      await explorer.setRoot(explorer.rootName, row.path);
      explorer.clearSearch();
      return;
    }
    await explorer.toggle(row.path);
    return;
  }
  await editor.openFile(props.serverId, teams.activeTeamId, explorer.rootName, row.path);
}

/** onRowClick focuses the tree and performs the row's default action. */
function onRowClick(row: ExplorerRow, index: number): void {
  treeEl.value?.focus({ preventScroll: true });
  if (row.type === "status") {
    if (row.status === "error") {
      void explorer.load(row.dir, true);
    }
    return;
  }
  activeIndex.value = index;
  explorer.select(row.path);
  void activateRow(row);
}

// ── Row context menu ─────────────────────────────────────────────────────
/** rowMenuOptions lists the actions available on one row. */
function rowMenuOptions(row: ExplorerEntryRow): DropdownOption[] {
  const options: DropdownOption[] = [];
  if (row.kind === "directory") {
    options.push(
      { label: String(t("files.editor.explorer.open")), key: "open" },
      { label: String(t("files.editor.explorer.root")), key: "root" },
      { type: "divider", key: "d1" },
      { label: String(t("files.editor.explorer.newFile")), key: "new-file", disabled: !explorer.canWrite },
      { label: String(t("files.editor.explorer.newFolder")), key: "new-folder", disabled: !explorer.canWrite },
    );
  } else {
    options.push({ label: String(t("files.editor.explorer.open")), key: "open" });
  }
  options.push(
    { type: "divider", key: "d2" },
    { label: String(t("files.editor.explorer.rename")), key: "rename", disabled: !explorer.canWrite },
    { label: String(t("files.editor.explorer.delete")), key: "delete", disabled: !explorer.canWrite },
  );
  return options;
}

/** onRowMenu dispatches one row action. */
function onRowMenu(key: string, row: ExplorerEntryRow): void {
  switch (key) {
    case "open":
      void activateRow(row);
      break;
    case "root":
      void explorer.setRoot(explorer.rootName, row.path);
      explorer.clearSearch();
      break;
    case "new-file":
      beginCreate("file", row.path);
      break;
    case "new-folder":
      beginCreate("directory", row.path);
      break;
    case "rename":
      beginRename(row);
      break;
    case "delete":
      beginDelete(row);
      break;
    default:
      break;
  }
}

// ── Search ───────────────────────────────────────────────────────────────
const searchText = computed<string>({
  get: () => explorer.searchQuery,
  set: (value) => explorer.setSearchQuery(value),
});
const includeSubdirectories = computed<boolean>({
  get: () => explorer.searchRecursive,
  set: (value) => explorer.setSearchRecursive(value),
});

let searchTimer: ReturnType<typeof setTimeout> | null = null;
const SEARCH_DEBOUNCE_MS = 250;

/** runSearchNow fires the search immediately (Enter or Retry). */
function runSearchNow(): void {
  if (searchTimer !== null) {
    clearTimeout(searchTimer);
    searchTimer = null;
  }
  void explorer.runSearch();
}

watch(
  () => [explorer.searchQuery, explorer.searchRecursive] as const,
  ([query]) => {
    if (searchTimer !== null) {
      clearTimeout(searchTimer);
      searchTimer = null;
    }
    if (query.trim() === "") {
      explorer.clearSearch();
      return;
    }
    searchTimer = setTimeout(runSearchNow, SEARCH_DEBOUNCE_MS);
  },
);

/** clearSearch restores the tree. */
function clearSearch(): void {
  explorer.clearSearch();
  void nextTick(() => treeEl.value?.focus({ preventScroll: true }));
}

// ── Create / rename / delete dialogs ─────────────────────────────────────
interface DialogState {
  mode: ExplorerDialogMode;
  path: string;
  name: string;
  dir: string;
  /** isDirectory lets a search-hit delete recurse without a cached listing. */
  isDirectory: boolean;
}

const dialog = ref<DialogState | null>(null);
const dialogBusy = ref(false);

const dialogExisting = computed<string[]>(() => {
  const state = dialog.value;
  if (state === null || state.mode === "delete") {
    return [];
  }
  return (explorer.dirs[state.dir]?.entries ?? []).map((entry) => entry.name);
});

/** isDirtyBlocked warns and refuses a write blocked by an unsaved tab. */
function isDirtyBlocked(path: string): boolean {
  const blocked = explorer.blockingNames([path]);
  if (blocked.length === 0) {
    return false;
  }
  message.warning(
    String(t("files.editor.dirtyBlocked", { names: blocked.join(", ") })),
  );
  return true;
}

/** beginCreate opens the create dialog in `dir` (the selected folder by default). */
function beginCreate(kind: "file" | "directory", dir?: string): void {
  const folder = dir ?? (selectedDirectory.value || explorer.rootPath);
  dialog.value = {
    mode: kind === "directory" ? "create-folder" : "create-file",
    path: "",
    name: "",
    dir: folder,
    isDirectory: kind === "directory",
  };
}

/** beginRename opens the rename dialog, refusing a dirty entry first. */
function beginRename(row: ExplorerEntryRow): void {
  if (isDirtyBlocked(row.path)) {
    return;
  }
  dialog.value = {
    mode: "rename",
    path: row.path,
    name: row.name,
    dir: parentPath(row.path),
    isDirectory: row.kind === "directory",
  };
}

/** beginDelete opens the recycle-bin confirmation, refusing a dirty entry first. */
function beginDelete(row: ExplorerEntryRow): void {
  if (isDirtyBlocked(row.path)) {
    return;
  }
  dialog.value = {
    mode: "delete",
    path: row.path,
    name: row.name,
    dir: parentPath(row.path),
    isDirectory: row.kind === "directory",
  };
}

/** closeDialog drops the dialog state. */
function closeDialog(): void {
  dialog.value = null;
}

/** submitDialog performs the dialog's mutation and reports the outcome. */
async function submitDialog(name: string): Promise<void> {
  const state = dialog.value;
  if (state === null || dialogBusy.value) {
    return;
  }
  dialogBusy.value = true;
  try {
    if (state.mode === "create-file" || state.mode === "create-folder") {
      await explorer.create(
        state.mode === "create-folder" ? "directory" : "file",
        name,
        state.dir,
      );
      message.success(String(t("files.editor.explorer.created", { name })));
    } else if (state.mode === "rename") {
      await explorer.rename(state.path, name);
      message.success(String(t("files.editor.explorer.renamed", { name })));
    } else {
      await explorer.remove(
        state.path,
        state.isDirectory ? "directory" : "file",
      );
      message.success(
        String(t("files.editor.explorer.deleted", { name: state.name })),
      );
    }
    dialog.value = null;
  } catch (err) {
    if (err instanceof ExplorerDirtyError) {
      message.warning(
        String(t("files.editor.dirtyBlocked", { names: err.names.join(", ") })),
      );
      dialog.value = null;
    } else {
      message.error(describeFilesError(err));
    }
  } finally {
    dialogBusy.value = false;
  }
}

// ── Toolbar actions ──────────────────────────────────────────────────────
/** setSelectedAsRoot re-roots the tree at the selected folder. */
function setSelectedAsRoot(): void {
  if (selectedDirectory.value === "") {
    return;
  }
  void explorer.setRoot(explorer.rootName, selectedDirectory.value);
}

/** refreshTree reloads the root and every expanded folder. */
function refreshTree(): void {
  void explorer.refresh();
}

// ── Resize ───────────────────────────────────────────────────────────────
const dragState = ref<{ pointerId: number; startX: number; startWidth: number } | null>(null);

/** clampWidth keeps the panel usable without swallowing the editor. */
function clampWidth(value: number): number {
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, value));
}

/** onSplitDown starts a drag on the vertical splitter. */
function onSplitDown(event: PointerEvent): void {
  if (event.button !== 0) {
    return;
  }
  const handle = event.currentTarget;
  if (!(handle instanceof HTMLElement)) {
    return;
  }
  dragState.value = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startWidth: props.width,
  };
  handle.setPointerCapture(event.pointerId);
  event.preventDefault();
}

/** onSplitMove reports the clamped width while dragging. */
function onSplitMove(event: PointerEvent): void {
  const state = dragState.value;
  if (state === null || event.pointerId !== state.pointerId) {
    return;
  }
  emit("resize", clampWidth(state.startWidth + event.clientX - state.startX));
}

/** onSplitUp ends the resize drag. */
function onSplitUp(event: PointerEvent): void {
  if (dragState.value !== null && event.pointerId === dragState.value.pointerId) {
    dragState.value = null;
  }
}

/** onSplitKey resizes from the keyboard (16 px per press). */
function onSplitKey(event: KeyboardEvent): void {
  if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") {
    return;
  }
  event.preventDefault();
  const delta = event.key === "ArrowRight" ? 16 : -16;
  emit("resize", clampWidth(props.width + delta));
}
</script>

<template>
  <aside
    class="ex-panel"
    :style="{ width: `${props.width}px` }"
    :aria-label="t('files.editor.explorer.heading')"
  >
    <header class="ex-head">
      <div class="ex-bar">
        <span class="ex-title">{{ t("files.editor.explorer.heading") }}</span>
        <span class="grow" />
        <NButton
          quaternary
          size="tiny"
          class="ex-action"
          :disabled="explorer.rootPath === ''"
          :aria-label="t('files.editor.explorer.up')"
          @click="() => void explorer.up()"
        >
          <template #icon>
            <ArrowUp :size="15" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
        <NButton
          quaternary
          size="tiny"
          class="ex-action"
          :loading="explorer.rootLoading"
          :aria-label="t('files.editor.explorer.refresh')"
          @click="refreshTree"
        >
          <template #icon>
            <RefreshCw :size="15" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
        <NButton
          quaternary
          size="tiny"
          class="ex-action"
          :disabled="selectedDirectory === ''"
          :aria-label="t('files.editor.explorer.root')"
          @click="setSelectedAsRoot"
        >
          <template #icon>
            <FolderOpen :size="15" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
        <NButton
          v-if="explorer.canWrite"
          quaternary
          size="tiny"
          class="ex-action"
          :aria-label="t('files.editor.explorer.newFile')"
          @click="beginCreate('file')"
        >
          <template #icon>
            <FilePlus :size="15" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
        <NButton
          v-if="explorer.canWrite"
          quaternary
          size="tiny"
          class="ex-action"
          :aria-label="t('files.editor.explorer.newFolder')"
          @click="beginCreate('directory')"
        >
          <template #icon>
            <FolderPlus :size="15" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
      </div>

      <div class="ex-root mono" :title="rootLabel">{{ rootLabel }}</div>

      <NInput
        v-model:value="searchText"
        size="small"
        clearable
        :placeholder="t('files.editor.explorer.searchPlaceholder')"
        :aria-label="t('files.editor.explorer.search')"
        @clear="clearSearch"
        @keydown.enter.prevent="runSearchNow"
      >
        <template #prefix>
          <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NInput>
      <NCheckbox v-model:checked="includeSubdirectories" size="small">
        {{ t("files.editor.explorer.includeSubdirs") }}
      </NCheckbox>
    </header>

    <div class="ex-scroll" ref="scrollEl" @scroll="onScroll">
      <div
        v-if="explorer.searchActive && explorer.searchLoading && rows.length === 0"
        class="ex-state"
      >
        <NSpin size="small" />
        <span>{{ t("files.editor.explorer.loading") }}</span>
      </div>
      <div v-else-if="explorer.searchActive && explorer.searchError" class="ex-state">
        <AlertCircle :size="18" :stroke-width="1.8" aria-hidden="true" />
        <span>{{ t("files.editor.explorer.searchFailed") }}</span>
        <NButton size="tiny" secondary @click="runSearchNow">
          {{ t("files.editor.explorer.retry") }}
        </NButton>
      </div>
      <div
        v-else-if="explorer.searchActive && !explorer.searchLoading && rows.length === 0"
        class="ex-state"
      >
        <span>{{ t("files.editor.explorer.searchEmpty") }}</span>
      </div>

      <div
        v-else
        ref="treeEl"
        class="ex-tree"
        role="tree"
        :aria-label="t('files.editor.explorer.heading')"
        :aria-activedescendant="activeDescendant"
        tabindex="0"
        @keydown="onTreeKeydown"
        @focus="onTreeFocus"
      >
        <div :style="{ height: `${windowRange.padTop}px` }" aria-hidden="true" />
        <template v-for="item in windowRows" :key="item.row.path">
          <div
            v-if="item.row.type === 'status'"
            class="ex-status"
            role="treeitem"
            aria-disabled="true"
            :aria-level="item.row.depth + 1"
            :style="{ paddingLeft: `${10 + item.row.depth * 14}px` }"
          >
            <template v-if="item.row.status === 'loading'">
              <NSpin :size="12" />
              <span>{{ t("files.editor.explorer.loading") }}</span>
            </template>
            <template v-else-if="item.row.status === 'error'">
              <AlertCircle :size="14" :stroke-width="1.8" aria-hidden="true" />
              <span>{{ t("files.editor.explorer.loadFailed") }}</span>
              <button
                type="button"
                class="ex-retry"
                @click="() => void explorer.load(statusDir(item.row), true)"
              >
                {{ t("files.editor.explorer.retry") }}
              </button>
            </template>
            <template v-else>
              <span>{{ t("files.editor.explorer.empty") }}</span>
            </template>
          </div>

          <div
            v-else
            :id="`ex-row-${item.index}`"
            class="ex-row"
            :class="{
              'is-active': item.index === activeIndex,
              'is-open': activePath === item.row.path,
              'is-folder': item.row.kind === 'directory',
            }"
            role="treeitem"
            :aria-level="item.row.depth + 1"
            :aria-selected="item.index === activeIndex"
            :aria-expanded="
              item.row.kind === 'directory' ? item.row.expanded : undefined
            "
            :data-path="item.row.path"
            :style="{ paddingLeft: `${10 + item.row.depth * 14}px` }"
            @mousedown.prevent
            @click="onRowClick(item.row, item.index)"
          >
            <span class="ex-caret" aria-hidden="true">
              <ChevronDown
                v-if="item.row.kind === 'directory' && item.row.expanded"
                :size="12"
                :stroke-width="2"
              />
              <ChevronRight
                v-else-if="item.row.kind === 'directory'"
                :size="12"
                :stroke-width="2"
              />
            </span>
            <FileTypeIcon :kind="entryIconKind(item.row.entry)" />
            <span class="ex-name" :title="item.row.name">{{ item.row.name }}</span>
            <span
              v-if="explorer.searchActive"
              class="ex-hint mono"
              :title="parentPath(item.row.path) || explorer.rootName"
            >
              {{ parentPath(item.row.path) || explorer.rootName }}
            </span>
            <span
              v-if="openPaths.has(item.row.path)"
              class="ex-open-dot"
              aria-hidden="true"
            />
            <NDropdown
              v-if="explorer.canWrite"
              trigger="click"
              size="small"
              :options="rowMenuOptions(item.row)"
              @select="(key: string | number) => onRowMenu(String(key), item.row as ExplorerEntryRow)"
            >
              <button
                type="button"
                class="ex-menu"
                tabindex="-1"
                :aria-label="t('files.editor.explorer.actionsFor', { name: item.row.name })"
                @mousedown.stop.prevent
                @click.stop
              >
                <MoreHorizontal :size="14" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </NDropdown>
          </div>
        </template>
        <div :style="{ height: `${windowRange.padBottom}px` }" aria-hidden="true" />
      </div>
    </div>

    <footer
      v-if="explorer.rootTruncated || (explorer.searchActive && explorer.searchTruncated)"
      class="ex-foot small muted"
    >
      {{
        explorer.searchActive
          ? t("files.editor.explorer.searchTruncated")
          : t("files.editor.explorer.truncated")
      }}
    </footer>

    <div
      class="ex-split"
      role="separator"
      aria-orientation="vertical"
      :aria-label="t('files.editor.explorer.resize')"
      :aria-valuenow="props.width"
      :aria-valuemin="MIN_WIDTH"
      :aria-valuemax="MAX_WIDTH"
      tabindex="0"
      @pointerdown="onSplitDown"
      @pointermove="onSplitMove"
      @pointerup="onSplitUp"
      @pointercancel="onSplitUp"
      @lostpointercapture="onSplitUp"
      @keydown="onSplitKey"
    />
  </aside>

  <EditorExplorerDialog
    :show="dialog !== null"
    :mode="dialog?.mode ?? 'create-file'"
    :name="dialog?.name ?? ''"
    :existing="dialogExisting"
    :busy="dialogBusy"
    @update:show="(value: boolean) => { if (!value) closeDialog(); }"
    @submit="(name: string) => void submitDialog(name)"
  />
</template>

<style scoped>
.ex-panel {
  position: relative;
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--surface-warm);
  border-right: 1px solid var(--border);
}

.ex-head {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
}

.ex-bar {
  display: flex;
  align-items: center;
  gap: 2px;
}

.ex-bar .grow {
  flex: 1 1 auto;
}

.ex-title {
  font-weight: 600;
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.ex-action {
  width: 24px;
  height: 24px;
}

.ex-root {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

.ex-scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

.ex-tree {
  padding-bottom: var(--space-2);
  outline: none;
}

.ex-tree:focus-visible {
  outline: 2px solid var(--accent-ink);
  outline-offset: -2px;
}

.ex-row {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  height: 26px;
  padding-right: var(--space-1);
  color: var(--fg-2);
  font-size: var(--text-sm);
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
}

.ex-row:hover {
  background: var(--accent-soft);
}

.ex-row.is-active {
  background: var(--accent-soft);
  color: var(--fg);
}

.ex-row.is-open .ex-name {
  color: var(--accent-ink);
}

.ex-row :deep(.fm-mark) {
  width: 16px;
  height: 16px;
}

.ex-row :deep(.fm-mark svg) {
  width: 14px;
  height: 14px;
}

.ex-caret {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  width: 12px;
  height: 12px;
  color: var(--muted);
}

.ex-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ex-hint {
  min-width: 0;
  margin-left: auto;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--muted);
  font-size: var(--text-xs);
}

.ex-open-dot {
  flex: 0 0 auto;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--accent);
}

.ex-menu {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  width: 20px;
  height: 20px;
  margin-left: auto;
  padding: 0;
  border: 0;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--muted);
  cursor: pointer;
  opacity: 0;
}

.ex-row:hover .ex-menu,
.ex-row.is-active .ex-menu,
.ex-menu:focus-visible {
  opacity: 1;
}

/* Touch has no hover: keep the row menu reachable. */
@media (hover: none) {
  .ex-menu {
    opacity: 1;
  }
}

.ex-menu:hover {
  background: var(--border);
  color: var(--fg-2);
}

.ex-status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: 26px;
  color: var(--muted);
  font-size: var(--text-xs);
}

.ex-retry {
  padding: 0;
  border: 0;
  background: none;
  color: var(--accent-ink);
  font: inherit;
  text-decoration: underline;
  cursor: pointer;
}

.ex-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  min-height: 120px;
  padding: var(--space-4);
  color: var(--muted);
  font-size: var(--text-sm);
  text-align: center;
}

.ex-foot {
  flex: 0 0 auto;
  padding: var(--space-1) var(--space-3);
  border-top: 1px solid var(--border);
}

.ex-split {
  position: absolute;
  top: 0;
  right: -3px;
  bottom: 0;
  width: 6px;
  cursor: col-resize;
  touch-action: none;
  z-index: 1;
}

.ex-split:hover,
.ex-split:focus-visible {
  background: var(--accent-soft);
  outline: none;
}

.small {
  font-size: var(--text-xs);
}
</style>
