<script setup lang="ts">
import {
  Archive,
  ArchiveRestore,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Copy,
  Download,
  Folder,
  HardDrive,
  LayoutGrid,
  List,
  Lock,
  MoreVertical,
  Pencil,
  Plus,
  RefreshCw,
  Scissors,
  Search,
  Trash2,
  Upload,
  X,
} from "@lucide/vue";
import {
  NButton,
  NCard,
  NCheckbox,
  NDropdown,
  NInput,
  NSelect,
  useMessage,
} from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import ConflictDialog from "@/features/files/components/ConflictDialog.vue";
import CompressDialog from "@/features/files/components/CompressDialog.vue";
import CreateEntryDialog from "@/features/files/components/CreateEntryDialog.vue";
import DeleteEntriesDialog from "@/features/files/components/DeleteEntriesDialog.vue";
import ExtractDialog from "@/features/files/components/ExtractDialog.vue";
import FileTypeIcon from "@/features/files/components/FileTypeIcon.vue";
import PermissionsDialog from "@/features/files/components/PermissionsDialog.vue";
import RenameEntryDialog from "@/features/files/components/RenameEntryDialog.vue";
import UploadDialog from "@/features/files/components/UploadDialog.vue";
import { useFileDownload } from "@/features/files/composables/useFileDownload";
import type { FilesEntry } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import type { FilesTab } from "@/features/files/stores/files";
import type { NameKind, TransferProblem } from "@/features/files/utils/files";
import {
  entryIconKind,
  entryTypeLabel,
  filesFromDrop,
  formatMode,
  formatOctalModeFull,
  formatTimestamp,
  isExtractableArchive,
  isFileDrag,
  isSelectable,
  joinPath,
} from "@/features/files/utils/files";
import {
  folderMenuOptions,
  rowMenuOptions,
} from "@/features/files/utils/filesMenu";
import type { FilesMenuItem } from "@/features/files/utils/filesMenu";
import { useTeamsStore } from "@/features/teams";
import { formatBytes } from "@/shared/utils/format";

interface Props {
  serverId: string;
}

const props = defineProps<Props>();

const { t, locale } = useI18n();
const message = useMessage();
const files = useFilesStore();
const editor = useEditorStore();
const teams = useTeamsStore();
const { download } = useFileDownload();

watch(
  () => props.serverId,
  (next) => void files.init(next),
);
// A team switch changes the caller's role and the server's visibility, so the
// listing must reload rather than keep the previous team's directory.
watch(
  () => teams.activeTeamId,
  () => void files.reload(),
);
onMounted(() => void files.init(props.serverId));

// ── Dialog state ──────────────────────────────────────────────────────────
const createOpen = ref(false);
const createKind = ref<NameKind>("directory");
const renameOpen = ref(false);
const renameTarget = ref<FilesEntry | null>(null);
const permissionsOpen = ref(false);
const permissionsTarget = ref<FilesEntry | null>(null);
const deleteOpen = ref(false);
const uploadOpen = ref(false);
const droppedFiles = ref<File[]>([]);
const dragActive = ref(false);
const compressOpen = ref(false);
const compressTargets = ref<FilesEntry[]>([]);
const extractOpen = ref(false);
const extractTarget = ref<FilesEntry | null>(null);
/** actionTargets are the entries the delete/transfer dialogs act on. */
const actionTargets = ref<FilesEntry[]>([]);

/** filterModel binds the NInput to the store's client-side filter. */
const filterModel = computed<string>({
  get: () => files.query,
  set: (value) => files.setQuery(value ?? ""),
});

/** rootOptions labels each root with its writable/read-only access. */
const rootOptions = computed(() =>
  files.roots.map((root) => ({
    value: root.name,
    label: `${root.name} · ${t(root.writable ? "files.browser.writable" : "files.browser.readOnly")}`,
  })),
);

/** selectedRoot binds NSelect to the store's root selection. */
const selectedRoot = computed<string>({
  get: () => files.rootName,
  set: (value) => void files.setRoot(value ?? ""),
});

/** hasSelectedFiles gates the bulk download action. */
const hasSelectedFiles = computed<boolean>(() =>
  files.selectedEntries.some((entry) => entry.kind === "file"),
);

/** selection is the current selection reduced to writable-safe entries. */
const selection = computed<FilesEntry[]>(() =>
  files.selectedEntries.filter(isSelectable),
);

/** footSummary is the "12 entries · 4 folders · 6 files…" footer line. */
const footSummary = computed<string>(() => {
  const parts = [t("files.browser.entryCount", { count: files.entries.length })];
  const counts = files.counts;
  if (counts.directory > 0) {
    parts.push(t("files.browser.folderCount", { count: counts.directory }));
  }
  if (counts.file > 0) {
    parts.push(t("files.browser.fileCount", { count: counts.file }));
  }
  if (counts.symlink > 0) {
    parts.push(t("files.browser.symlinkCount", { count: counts.symlink }));
  }
  if (counts.special > 0) {
    parts.push(t("files.browser.specialCount", { count: counts.special }));
  }
  return parts.join(" · ");
});

/** currentPath is the "root/relative/path" the footer renders. */
const currentPath = computed<string>(() =>
  files.path === "" ? files.rootName : `${files.rootName}/${files.path}`,
);

/** pasteProblemMessage explains why the clipboard cannot land at one destination. */
function pasteProblemMessage(
  problem: "cross-root" | TransferProblem | null,
): string {
  switch (problem) {
    case "cross-root":
      return String(t("files.dialogs.pasteCrossRoot"));
    case "same-folder":
      return String(t("files.dialogs.transferSameFolder"));
    case "into-itself":
      return String(t("files.dialogs.transferIntoItself"));
    default:
      return "";
  }
}

/** pasteProblemText explains why the clipboard cannot be pasted here. */
const pasteProblemText = computed<string>(() =>
  pasteProblemMessage(files.pasteProblem),
);

/** typeLabel resolves the Type column through the active locale. */
function typeLabel(entry: FilesEntry): string {
  return entryTypeLabel(entry, locale.value);
}

/** iconKind resolves the marker icon family for one entry. */
function iconKind(entry: FilesEntry): ReturnType<typeof entryIconKind> {
  return entryIconKind(entry);
}

/** entrySubtitle is the second line under the name (type + symlink target). */
function entrySubtitle(entry: FilesEntry): string {
  if (entry.kind === "symlink") {
    return entry.symlink_target
      ? `${typeLabel(entry)} → ${entry.symlink_target}`
      : typeLabel(entry);
  }
  return typeLabel(entry);
}

/** ariaSort reflects the store's sort for one column header. */
function ariaSort(key: string): "ascending" | "descending" | "none" {
  if (files.sortKey !== key) {
    return "none";
  }
  return files.sortDir === "asc" ? "ascending" : "descending";
}

/** activate opens a directory, or a file in the code editor. */
function activate(entry: FilesEntry): void {
  if (entry.kind === "directory") {
    void files.openDirectory(entry.name);
    return;
  }
  openInEditor(entry);
}

/** openInEditor opens one text file in the editor (owner/admin only). */
function openInEditor(entry: FilesEntry): void {
  if (entry.kind !== "file" || !files.canReadContent) {
    return;
  }
  void editor.openFile(
    props.serverId,
    teams.activeTeamId,
    files.rootName,
    joinPath(files.path, entry.name),
  );
}

// ── Tabs (JUS-123) ────────────────────────────────────────────────────────
// The tab bar lists every open directory tab. Each tab owns its path, root,
// selection, sort/filter and scroll offset (the store keeps them per tab); the
// clipboard stays shared, so a cut in one tab pastes in another.
/** tabListEl is the role=tablist strip, refocused after an arrow-key move. */
const tabListEl = ref<HTMLElement | null>(null);
/** scrollEl is the mounted entry-list scroll container (list or grid). */
const scrollEl = ref<HTMLElement | null>(null);

/** tabLabel is the visible tab name: the folder's basename, or the root. */
function tabLabel(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : (tab.path.split("/").pop() ?? tab.rootName);
}

/** tabTitle is the tab tooltip / accessible name: the full root-relative path. */
function tabTitle(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : `${tab.rootName}/${tab.path}`;
}

/** openNewTab opens a tab at the active root's top level. */
function openNewTab(): void {
  void files.openTab();
}

/** focusActiveTab moves DOM focus onto the active tab after a keyboard move. */
async function focusActiveTab(): Promise<void> {
  await nextTick();
  tabListEl.value
    ?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
    ?.focus();
}

/** neighbourTabId returns the tab `delta` places from `id`, wrapping around. */
function neighbourTabId(id: string, delta: number): string {
  const count = files.tabs.length;
  const index = files.tabs.findIndex((tab) => tab.id === id);
  return files.tabs[(index + delta + count) % count]?.id ?? id;
}

/** onTabKeydown drives the roving-tabindex tablist (arrows, Home/End, Delete). */
function onTabKeydown(event: KeyboardEvent, tab: FilesTab): void {
  switch (event.key) {
    case "ArrowRight":
    case "ArrowDown":
      event.preventDefault();
      void files.switchTab(neighbourTabId(tab.id, 1)).then(focusActiveTab);
      break;
    case "ArrowLeft":
    case "ArrowUp":
      event.preventDefault();
      void files.switchTab(neighbourTabId(tab.id, -1)).then(focusActiveTab);
      break;
    case "Home":
      event.preventDefault();
      void files.switchTab(files.tabs[0].id).then(focusActiveTab);
      break;
    case "End":
      event.preventDefault();
      void files
        .switchTab(files.tabs[files.tabs.length - 1].id)
        .then(focusActiveTab);
      break;
    case "Delete":
      event.preventDefault();
      void files.closeTab(tab.id).then(focusActiveTab);
      break;
  }
}

/** onTabAuxClick closes a tab on a middle-button click. */
function onTabAuxClick(event: MouseEvent, id: string): void {
  if (event.button !== 1) {
    return;
  }
  event.preventDefault();
  void files.closeTab(id);
}

/** onListScroll records the active tab's entry-list scroll offset. */
function onListScroll(event: Event): void {
  files.setScrollTop((event.currentTarget as HTMLElement).scrollTop);
}

// Restore a tab's scroll offset after a switch and follow a navigation back to
// the top (the store zeroes scrollTop on navigation, so the DOM replays it).
watch(
  () => [files.activeTabId, files.path] as const,
  async () => {
    await nextTick();
    if (scrollEl.value) {
      scrollEl.value.scrollTop = files.scrollTop;
    }
  },
);

/**
 * blockedByDirty refuses a rename/move/delete of entries with unsaved editor
 * tabs, so the user's edits are never dropped by a file operation. `dir`
 * defaults to the directory on screen; a paste passes the clipboard's source
 * directory.
 */
function blockedByDirty(names: string[], dir = files.path): boolean {
  const blocked = editor.blockingNames(files.rootName, dir, names);
  if (blocked.length === 0) {
    return false;
  }
  message.warning(
    String(t("files.editor.dirtyBlocked", { names: blocked.join(", ") })),
  );
  return true;
}

/** startDownload mints a ticket and hands the file to the browser. */
async function startDownload(entry: FilesEntry): Promise<void> {
  if (!files.canReadContent) {
    return;
  }
  await download(
    props.serverId,
    teams.activeTeamId,
    files.rootName,
    files.path,
    entry,
  );
}

/** downloadSelected downloads every selected file, one ticket each. */
async function downloadSelected(): Promise<void> {
  for (const entry of files.selectedEntries) {
    if (entry.kind !== "file") {
      continue;
    }
    await download(
      props.serverId,
      teams.activeTeamId,
      files.rootName,
      files.path,
      entry,
    );
  }
}

// ── Dialog openers ────────────────────────────────────────────────────────
function openCreate(kind: NameKind): void {
  createKind.value = kind;
  createOpen.value = true;
}

/** newMenuOpen tracks the New dropdown disclosure state (aria-expanded). */
const newMenuOpen = ref(false);

/** newMenuOptions groups New file / New folder under one toolbar dropdown. */
const newMenuOptions = computed(() =>
  [
    { label: String(t("files.actions.newFile")), key: "file" },
    { label: String(t("files.actions.newFolder")), key: "directory" },
  ].map((option) => ({ ...option, props: { role: "menuitem" } })),
);

/** onNewMenu maps the New dropdown's key back to a create kind. */
function onNewMenu(key: string): void {
  openCreate(key === "directory" ? "directory" : "file");
}

function openRename(entry: FilesEntry): void {
  if (blockedByDirty([entry.name])) {
    return;
  }
  renameTarget.value = entry;
  renameOpen.value = true;
}

/** openPermissions opens the chmod dialog for one file or folder. */
function openPermissions(entry: FilesEntry): void {
  permissionsTarget.value = entry;
  permissionsOpen.value = true;
}

function openDelete(entries: FilesEntry[]): void {
  if (blockedByDirty(entries.map((entry) => entry.name))) {
    return;
  }
  actionTargets.value = entries;
  deleteOpen.value = true;
}

function openUpload(dropped: File[] = []): void {
  droppedFiles.value = dropped;
  uploadOpen.value = true;
}

/** openCompress builds an archive from the current selection. */
function openCompress(entries: FilesEntry[]): void {
  if (entries.length === 0) {
    return;
  }
  compressTargets.value = entries;
  compressOpen.value = true;
}

/** openExtract unpacks one archive into the current directory. */
function openExtract(entry: FilesEntry): void {
  if (!isExtractableArchive(entry)) {
    return;
  }
  extractTarget.value = entry;
  extractOpen.value = true;
}

/** onDragOver highlights the zone only for a file drag on a writable root. */
function onDragOver(event: DragEvent): void {
  if (!isFileDrag(event.dataTransfer?.types)) {
    return;
  }
  // A file drag must preventDefault to allow the drop, even when the root is
  // read-only (otherwise the browser opens the dropped file).
  event.preventDefault();
  if (files.canWrite) {
    dragActive.value = true;
  }
}

function onDragLeave(): void {
  dragActive.value = false;
}

/**
 * onDrop opens the upload dialog queued with the dropped files. A drop is only
 * honoured on a writable root, a non-file drag is ignored, and a dropped
 * directory is rejected with a clear message instead of uploaded as an empty
 * file.
 */
function onDrop(event: DragEvent): void {
  dragActive.value = false;
  if (!isFileDrag(event.dataTransfer?.types)) {
    return;
  }
  event.preventDefault();
  if (!files.canWrite) {
    message.warning(String(t("files.dialogs.dropReadOnly")));
    return;
  }
  const { files: dropped, folders } = filesFromDrop(event.dataTransfer);
  if (folders > 0) {
    message.warning(String(t("files.dialogs.foldersUnsupported")));
  }
  if (dropped.length > 0) {
    openUpload(dropped);
  }
}

/**
 * pasteAction applies the clipboard into `destination` (the current directory
 * by default), with feedback. A folder-row Paste passes that folder's path, so
 * right-clicking a folder pastes into it rather than into the folder on screen.
 */
async function pasteAction(destination = files.path): Promise<void> {
  const problem = files.pasteProblemAt(destination);
  if (problem !== null) {
    message.warning(pasteProblemMessage(problem));
    return;
  }
  const clip = files.clipboard;
  if (clip && clip.mode === "cut" && blockedByDirty(clip.names, clip.path)) {
    return;
  }
  const mode = files.clipboardMode;
  const summary = await files.pasteInto(destination);
  if (summary.failed.length > 0) {
    message.warning(
      String(
        t("files.dialogs.transferPartial", {
          failed: summary.failed.length,
          error: summary.failed[0]?.error ?? "",
        }),
      ),
    );
    return;
  }
  if (summary.done.length > 0) {
    message.success(
      String(
        t(mode === "cut" ? "files.dialogs.moved" : "files.dialogs.copied", {
          count: summary.done.length,
        }),
      ),
    );
  }
}

/** menuContext is the gate set both menus read, rebuilt as the gates change. */
const menuContext = computed(() => ({
  canWrite: files.canWrite,
  canReadContent: files.canReadContent,
  canPaste: files.hasClipboard && files.pasteProblem === null,
  t: (key: string) => String(t(key)),
}));

/**
 * rowMenu builds the per-row (⋮ and right-click) actions menu. Paste is offered
 * on a folder row only, and pastes INTO that folder, so its validity is checked
 * against the folder rather than the directory on screen.
 */
function rowMenu(entry: FilesEntry): FilesMenuItem[] {
  const base = menuContext.value;
  const canPaste =
    entry.kind === "directory" &&
    files.hasClipboard &&
    files.pasteProblemAt(joinPath(files.path, entry.name)) === null;
  return rowMenuOptions(entry, { ...base, canPaste });
}

/** onRowMenu dispatches one row action on the row or the whole selection. */
function onRowMenu(key: string, entry: FilesEntry): void {
  switch (key) {
    case "open":
      void files.openDirectory(entry.name);
      break;
    case "open-tab":
      // Open the folder in a new tab, leaving this one where it is.
      void files.openTab({ root: files.rootName, path: joinPath(files.path, entry.name) });
      break;
    case "edit":
      openInEditor(entry);
      break;
    case "download":
      void startDownload(entry);
      break;
    case "rename":
      openRename(entry);
      break;
    case "permissions":
      openPermissions(entry);
      break;
    case "copy":
      files.copySelection(files.targetsForRow(entry).map((item) => item.name));
      break;
    case "cut":
      files.cutSelection(files.targetsForRow(entry).map((item) => item.name));
      break;
    case "paste":
      void pasteAction(joinPath(files.path, entry.name));
      break;
    case "extract":
      openExtract(entry);
      break;
    case "delete":
      openDelete(files.targetsForRow(entry));
      break;
  }
}

// ── Context menu (JUS-124) ────────────────────────────────────────────────
// One controlled NDropdown serves the row right-click, the empty-space
// right-click and the Shift+F10 / menu key on every row. The ⋮ button keeps its
// own click dropdown; both reuse the same builders.
const contextOpen = ref(false);
const contextX = ref(0);
const contextY = ref(0);
/** contextEntry is the row the menu acts on; null means the folder itself. */
const contextEntry = ref<FilesEntry | null>(null);

/** contextOptions is the row menu when a row is targeted, else the folder menu. */
const contextOptions = computed<FilesMenuItem[]>(() =>
  contextEntry.value
    ? rowMenu(contextEntry.value)
    : folderMenuOptions(menuContext.value),
);

/** contextAria names the menu for the row it opened on, or the folder. */
const contextAria = computed<string>(() =>
  contextEntry.value
    ? String(t("files.browser.moreActions", { name: contextEntry.value.name }))
    : String(t("files.browser.folderActions")),
);

/** openRowContext opens the row menu at the pointer and applies the selection rule. */
function openRowContext(event: MouseEvent, entry: FilesEntry): void {
  // A special/unspecified row has no actions, so suppress the empty menu.
  if (rowMenu(entry).length === 0) {
    return;
  }
  files.selectForRowContext(entry);
  contextEntry.value = entry;
  contextX.value = event.clientX;
  contextY.value = event.clientY;
  contextOpen.value = true;
}

/** openFolderContext opens the folder-level menu at the pointer. */
function openFolderContext(event: MouseEvent): void {
  contextEntry.value = null;
  contextX.value = event.clientX;
  contextY.value = event.clientY;
  contextOpen.value = true;
}

/** onRowKeydown opens the row menu on Shift+F10 or the dedicated menu key. */
function onRowKeydown(event: KeyboardEvent, entry: FilesEntry): void {
  const isMenuKey =
    event.key === "ContextMenu" || (event.shiftKey && event.key === "F10");
  if (!isMenuKey || rowMenu(entry).length === 0) {
    return;
  }
  event.preventDefault();
  files.selectForRowContext(entry);
  contextEntry.value = entry;
  const box = (event.currentTarget as HTMLElement).getBoundingClientRect();
  contextX.value = box.left + 16;
  contextY.value = box.top + box.height / 2;
  contextOpen.value = true;
}

/**
 * onDocumentPointerDown closes the context menu on a press outside it. A
 * manual-trigger popover never auto-closes and naive's clickoutside misfires on
 * the menu's own items, so the listener is owned here: a press inside the
 * dropdown is ignored, anything else closes the menu.
 */
function onDocumentPointerDown(event: PointerEvent): void {
  if (!contextOpen.value) {
    return;
  }
  const target = event.target as HTMLElement | null;
  if (target && target.closest(".n-dropdown")) {
    return;
  }
  contextOpen.value = false;
}

onMounted(() =>
  document.addEventListener("pointerdown", onDocumentPointerDown, true),
);
onBeforeUnmount(() =>
  document.removeEventListener("pointerdown", onDocumentPointerDown, true),
);

/** onContextSelect dispatches a context-menu action and closes the menu. */
function onContextSelect(key: string): void {
  const entry = contextEntry.value;
  contextOpen.value = false;
  if (entry) {
    onRowMenu(key, entry);
    return;
  }
  onFolderMenu(key);
}

/** onFolderMenu dispatches a folder-level action. */
function onFolderMenu(key: string): void {
  switch (key) {
    case "new-file":
      openCreate("file");
      break;
    case "new-folder":
      openCreate("directory");
      break;
    case "paste":
      void pasteAction();
      break;
    case "upload":
      openUpload();
      break;
    case "refresh":
      void files.refresh();
      break;
  }
}
</script>

<template>
  <NCard
    class="files-browser"
    :class="{ 'is-dragging': dragActive }"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
  >
    <template #header>
      <div class="fm-card-head">
        <h3 class="fm-card-title">{{ $t("files.browser.heading") }}</h3>
        <div class="fm-card-tools">
          <span class="small muted">{{ $t("files.browser.rootLabel") }}</span>
          <NSelect
            v-model:value="selectedRoot"
            class="fm-root-select"
            size="small"
            :options="rootOptions"
            :aria-label="$t('files.browser.rootLabel')"
          />
          <span class="segmented" role="group">
            <button
              type="button"
              class="seg-btn"
              :class="{ 'is-active': files.viewMode === 'list' }"
              :aria-label="$t('files.browser.viewList')"
              :title="$t('files.browser.viewList')"
              :aria-pressed="files.viewMode === 'list'"
              @click="files.setViewMode('list')"
            >
              <List :size="15" :stroke-width="1.8" aria-hidden="true" />
            </button>
            <button
              type="button"
              class="seg-btn"
              :class="{ 'is-active': files.viewMode === 'grid' }"
              :aria-label="$t('files.browser.viewGrid')"
              :title="$t('files.browser.viewGrid')"
              :aria-pressed="files.viewMode === 'grid'"
              @click="files.setViewMode('grid')"
            >
              <LayoutGrid :size="15" :stroke-width="1.8" aria-hidden="true" />
            </button>
          </span>
        </div>
      </div>
    </template>

    <!-- Tab bar (JUS-123): one tab per open directory. A middle-click or the
         × closes a tab; the trailing + opens a new one at the root top. -->
    <div class="fm-tabs">
      <div
        ref="tabListEl"
        class="fm-tabstrip"
        role="tablist"
        :aria-label="$t('files.tabs.list')"
      >
        <div
          v-for="tab in files.tabs"
          :key="tab.id"
          class="fm-tab"
          :class="{ 'is-active': tab.id === files.activeTabId }"
        >
          <button
            type="button"
            class="fm-tab-label"
            role="tab"
            :aria-selected="tab.id === files.activeTabId"
            :tabindex="tab.id === files.activeTabId ? 0 : -1"
            :title="tabTitle(tab)"
            :aria-label="tabTitle(tab)"
            @click="files.switchTab(tab.id)"
            @keydown="onTabKeydown($event, tab)"
            @auxclick="onTabAuxClick($event, tab.id)"
          >
            <span class="fm-tab-name">{{ tabLabel(tab) }}</span>
          </button>
          <button
            v-if="files.tabs.length > 1"
            type="button"
            class="fm-tab-close"
            :aria-label="$t('files.tabs.close', { name: tabLabel(tab) })"
            :title="$t('files.tabs.close', { name: tabLabel(tab) })"
            @click.stop="void files.closeTab(tab.id)"
            @auxclick="onTabAuxClick($event, tab.id)"
          >
            <X :size="12" :stroke-width="2" aria-hidden="true" />
          </button>
        </div>
      </div>
      <button
        type="button"
        class="fm-tab-add"
        :disabled="files.rootName === ''"
        :aria-label="$t('files.tabs.new')"
        :title="$t('files.tabs.new')"
        @click="openNewTab()"
      >
        <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
      </button>
    </div>

    <div v-if="files.canWrite" class="fm-toolbar">
      <div class="fm-toolbar-group">
        <NDropdown
          v-model:show="newMenuOpen"
          trigger="click"
          placement="bottom-start"
          :options="newMenuOptions"
          :menu-props="() => ({
            role: 'menu',
            'aria-label': $t('files.actions.new'),
          })"
          @select="onNewMenu"
        >
          <NButton
            size="small"
            secondary
            aria-haspopup="menu"
            :aria-expanded="newMenuOpen"
            :disabled="files.acting"
            :title="$t('files.actions.new')"
            :aria-label="$t('files.actions.new')"
          >
            <template #icon>
              <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            <span class="fm-btn-label">{{ $t("files.actions.new") }}</span>
            <ChevronDown class="fm-caret" :size="12" :stroke-width="1.8" aria-hidden="true" />
          </NButton>
        </NDropdown>
        <NButton
          size="small"
          type="primary"
          :disabled="files.acting"
          :title="$t('files.actions.upload')"
          :aria-label="$t('files.actions.upload')"
          @click="openUpload()"
        >
          <template #icon>
            <Upload :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          <span class="fm-btn-label">{{ $t("files.actions.upload") }}</span>
        </NButton>
      </div>

      <div class="fm-toolbar-group fm-toolbar-end">
        <NButton
          v-if="files.hasClipboard"
          size="small"
          secondary
          :disabled="files.acting || files.pasteProblem !== null"
          :title="pasteProblemText || $t('files.actions.paste')"
          :aria-label="pasteProblemText || $t('files.actions.paste')"
          @click="() => void pasteAction()"
        >
          <template #icon>
            <Copy :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          <span class="fm-btn-label">
            {{ $t("files.actions.paste") }} ({{ files.clipboard?.names.length ?? 0 }})
          </span>
        </NButton>
        <span v-if="files.hasClipboard" class="small muted clipboard-hint">
          {{ files.clipboardMode === "cut" ? $t("files.clipboard.cut") : $t("files.clipboard.copy") }}
        </span>
        <NButton
          size="small"
          secondary
          :disabled="files.acting || selection.length === 0"
          :title="selection.length === 0 ? $t('files.dialogs.compressSelect') : $t('files.actions.compress')"
          :aria-label="selection.length === 0 ? $t('files.dialogs.compressSelect') : $t('files.actions.compress')"
          @click="openCompress(selection)"
        >
          <template #icon>
            <Archive :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          <span class="fm-btn-label">{{ $t("files.actions.compress") }}</span>
        </NButton>
      </div>
    </div>

    <div class="fm-bar">
      <nav class="fm-path" :aria-label="$t('files.browser.parent')">
        <button
          type="button"
          class="fm-crumb fm-up"
          :disabled="files.path === ''"
          :aria-label="$t('files.browser.parent')"
          :title="$t('files.browser.parent')"
          @click="files.goUp()"
        >
          <ArrowUp :size="14" :stroke-width="1.8" aria-hidden="true" />
        </button>
        <template v-for="(crumb, index) in files.breadcrumb" :key="crumb.path">
          <span v-if="index > 0" class="sep" aria-hidden="true">/</span>
          <button
            type="button"
            class="fm-crumb"
            :class="{ 'is-current': index === files.breadcrumb.length - 1 }"
            :aria-current="index === files.breadcrumb.length - 1 ? 'page' : undefined"
            @click="files.goToPath(crumb.path)"
          >
            <HardDrive v-if="crumb.isRoot" :size="14" :stroke-width="1.8" aria-hidden="true" />
            <span>{{ crumb.name }}</span>
          </button>
        </template>
      </nav>

      <NInput
        v-model:value="filterModel"
        class="fm-search"
        size="small"
        clearable
        :placeholder="$t('files.browser.filter')"
        :aria-label="$t('files.browser.filter')"
      >
        <template #prefix>
          <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NInput>

      <NButton size="small" secondary :loading="files.loading" @click="files.refresh()">
        <template #icon>
          <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ $t("files.browser.refresh") }}
      </NButton>
    </div>

    <div
      v-if="files.selectedNames.length > 0"
      class="fm-bulk"
      role="region"
      :aria-label="$t('files.browser.selectionCount', { count: files.selectedNames.length })"
    >
      <span class="bold">{{ $t("files.browser.selectionCount", { count: files.selectedNames.length }) }}</span>
      <span class="muted">{{ formatBytes(files.selectedBytes) }}</span>
      <span class="fm-bulk-actions">
        <NButton
          size="small"
          secondary
          :disabled="!files.canReadContent || !hasSelectedFiles"
          :title="files.canReadContent ? '' : $t('files.browser.fileNeedsRole')"
          @click="downloadSelected()"
        >
          <template #icon>
            <Download :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ $t("files.browser.downloadSelected") }}
        </NButton>
        <template v-if="files.canWrite">
          <NButton size="small" secondary :disabled="files.acting" @click="files.copySelection(selection.map((item) => item.name))">
            <template #icon>
              <Copy :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ $t("files.actions.copy") }}
          </NButton>
          <NButton size="small" secondary :disabled="files.acting" @click="files.cutSelection(selection.map((item) => item.name))">
            <template #icon>
              <Scissors :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ $t("files.actions.cut") }}
          </NButton>
          <NButton size="small" type="error" secondary :disabled="files.acting" @click="openDelete(selection)">
            <template #icon>
              <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ $t("common.actions.delete") }}
          </NButton>
        </template>
        <NButton size="small" quaternary @click="files.clearSelection()">
          {{ $t("files.browser.clear") }}
        </NButton>
      </span>
    </div>

    <div v-if="files.disabled" class="empty fm-state" @contextmenu.prevent="openFolderContext">
      <Lock :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4>{{ $t("files.browser.disabledTitle") }}</h4>
      <p class="small">{{ $t("files.browser.disabledHint") }}</p>
      <NButton size="small" secondary class="fm-state-action" @click="files.retry()">
        {{ $t("common.actions.retry") }}
      </NButton>
    </div>

    <div v-else-if="files.loading && files.entries.length === 0" class="empty fm-state" @contextmenu.prevent="openFolderContext">
      <span class="fm-spinner" aria-hidden="true"></span>
      <h4>{{ $t("files.browser.loading") }}</h4>
    </div>

    <div v-else-if="files.permissionDenied" class="empty fm-state" @contextmenu.prevent="openFolderContext">
      <Lock :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4>{{ $t("files.browser.permissionTitle") }}</h4>
      <p class="small">{{ $t("files.browser.permissionHint") }}</p>
      <NButton size="small" secondary class="fm-state-action" @click="files.retry()">
        {{ $t("common.actions.retry") }}
      </NButton>
    </div>

    <div v-else-if="files.error && files.entries.length === 0" class="empty fm-state" @contextmenu.prevent="openFolderContext">
      <CircleAlert class="fm-danger" :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4 class="fm-danger">{{ $t("files.browser.errorTitle") }}</h4>
      <p class="small">{{ files.error }}</p>
      <NButton size="small" secondary class="fm-state-action" @click="files.retry()">
        {{ $t("common.actions.retry") }}
      </NButton>
    </div>

    <div v-else-if="files.loaded && files.visibleEntries.length === 0" class="empty fm-state" @contextmenu.prevent="openFolderContext">
      <Folder :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4>{{ $t("files.browser.empty") }}</h4>
      <p class="small">{{ $t("files.browser.emptyHint") }}</p>
    </div>

    <template v-else>
      <div v-if="files.error" class="callout fm-inline-error" role="status">
        <CircleAlert class="fm-danger" :size="18" :stroke-width="1.8" aria-hidden="true" />
        <div class="grow">
          <p class="small">{{ files.error }}</p>
        </div>
        <NButton size="tiny" secondary @click="files.retry()">
          {{ $t("common.actions.retry") }}
        </NButton>
      </div>

      <div v-if="files.loaded && !files.writable && files.rootName !== ''" class="callout fm-readonly">
        <Lock :size="18" :stroke-width="1.8" aria-hidden="true" />
        <div>
          <h4>{{ $t("files.browser.readOnlyTitle") }}</h4>
          <p class="small">{{ $t("files.browser.readOnlyHint") }}</p>
        </div>
      </div>

      <div v-if="files.viewMode === 'list'" ref="scrollEl" class="table-wrap" @scroll="onListScroll" @contextmenu.prevent="openFolderContext">
        <div class="fm-table" role="table">
          <div class="fm-row fm-head" role="row">
            <span class="fm-select" role="columnheader">
              <NCheckbox
                size="small"
                :checked="files.allVisibleSelected"
                :indeterminate="files.someVisibleSelected"
                :aria-label="$t('files.browser.selectAll')"
                @update:checked="files.toggleSelectAll()"
              />
            </span>
            <div class="fm-name" role="columnheader" :aria-sort="ariaSort('name')">
              <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'name' }" @click="files.setSort('name')">
                {{ $t("files.browser.columnName") }}
                <ChevronDown v-if="files.sortKey === 'name'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </div>
            <div role="columnheader" :aria-sort="ariaSort('kind')">
              <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'kind' }" @click="files.setSort('kind')">
                {{ $t("files.browser.columnType") }}
                <ChevronDown v-if="files.sortKey === 'kind'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </div>
            <div role="columnheader" class="fm-cell--size" :aria-sort="ariaSort('size')">
              <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'size' }" @click="files.setSort('size')">
                {{ $t("files.browser.columnSize") }}
                <ChevronDown v-if="files.sortKey === 'size'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </div>
            <div role="columnheader" :aria-sort="ariaSort('modified')">
              <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'modified' }" @click="files.setSort('modified')">
                {{ $t("files.browser.columnModified") }}
                <ChevronDown v-if="files.sortKey === 'modified'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </div>
            <div role="columnheader" :aria-sort="ariaSort('mode')">
              <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'mode' }" @click="files.setSort('mode')">
                {{ $t("files.browser.columnMode") }}
                <ChevronDown v-if="files.sortKey === 'mode'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </div>
            <div role="columnheader" class="right">{{ $t("files.browser.columnActions") }}</div>
          </div>

          <div
            v-for="entry in files.visibleEntries"
            :key="entry.name"
            class="fm-row"
            :class="{ 'is-selected': files.isSelected(entry.name) }"
            role="row"
            @contextmenu.stop.prevent="openRowContext($event, entry)"
            @keydown="onRowKeydown($event, entry)"
          >
            <span class="fm-select" role="cell">
              <NCheckbox
                v-if="isSelectable(entry)"
                size="small"
                :checked="files.isSelected(entry.name)"
                :aria-label="$t('files.browser.selectEntry', { name: entry.name })"
                @update:checked="files.toggleSelection(entry.name)"
              />
            </span>
            <div class="fm-name" role="cell">
              <FileTypeIcon :kind="iconKind(entry)" />
              <button type="button" class="fm-label" @click="activate(entry)">
                <b>{{ entry.name }}</b>
                <span>{{ entrySubtitle(entry) }}</span>
              </button>
            </div>
            <div class="fm-cell" role="cell">{{ typeLabel(entry) }}</div>
            <div class="fm-cell fm-cell--size" role="cell">{{ entry.kind === "directory" ? "—" : formatBytes(entry.size) }}</div>
            <div class="fm-cell" role="cell">{{ formatTimestamp(entry.modified_at) }}</div>
            <div class="fm-cell fm-mode" role="cell">
              <span>{{ formatMode(entry.mode) }}</span>
              <span class="fm-mode-octal">{{ formatOctalModeFull(entry.mode) }}</span>
            </div>
            <div class="fm-actions" role="cell">
              <NButton
                v-if="entry.kind === 'directory'"
                size="tiny"
                quaternary
                :aria-label="$t('files.browser.openFolder', { name: entry.name })"
                @click="files.openDirectory(entry.name)"
              >
                <template #icon>
                  <ChevronRight :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <NButton
                v-else-if="entry.kind === 'file'"
                size="tiny"
                quaternary
                :disabled="!files.canReadContent"
                :title="files.canReadContent ? $t('files.browser.downloadFile', { name: entry.name }) : $t('files.browser.fileNeedsRole')"
                :aria-label="$t('files.browser.downloadFile', { name: entry.name })"
                @click="startDownload(entry)"
              >
                <template #icon>
                  <Download :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <NButton
                v-if="files.canWrite && isExtractableArchive(entry)"
                size="tiny"
                quaternary
                :title="$t('files.actions.extract')"
                :aria-label="$t('files.actions.extract')"
                @click="openExtract(entry)"
              >
                <template #icon>
                  <ArchiveRestore :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <NButton
                v-if="files.canWrite && isSelectable(entry)"
                size="tiny"
                quaternary
                :aria-label="$t('files.actions.rename')"
                @click="openRename(entry)"
              >
                <template #icon>
                  <Pencil :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <NButton
                v-if="files.canWrite && isSelectable(entry)"
                size="tiny"
                quaternary
                :aria-label="$t('files.browser.deleteEntry', { name: entry.name })"
                @click="openDelete([entry])"
              >
                <template #icon>
                  <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <NDropdown
                trigger="click"
                placement="bottom-end"
                :options="rowMenu(entry)"
                :menu-props="() => ({
                  role: 'menu',
                  'aria-label': $t('files.browser.moreActions', { name: entry.name }),
                })"
                @select="(key: string) => onRowMenu(key, entry)"
              >
                <NButton
                  size="tiny"
                  quaternary
                  :aria-label="$t('files.browser.moreActions', { name: entry.name })"
                >
                  <template #icon>
                    <MoreVertical :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </NDropdown>
            </div>
          </div>
        </div>
      </div>

      <div v-else ref="scrollEl" class="fm-grid" @scroll="onListScroll" @contextmenu.prevent="openFolderContext">
        <div
          v-for="entry in files.visibleEntries"
          :key="entry.name"
          class="fm-tile-wrap"
          @contextmenu.stop.prevent="openRowContext($event, entry)"
          @keydown="onRowKeydown($event, entry)"
        >
          <label
            v-if="isSelectable(entry)"
            class="fm-tile-check"
            :title="$t('files.browser.selectEntry', { name: entry.name })"
          >
            <NCheckbox
              size="small"
              :checked="files.isSelected(entry.name)"
              :aria-label="$t('files.browser.selectEntry', { name: entry.name })"
              @update:checked="files.toggleSelection(entry.name)"
            />
          </label>
          <button
            type="button"
            class="fm-tile"
            :class="{ 'is-selected': files.isSelected(entry.name) }"
            @click="activate(entry)"
          >
            <span class="fm-tile-top">
              <FileTypeIcon :kind="iconKind(entry)" />
            </span>
            <span class="fm-label">
              <b>{{ entry.name }}</b>
              <span>{{ typeLabel(entry) }}</span>
            </span>
            <span class="fm-meta">
              <template v-if="entry.kind !== 'directory'">{{ formatBytes(entry.size) }} · </template>{{ formatTimestamp(entry.modified_at) }}
            </span>
          </button>
        </div>
      </div>

      <div v-if="files.hasMore" class="fm-more">
        <NButton size="small" secondary :loading="files.loadingMore" @click="files.loadMore()">
          {{ files.loadingMore ? $t("files.browser.loadingMore") : $t("files.browser.loadMore") }}
        </NButton>
      </div>

      <div class="fm-foot">
        <span class="grow">{{ footSummary }}</span>
        <span v-if="files.rootName !== ''">{{ $t("files.browser.currentPath", { path: currentPath }) }}</span>
        <span>{{ $t("files.browser.freeSpace", { free: formatBytes(files.freeBytes) }) }}</span>
      </div>
    </template>

    <div v-if="dragActive" class="fm-drop-overlay" aria-hidden="true">
      <Upload :size="26" :stroke-width="1.6" />
      <span>{{ $t("files.dialogs.uploadDrop") }}</span>
    </div>

    <CreateEntryDialog v-model:show="createOpen" :kind="createKind" />
    <RenameEntryDialog v-model:show="renameOpen" :entry="renameTarget" />
    <PermissionsDialog v-model:show="permissionsOpen" :entry="permissionsTarget" />
    <DeleteEntriesDialog v-model:show="deleteOpen" :entries="actionTargets" />
    <UploadDialog v-model:show="uploadOpen" :initial-files="droppedFiles" />
    <CompressDialog v-model:show="compressOpen" :entries="compressTargets" />
    <ExtractDialog v-model:show="extractOpen" :entry="extractTarget" />
    <ConflictDialog />

    <!-- One controlled dropdown serves the row / empty-space context menu and
         the Shift+F10 / menu key; x/y place it at the pointer or the row. -->
    <NDropdown
      v-model:show="contextOpen"
      trigger="manual"
      placement="bottom-start"
      :x="contextX"
      :y="contextY"
      :options="contextOptions"
      :menu-props="() => ({ role: 'menu', 'aria-label': contextAria })"
      @select="onContextSelect"
    />
  </NCard>
</template>

<style scoped>
/* Ported from docs/design/assets/gotham-views.css (FILE MANAGER section) and
 * docs/design/files.html; only tokens from styles/tokens.css are used. */
.files-browser {
  position: relative;
  /* The panel is a flex column that fills whatever height its parent gives it
   * (JUS-118): toolbar/bar/bulk/footer stay fixed while the entry list is the
   * only scrolling region (`flex: 1` on .table-wrap / .fm-grid / .fm-state). */
  display: flex;
  flex-direction: column;
  min-height: 0;
  max-height: none;
}

.files-browser :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
}

.fm-card-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.fm-card-title {
  margin: 0;
  font-size: var(--text-lg);
  color: var(--fg-2);
}

.fm-card-tools {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.fm-root-select {
  width: 220px;
}

/* Tab bar (JUS-123): one tab per open directory. The strip scrolls
   horizontally at narrow widths instead of wrapping the layout; the active tab
   is filled, and each label ellipsises inside its own max width. */
.fm-tabs {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  margin-bottom: var(--space-3);
}

.fm-tabstrip {
  display: flex;
  align-items: stretch;
  gap: 2px;
  flex: 1 1 auto;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: thin;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 2px;
}

.fm-tab {
  display: flex;
  align-items: center;
  flex: 0 0 auto;
  max-width: 200px;
  border-radius: 3px;
}

.fm-tab.is-active {
  background: var(--selected-row);
}

.fm-tab-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  border: 0;
  background: none;
  padding: 5px 8px;
  border-radius: 3px;
  color: var(--muted);
  font-size: var(--text-xs);
  cursor: pointer;
}

.fm-tab.is-active .fm-tab-label {
  color: var(--fg-2);
  font-weight: 600;
}

.fm-tab-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-tab-close {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  margin-right: 2px;
  border: 0;
  background: none;
  border-radius: 3px;
  color: var(--muted);
  cursor: pointer;
}

.fm-tab-close:hover {
  background: var(--hover-row);
  color: var(--fg-2);
}

.fm-tab-add {
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border: 1px solid var(--border);
  background: var(--surface-warm);
  border-radius: var(--radius-sm);
  color: var(--muted);
  cursor: pointer;
}

.fm-tab-add:hover:not(:disabled) {
  color: var(--fg-2);
  border-color: var(--accent);
}

.fm-tab-add:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.fm-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  flex-wrap: wrap;
  margin-bottom: var(--space-3);
}

/* Left group (New dropdown + Upload) and the right-aligned bulk group
   (Paste + Compress), JUS-120. */
.fm-toolbar-group {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.fm-toolbar-end {
  margin-left: auto;
}

.clipboard-hint {
  margin-left: var(--space-1);
}

.fm-caret {
  margin-left: 2px;
  color: var(--muted);
}

/* Small screens drop the toolbar labels, leaving icon-only buttons whose
   accessible name comes from each button's aria-label + title (JUS-120). */
@media (max-width: 640px) {
  .fm-btn-label {
    display: none;
  }
}

.segmented {
  display: inline-flex;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 2px;
}

.seg-btn {
  display: grid;
  place-items: center;
  width: 30px;
  height: 26px;
  border: 0;
  background: none;
  border-radius: 3px;
  color: var(--muted);
  cursor: pointer;
}

.seg-btn:hover {
  color: var(--fg-2);
}

.seg-btn.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
}

.fm-bar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 0 0 auto;
  flex-wrap: wrap;
  margin-bottom: var(--space-3);
}

.fm-path {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-wrap: wrap;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 2px;
  min-height: 32px;
}

.fm-crumb {
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
  transition: background var(--motion-base) var(--ease-standard), color var(--motion-base) var(--ease-standard);
}

.fm-crumb:hover:not(:disabled) {
  background: var(--hover-row);
  color: var(--fg-2);
}

.fm-crumb:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.fm-crumb.is-current {
  background: var(--selected-row);
  color: var(--fg-2);
  font-weight: 600;
}

.fm-path .sep {
  color: var(--meta);
}

.fm-search {
  max-width: 260px;
  min-width: 180px;
  margin-left: auto;
}

.fm-bulk {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  flex-wrap: wrap;
  background: var(--accent-soft);
  border-bottom: 1px solid color-mix(in oklab, var(--accent) 28%, transparent);
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.fm-bulk-actions {
  margin-left: auto;
  display: inline-flex;
  gap: var(--space-2);
}

.fm-state {
  /* A state view owns the whole entry area, which is why it scrolls
     internally instead of growing the panel. */
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-8) 0;
  color: var(--muted);
}

.fm-state h4 {
  color: var(--fg-2);
  margin: 0 0 4px;
}

.fm-state .fm-danger {
  color: var(--danger-ink);
}

.fm-state-action {
  margin-top: var(--space-3);
}

.fm-spinner {
  width: 22px;
  height: 22px;
  border-radius: var(--radius-pill);
  border: 2px solid var(--border-soft);
  border-top-color: var(--accent);
  animation: spin 700ms linear infinite;
}

.fm-readonly {
  margin-bottom: var(--space-3);
}

.fm-inline-error {
  margin-bottom: var(--space-3);
  align-items: center;
}

.fm-inline-error .grow {
  flex: 1 1 auto;
}

.fm-inline-error p {
  margin: 0;
}

.table-wrap {
  /* The entry list is the panel's single scrolling region (JUS-118). */
  flex: 1 1 auto;
  min-height: 0;
  overflow-x: auto;
  overflow-y: auto;
}

.fm-table {
  min-width: 820px;
}

.fm-row {
  display: grid;
  /* Leading checkbox column, then the type icon beside the name. */
  grid-template-columns: 34px minmax(240px, 1.6fr) 118px 96px 148px 84px 168px;
  gap: var(--space-3);
  align-items: center;
  padding: 7px var(--space-3);
  min-height: 42px;
  border-bottom: 1px solid var(--border);
  position: relative;
  transition: background var(--motion-base) var(--ease-standard);
}

.fm-row:hover {
  background: var(--hover-row);
}

.fm-row.is-selected {
  background: var(--accent-soft);
}

.fm-row.is-selected:hover {
  background: var(--accent-softer);
}

.fm-head {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
  border-bottom: 1px solid var(--border-soft);
  min-height: 34px;
  padding-block: var(--space-2);
}

.fm-head .right {
  text-align: right;
}

.fm-sort {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: inherit;
  font: inherit;
  letter-spacing: inherit;
  text-transform: inherit;
  border: 0;
  background: none;
  cursor: pointer;
  padding: 0;
}

.fm-sort:hover {
  color: var(--fg-2);
}

.fm-sort.is-active {
  color: var(--fg-2);
}

.fm-sort-arrow {
  transition: transform var(--motion-fast) linear;
}

.fm-sort-arrow.is-desc {
  transform: rotate(180deg);
}

.fm-name {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  text-align: left;
}

.fm-label {
  background: none;
  border: 0;
  padding: 0;
  text-align: left;
  min-width: 0;
  cursor: pointer;
}

.fm-label b {
  display: block;
  font-weight: 500;
  color: var(--fg);
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-label span {
  display: block;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
}

/* The row checkbox is revealed on hover / focus / selection. */
.fm-select {
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity var(--motion-fast) linear;
}

.fm-row:hover .fm-select,
.fm-row:focus-within .fm-select,
.fm-row.is-selected .fm-select {
  opacity: 1;
}

.fm-head .fm-select {
  opacity: 1;
}

.fm-cell {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
}

.fm-cell--size {
  text-align: right;
}

.fm-mode {
  display: flex;
  flex-direction: column;
  line-height: 1.3;
}

.fm-mode-octal {
  color: var(--fg-2);
}

.fm-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  justify-content: flex-end;
  opacity: 0;
  transition: opacity var(--motion-base) var(--ease-standard);
}

.fm-row:hover .fm-actions,
.fm-row:focus-within .fm-actions,
.fm-row.is-selected .fm-actions {
  opacity: 1;
}

.fm-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(176px, 1fr));
  gap: var(--space-3);
  /* Grid view scrolls internally too, keeping tiles top-aligned. */
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  align-content: start;
}

.fm-tile-wrap {
  position: relative;
  display: flex;
}

.fm-tile {
  position: relative;
  flex: 1 1 auto;
  width: 100%;
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-height: 132px;
  cursor: pointer;
  transition: border-color var(--motion-base) var(--ease-standard), background var(--motion-base) var(--ease-standard);
}

.fm-tile:hover {
  border-color: var(--accent);
}

.fm-tile.is-selected {
  border-color: var(--accent);
  background: var(--accent-soft);
}

.fm-tile-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-right: 26px;
}

.fm-tile .fm-label b {
  display: block;
  font-weight: 500;
  color: var(--fg);
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-tile .fm-label span {
  display: block;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
}

.fm-tile .fm-meta {
  margin-top: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
}

.fm-tile-check {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 2;
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: var(--radius-sm);
  background: var(--float);
  border: 1px solid var(--border);
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--motion-fast) linear;
}

.fm-tile-wrap:hover .fm-tile-check,
.fm-tile-wrap:focus-within .fm-tile-check,
.fm-tile-wrap:has(input:checked) .fm-tile-check {
  opacity: 1;
  pointer-events: auto;
}

.fm-more {
  display: flex;
  flex: 0 0 auto;
  justify-content: center;
  padding: var(--space-3) 0;
}

.fm-foot {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex: 0 0 auto;
  flex-wrap: wrap;
  padding: var(--space-3) 0 0;
  border-top: 1px solid var(--border);
  margin-top: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
}

.fm-foot .grow {
  flex: 1 1 auto;
}

.fm-drop-overlay {
  position: absolute;
  inset: 0;
  z-index: 5;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  border: 2px dashed var(--accent);
  border-radius: var(--radius-md);
  background: color-mix(in oklab, var(--surface) 82%, transparent);
  color: var(--accent-ink);
  font-weight: 500;
  pointer-events: none;
}

@media (hover: none) {
  .fm-actions,
  .fm-select {
    opacity: 1;
  }

  .fm-tile-check {
    opacity: 1;
    pointer-events: auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  .fm-actions,
  .fm-select,
  .fm-tile-check,
  .fm-sort-arrow {
    transition: none;
  }
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
