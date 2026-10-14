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
import { useElementSize } from "@vueuse/core";
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
import RecycleBinDialog from "@/features/files/components/RecycleBinDialog.vue";
import RenameEntryDialog from "@/features/files/components/RenameEntryDialog.vue";
import UploadDialog from "@/features/files/components/UploadDialog.vue";
import { useCoarseNow } from "@/features/files/composables/useCoarseNow";
import { useFileDownload } from "@/features/files/composables/useFileDownload";
import type { FilesEntry } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import type { FilesTab } from "@/features/files/stores/files";
import { useTrashStore } from "@/features/files/stores/trash";
import type { NameKind, TransferProblem } from "@/features/files/utils/files";
import {
  entryAction,
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
  resolveSymlinkTarget,
  typeaheadIndex,
} from "@/features/files/utils/files";
import {
  folderMenuOptions,
  rowMenuOptions,
} from "@/features/files/utils/filesMenu";
import type { FilesMenuItem } from "@/features/files/utils/filesMenu";
import {
  clampScrollTop,
  gridColumnCount,
  gridWindow,
  windowRange,
} from "@/features/files/utils/virtual";
import { useTeamsStore } from "@/features/teams";
import { formatBytes, relativeTime } from "@/shared/utils/format";

interface Props {
  serverId: string;
}

const props = defineProps<Props>();

const { t, locale } = useI18n();
const message = useMessage();
const files = useFilesStore();
const trash = useTrashStore();
const editor = useEditorStore();
const teams = useTeamsStore();
const { download } = useFileDownload();
/** coarseNow is the shared minute clock behind the Modified column's age. */
const coarseNow = useCoarseNow();

watch(
  () => props.serverId,
  (next) => {
    // A node switch changes the bin's scope; drop any loaded listing first so
    // the dialog can never show (or act on) the previous node's entries.
    trash.reset();
    void files.init(next);
  },
);
// A team switch changes the caller's role and the server's visibility, so the
// listing must reload rather than keep the previous team's directory. It also
// changes the bin's scope.
watch(
  () => teams.activeTeamId,
  () => {
    trash.reset();
    void files.reload();
  },
);
// The bin is per root: a root switch invalidates the loaded listing.
watch(
  () => files.rootName,
  () => trash.reset(),
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
/** recycleOpen shows the recycle-bin dialog for the current root (JUS-122). */
const recycleOpen = ref(false);
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

/** hasSelection gates the toolbar's bulk actions (JUS-128). */
const hasSelection = computed<boolean>(() => files.selectedNames.length > 0);

/** selection is the current selection reduced to writable-safe entries. */
const selection = computed<FilesEntry[]>(() =>
  files.selectedEntries.filter(isSelectable),
);

/** footSummary is the "12 entries · 4 folders · 6 files…" footer line. */
const footSummary = computed<string>(() => {
  // While the folder filter is narrowing the list, the leading count reports
  // "shown of total" so a filtered view never reads as the whole folder.
  const filtering = files.query.trim() !== "";
  const parts = [
    filtering
      ? t("files.browser.entryCountFiltered", {
          shown: files.visibleEntries.length,
          total: files.entries.length,
        })
      : t("files.browser.entryCount", { count: files.entries.length }),
  ];
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

/** entrySubtitle is the grid tile's second line (type + symlink target). The
 * list view has a Type column and keeps only {@link entryTarget}, so the type
 * is never repeated there. */
function entrySubtitle(entry: FilesEntry): string {
  if (entry.kind === "symlink") {
    return entry.symlink_target
      ? `${typeLabel(entry)} → ${entry.symlink_target}`
      : typeLabel(entry);
  }
  return typeLabel(entry);
}

/**
 * entryTarget is the list row's inline hint: a symlink's target, or "" — the
 * type already lives in the Type column, so it is not repeated under the name
 * (JUS-134).
 */
function entryTarget(entry: FilesEntry): string {
  return entry.kind === "symlink" && entry.symlink_target
    ? `→ ${entry.symlink_target}`
    : "";
}

/**
 * symlinkTargetPath resolves a symlink row's target to the root-relative path
 * "Go to target" opens ("" is the root's top level), or null when the target
 * lives outside the browsable root (the action is then hidden). A non-symlink
 * row resolves to null.
 */
function symlinkTargetPath(entry: FilesEntry): string | null {
  if (entry.kind !== "symlink" || !entry.symlink_target) {
    return null;
  }
  return resolveSymlinkTarget(
    files.path,
    entry.name,
    entry.symlink_target,
    files.currentRoot?.path ?? "",
  );
}

/**
 * goToSymlinkTarget opens a symlink's resolved target. The listing does not say
 * whether the target is a directory, so a target that turns out to be a file,
 * a broken link or an unreadable folder keeps the current view and reports it
 * with a non-blocking warning rather than replacing the list with an error
 * (JUS-132).
 */
async function goToSymlinkTarget(entry: FilesEntry): Promise<void> {
  const target = symlinkTargetPath(entry);
  if (target === null) {
    return;
  }
  if (!(await files.openSymlinkTarget(target))) {
    message.warning(
      String(t("files.dialogs.symlinkTargetFailed", { name: entry.name })),
    );
  }
}

/** ariaSort reflects the store's sort for one column header. */
function ariaSort(key: string): "ascending" | "descending" | "none" {
  if (files.sortKey !== key) {
    return "none";
  }
  return files.sortDir === "asc" ? "ascending" : "descending";
}

/**
 * activate performs an entry's default action for a double-click (or Enter) on
 * a row (JUS-130). The dispatch rule lives in {@link entryAction} so it is
 * unit-tested; every branch reuses the existing open/extract path rather than
 * duplicating it.
 */
function activate(entry: FilesEntry): void {
  switch (
    entryAction(entry, {
      canOpenEditor: files.canReadContent,
      canExtract: files.canWrite,
    })
  ) {
    case "directory":
      void files.openDirectory(entry.name);
      break;
    case "editor":
      openInEditor(entry);
      break;
    case "extract":
      openExtract(entry);
      break;
    case "none":
      break;
  }
}

/** isRowControl reports a press that belongs to a row's own control, not the row. */
function isRowControl(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    target.closest(".fm-select, .fm-actions, .fm-tile-check") !== null
  );
}

/**
 * extendRange applies a Shift+click's range selection from the anchor to `entry`
 * over the current sorted+filtered order (JUS-138). ⌘/Ctrl+Shift keeps the
 * existing selection; Shift alone replaces it.
 */
function extendRange(event: MouseEvent, entry: FilesEntry): void {
  if (!isSelectable(entry)) {
    return;
  }
  files.selectRange(entry.name, event.ctrlKey || event.metaKey);
}

/**
 * onRowClick selects a row. A plain click makes it the sole selection;
 * Ctrl/⌘+click toggles it in the current selection; Shift+click extends the
 * selection from the last-clicked anchor (⌘/Ctrl+Shift keeps it, Shift alone
 * replaces it). Activation is left to the double-click (or Enter), so a press on
 * a checkbox/action never changes the selection and two clicks never open the
 * entry twice. The row-control guard runs FIRST, so Shift+click on an inline
 * action or the ⋮ menu behaves exactly as without Shift (the control runs, the
 * selection is untouched); the checkbox's own capture handler covers the other
 * row control.
 */
function onRowClick(event: MouseEvent, entry: FilesEntry): void {
  if (isRowControl(event.target) || !isSelectable(entry)) {
    return;
  }
  if (event.shiftKey) {
    event.preventDefault();
    extendRange(event, entry);
    // A Shift+mousedown cancelled the browser's focus (onListMouseDown), so
    // move the roving focus to the clicked row here (JUS-139).
    focusRow(entry.name);
    return;
  }
  if (event.ctrlKey || event.metaKey) {
    files.toggleSelection(entry.name);
    return;
  }
  files.selectOnly(entry.name);
}

/**
 * onCheckboxCapture routes a Shift+click on a row's checkbox into the range
 * selection. Capture runs before the checkbox, and swallowing the press stops it
 * toggling that one row as well, so a Shift+click on the checkbox behaves like
 * one on the row itself. A plain click is left to the checkbox (JUS-138).
 */
function onCheckboxCapture(event: MouseEvent, entry: FilesEntry): void {
  if (!event.shiftKey) {
    return;
  }
  event.preventDefault();
  event.stopPropagation();
  extendRange(event, entry);
  focusRow(entry.name);
}

/**
 * onListMouseDown suppresses the browser's text selection for a Shift+press in
 * the list/grid: without it a Shift+click starts selecting the rows' text
 * instead of a run of entries (JUS-138). It also cancels the default focus a
 * press would move; the Shift+click handlers move the roving focus explicitly
 * (JUS-139), so the row the user clicks still becomes the focused row.
 */
function onListMouseDown(event: MouseEvent): void {
  if (event.shiftKey) {
    event.preventDefault();
  }
}

/**
 * onRowDblclick performs a row's default action, ignoring its own controls and
 * a Shift+double-click (JUS-138): Shift is a range gesture, so a shift-double
 * click must not open a folder (which would clear the selection) or activate an
 * entry. A plain double-click (JUS-130) is unchanged.
 */
function onRowDblclick(event: MouseEvent, entry: FilesEntry): void {
  if (event.shiftKey || isRowControl(event.target)) {
    return;
  }
  activate(entry);
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

// ── Windowing (JUS-137) ───────────────────────────────────────────────────
// A long directory renders only the visible window plus overscan, with
// pixel-exact spacers standing in for the unrendered rows so the scrollbar (and
// the sticky header, selection, filter and footer) keep working across rows that
// are not in the DOM. Below the threshold every row renders, so a small folder
// keeps exactly the DOM it always had and both paths behave identically.
/** VIRTUAL_THRESHOLD is the entry count above which the list/grid window. */
const VIRTUAL_THRESHOLD = 100;
/** rowHeight caches --fm-row-height (px) for the window maths. */
const rowHeight = ref(36);
/** tileMetrics caches the .fm-grid tile size/gap (px) for the window maths. */
const tileMetrics = ref({ min: 176, gap: 12, height: 132 });
/** localScrollTop mirrors the entry list's offset (the store keeps it per tab). */
const localScrollTop = ref(0);
/** headEl measures the sticky header the window must not count as visible. */
const headEl = ref<HTMLElement | null>(null);
const scrollSize = useElementSize(scrollEl);
// border-box: a revealed row must clear the header's padding and border too.
const headSize = useElementSize(headEl, undefined, { box: "border-box" });

// ── Focus retention (JUS-137 review rounds 1-2) ───────────────────────────
// Windowing unmounts off-window rows, which used to (a) drop keyboard focus to
// <body> when the focused row scrolled away and (b) make off-window rows
// unreachable. Rather than add a roving tabindex — which needs arrow-key row
// navigation, still out of scope — the row that owns focus (or the row whose
// context menu is open) is PINNED into the rendered set: it stays mounted
// wherever it is, rendered absolutely at its true offset so it never disturbs
// the flow spacers, and keeps focus/menu. Tab keeps walking the rendered rows;
// as focus passes the window edge the native focus scroll advances the window,
// so the next entry is always already rendered.
/** focusedName is the entry that last received focus inside the list/grid. */
const focusedName = ref<string | null>(null);
/** menuRowName is the row whose context menu is open. */
const menuRowName = ref<string | null>(null);
/**
 * pinnedName is the one entry kept rendered outside the window: the row whose
 * context menu is open, else the row that owns focus. It is DERIVED, so every
 * route that drops a pin source (menu closed/Escape, focus left, directory or
 * tab change, entry gone) releases the pin — there is no stale flag to leak.
 */
const pinnedName = computed<string | null>(
  () => menuRowName.value ?? focusedName.value,
);
/**
 * restorePending is armed only by a render WE schedule (see the arm watcher
 * below) and consumed once after it. Chromium blurs a node the keyed diff moves
 * as well as one the user clicks away from, and both report relatedTarget=null;
 * the arm is what tells the two apart, so a plain click never steals focus back.
 */
let restorePending = false;
/** lastFocusWasPointer marks a click's focus so it never nudges the scroll. */
let lastFocusWasPointer = false;

/** pinnedIndex is the pinned entry's index in the current listing, or -1. */
const pinnedIndex = computed<number>(() => {
  const name = pinnedName.value;
  if (name === null) {
    return -1;
  }
  return files.visibleEntries.findIndex((entry) => entry.name === name);
});

/**
 * parsePx reads a CSS px length ("36px") with a numeric fallback. A non-px token
 * (rem, calc) fails loudly to the fallback rather than mis-parsing to a tiny
 * size that would window the whole listing.
 */
function parsePx(value: string, fallback: number): number {
  const text = value.trim();
  if (!text.endsWith("px")) {
    return fallback;
  }
  const parsed = Number.parseFloat(text);
  return Number.isFinite(parsed) ? parsed : fallback;
}

/**
 * readVirtualMetrics re-reads the row/tile sizes from the live CSS tokens, so
 * the window maths tracks the one-value density change and never duplicates a
 * size (JUS-134 kept --fm-row-height as the single list-row height).
 */
function readVirtualMetrics(): void {
  const el = scrollEl.value;
  if (el === null) {
    return;
  }
  const style = getComputedStyle(el);
  rowHeight.value = parsePx(
    style.getPropertyValue("--fm-row-height"),
    rowHeight.value,
  );
  tileMetrics.value = {
    min: parsePx(style.getPropertyValue("--fm-tile-min"), tileMetrics.value.min),
    gap: parsePx(style.columnGap, tileMetrics.value.gap),
    height: parsePx(
      style.getPropertyValue("--fm-tile-height"),
      tileMetrics.value.height,
    ),
  };
}
watch(
  () => scrollEl.value,
  (el) => {
    readVirtualMetrics();
    // A list↔grid switch mounts a fresh scroller at offset 0; resync so the
    // window does not render around the previous view's offset (a blank list).
    localScrollTop.value = el?.scrollTop ?? 0;
  },
  { flush: "post" },
);

/** isListVirtual / isGridVirtual gate windowing on a long listing. */
const isListVirtual = computed<boolean>(
  () =>
    files.viewMode === "list" &&
    files.visibleEntries.length > VIRTUAL_THRESHOLD,
);
const isGridVirtual = computed<boolean>(
  () =>
    files.viewMode === "grid" &&
    files.visibleEntries.length > VIRTUAL_THRESHOLD,
);

/** listWindow is the list slice to render (the whole list when not windowing). */
const listWindow = computed(() => {
  const count = files.visibleEntries.length;
  if (!isListVirtual.value) {
    return { start: 0, end: count, padTop: 0, padBottom: 0 };
  }
  return windowRange(
    count,
    rowHeight.value,
    clampScrollTop(
      localScrollTop.value,
      headSize.height.value + count * rowHeight.value,
      scrollSize.height.value,
    ),
    scrollSize.height.value,
    headSize.height.value,
  );
});

/**
 * RenderedRow is one row/tile in the DOM: its listing index and whether it is
 * the pinned (out-of-window, focus- or menu-holding) element.
 */
interface RenderedRow {
  entry: FilesEntry;
  index: number;
  pinned: boolean;
}

/** windowRows is the rendered slice plus the pinned row, in listing order. */
function windowRows(
  entries: FilesEntry[],
  start: number,
  end: number,
): RenderedRow[] {
  const rows: RenderedRow[] = [];
  for (let index = start; index < end; index += 1) {
    const entry = entries[index];
    if (entry) {
      rows.push({ entry, index, pinned: false });
    }
  }
  const pinned = pinnedIndex.value;
  if (
    pinned >= 0 &&
    entries[pinned] &&
    (pinned < start || pinned >= end)
  ) {
    rows.push({ entry: entries[pinned], index: pinned, pinned: true });
    rows.sort((a, b) => a.index - b.index);
  }
  return rows;
}

/** listRows is the windowed list slice plus the pinned row. */
const listRows = computed<RenderedRow[]>(() => {
  const entries = files.visibleEntries;
  if (!isListVirtual.value) {
    return entries.map((entry, index) => ({ entry, index, pinned: false }));
  }
  const win = listWindow.value;
  return windowRows(entries, win.start, win.end);
});

/** pinnedListStyle places the pinned list row at its true offset inside
 *  .fm-table (position: relative), just below the in-flow sticky header. */
function pinnedListStyle(index: number): Record<string, string> {
  return { top: `${headSize.height.value + index * rowHeight.value}px` };
}

/** gridColumns is the current auto-fill column count for the container width. */
const gridColumns = computed<number>(() =>
  gridColumnCount(
    scrollSize.width.value,
    tileMetrics.value.min,
    tileMetrics.value.gap,
  ),
);
/** gridPitch is one tile row's height plus the row gap. */
const gridPitch = computed<number>(
  () => tileMetrics.value.height + tileMetrics.value.gap,
);
/** gridContentHeight is the full grid's pixel height (no trailing row gap). */
function gridContentHeight(count: number): number {
  const rows = Math.ceil(count / gridColumns.value);
  return rows > 0 ? rows * gridPitch.value - tileMetrics.value.gap : 0;
}
/** gridWindowRange is the grid slice (whole rows) to render. */
const gridWindowRange = computed(() => {
  const count = files.visibleEntries.length;
  if (!isGridVirtual.value) {
    return { start: 0, end: count, padTop: 0, padBottom: 0 };
  }
  return gridWindow(
    count,
    gridColumns.value,
    gridPitch.value,
    clampScrollTop(
      localScrollTop.value,
      gridContentHeight(count),
      scrollSize.height.value,
    ),
    scrollSize.height.value,
  );
});
/** gridRows is the windowed grid slice plus the pinned tile in listing order. */
const gridRows = computed<RenderedRow[]>(() => {
  const entries = files.visibleEntries;
  if (!isGridVirtual.value) {
    return entries.map((entry, index) => ({ entry, index, pinned: false }));
  }
  const win = gridWindowRange.value;
  return windowRows(entries, win.start, win.end);
});

/** gridTileWidth is one tile's width for the current column count. */
const gridTileWidth = computed<number>(() => {
  const cols = Math.max(1, gridColumns.value);
  return (
    (scrollSize.width.value - (cols - 1) * tileMetrics.value.gap) / cols
  );
});

/** pinnedTileStyle places the pinned grid tile at its true row/column offset. */
function pinnedTileStyle(index: number): Record<string, string> {
  const cols = Math.max(1, gridColumns.value);
  const gap = tileMetrics.value.gap;
  return {
    top: `${Math.floor(index / cols) * gridPitch.value}px`,
    left: `${(index % cols) * (gridTileWidth.value + gap)}px`,
    width: `${gridTileWidth.value}px`,
  };
}

/**
 * The grid spacers span every column, so the CSS row gap also sits between a
 * spacer and the first/last tile row. Subtracting one gap keeps the rendered
 * rows exactly on their true pixel offsets. (The list uses in-flow spacers with
 * no gap, so its pads are used as-is.)
 */
const gridSpacerTop = computed<number>(() =>
  gridWindowRange.value.padTop > 0
    ? gridWindowRange.value.padTop - tileMetrics.value.gap
    : 0,
);
const gridSpacerBottom = computed<number>(() =>
  gridWindowRange.value.padBottom > 0
    ? gridWindowRange.value.padBottom - tileMetrics.value.gap
    : 0,
);

/**
 * tabLabel is the visible tab name: the folder's basename, or the root. When
 * two tabs show different locations that share a basename (two folders named
 * "config" under different parents) the parent segment is prefixed so the tabs
 * stay distinguishable; the full path is always the tab's tooltip
 * ({@link tabTitle}).
 */
function tabLabel(tab: FilesTab): string {
  const base = tabBasename(tab);
  const here = tabLocation(tab);
  const duplicate = files.tabs.some(
    (other) =>
      other.id !== tab.id &&
      tabBasename(other) === base &&
      tabLocation(other) !== here,
  );
  if (!duplicate) {
    return base;
  }
  const parent = tab.path.split("/").slice(-2, -1)[0];
  return parent ? `${parent}/${base}` : `${tab.rootName}/${base}`;
}

/** tabBasename is a tab's own folder name (the root name at the top level). */
function tabBasename(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : (tab.path.split("/").pop() ?? tab.rootName);
}

/** tabLocation is the tab's full "root/path" identity. */
function tabLocation(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : `${tab.rootName}/${tab.path}`;
}

/** modifiedLabel renders the Modified column as a localized relative age that
 * refreshes on the shared coarse clock, so it never goes stale on an idle page
 * (JUS-134). */
function modifiedLabel(entry: FilesEntry): string {
  return relativeTime(
    entry.modified_at,
    locale.value === "vi" ? "vi" : "en",
    { long: true, now: coarseNow.value },
  );
}

/** tabTitle is the tab tooltip / accessible name: the full root-relative path. */
function tabTitle(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : `${tab.rootName}/${tab.path}`;
}

/** openNewTab opens a tab at the active root's top level. */
function openNewTab(): void {
  void files.openTab();
}

/** focusActiveTab moves DOM focus onto the active tab and keeps it in view. */
async function focusActiveTab(): Promise<void> {
  await nextTick();
  const active = tabListEl.value?.querySelector<HTMLElement>(
    '[role="tab"][aria-selected="true"]',
  );
  active?.focus();
  active?.scrollIntoView({ block: "nearest", inline: "nearest" });
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
      void files.switchTab(neighbourTabId(tab.id, 1));
      break;
    case "ArrowLeft":
    case "ArrowUp":
      event.preventDefault();
      void files.switchTab(neighbourTabId(tab.id, -1));
      break;
    case "Home":
      event.preventDefault();
      void files.switchTab(files.tabs[0].id);
      break;
    case "End":
      event.preventDefault();
      void files.switchTab(files.tabs[files.tabs.length - 1].id);
      break;
    case "Delete":
      // Closing the only tab would silently recreate it at the root (the × is
      // hidden for a single tab), so Delete is a no-op there too.
      if (files.tabs.length > 1) {
        event.preventDefault();
        void files.closeTab(tab.id);
      }
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

/**
 * onListScroll records the active tab's entry-list scroll offset and drives the
 * windowed range (the store owns the per-tab offset for tab-restore).
 */
function onListScroll(event: Event): void {
  const el = event.currentTarget as HTMLElement;
  localScrollTop.value = el.scrollTop;
  files.setScrollTop(el.scrollTop);
}

/**
 * scrollEntryIntoView brings one entry into the list's visible band, scrolling
 * when the row is outside the current window or below the fold. It is how a row
 * that must be seen (a just-created or renamed entry) stays reachable when the
 * listing is windowed and the row itself is not rendered (JUS-137).
 */
function scrollEntryIntoView(name: string): void {
  const index = files.visibleEntries.findIndex((entry) => entry.name === name);
  const el = scrollEl.value;
  if (index < 0 || el === null) {
    return;
  }
  const grid = files.viewMode === "grid";
  const itemSize = grid
    ? tileMetrics.value.height + tileMetrics.value.gap
    : rowHeight.value;
  const row = grid ? Math.floor(index / gridColumns.value) : index;
  // The sticky header shares the scrollport, so the usable band starts below it.
  const lead = grid ? 0 : headSize.height.value;
  const top = lead + row * itemSize;
  const bottom = top + itemSize;
  if (top < el.scrollTop + lead) {
    el.scrollTop = Math.max(0, top - lead);
  } else if (bottom > el.scrollTop + el.clientHeight) {
    el.scrollTop = bottom - el.clientHeight;
  }
  localScrollTop.value = el.scrollTop;
  // Record the new offset in the store too: a create/rename changes
  // entries.length, and the replay watcher below would otherwise restore the
  // stale pre-reveal offset over this scroll.
  files.setScrollTop(el.scrollTop);
}

// ── Row keyboard navigation (JUS-139) ─────────────────────────────────────
// The list/grid is one roving tabindex: the focused row's name control carries
// tabindex 0 and every other row's carries -1, so Tab enters the listing once
// and the arrows move within it. The per-row controls (checkbox, inline
// actions, ⋮) keep their own tab stops, so Tab still walks a row's controls and
// crosses the window edge — the JUS-137 focus pin above keeps that crossing and
// a scroll-away safe, so it stays.
/** rovingName is the entry whose name control holds the list's tab stop. */
const rovingName = ref<string | null>(null);
/** TYPEAHEAD_RESET_MS is the idle gap after which a type-to-focus run resets. */
const TYPEAHEAD_RESET_MS = 1000;
/** typeBuffer accumulates type-to-focus keystrokes; typeTimer clears it. */
let typeBuffer = "";
let typeTimer: ReturnType<typeof setTimeout> | null = null;

/**
 * rovingTabName is the entry that owns tabindex 0. It follows the focused row,
 * falling back to the first rendered row (then the first entry) so the list
 * always has exactly one tab stop, even after the roving row scrolls out.
 */
const rovingTabName = computed<string | null>(() => {
  const entries = files.visibleEntries;
  if (entries.length === 0) {
    return null;
  }
  if (
    rovingName.value !== null &&
    entries.some((entry) => entry.name === rovingName.value)
  ) {
    return rovingName.value;
  }
  const rendered = files.viewMode === "grid" ? gridRows.value : listRows.value;
  return rendered[0]?.entry.name ?? entries[0].name;
});

/** rowTabindex is the roving tabindex for one row's name control. */
function rowTabindex(name: string): 0 | -1 {
  return name === rovingTabName.value ? 0 : -1;
}

/** isTextTarget reports a keystroke aimed at a text field, not the listing. */
function isTextTarget(target: HTMLElement | null): boolean {
  if (target === null) {
    return false;
  }
  const tag = target.tagName;
  return (
    tag === "INPUT" ||
    tag === "TEXTAREA" ||
    tag === "SELECT" ||
    target.isContentEditable
  );
}

/** focusedListIndex is the listing index of the row that owns focus, or -1. */
function focusedListIndex(): number {
  const active = document.activeElement as HTMLElement | null;
  const row = active?.closest<HTMLElement>("[data-entry]");
  const name = row?.dataset.entry ?? rovingTabName.value;
  return name === null
    ? -1
    : files.visibleEntries.findIndex((entry) => entry.name === name);
}

/** pageStep is how many entries one PageUp/PageDown moves (a viewport of rows). */
function pageStep(itemSize: number, columns: number): number {
  const rows = Math.max(
    1,
    Math.floor(scrollSize.height.value / Math.max(1, itemSize)),
  );
  return rows * columns;
}

/** entryAt clamps one listing index to a real entry, or null. */
function entryAt(index: number): FilesEntry | null {
  const entries = files.visibleEntries;
  if (entries.length === 0) {
    return null;
  }
  return entries[Math.min(Math.max(index, 0), entries.length - 1)] ?? null;
}

/**
 * focusRowAt moves the roving focus to one listing index, materialising the row
 * first (scrollEntryIntoView + nextTick) so a row outside the rendered window
 * can still take focus. The pin is released first so the keyed re-render cannot
 * pull focus back to the row being left.
 */
async function focusRowAt(index: number): Promise<void> {
  const entry = entryAt(index);
  if (entry === null) {
    return;
  }
  rovingName.value = entry.name;
  releaseFocus();
  scrollEntryIntoView(entry.name);
  await nextTick();
  focusEntry(entry.name);
}

/**
 * focusRow moves the roving focus to one entry by name. Shift+click cancels the
 * browser's own focus (onListMouseDown preventDefault), so the click handler
 * calls this to move the roving focus to the clicked row (JUS-139).
 */
function focusRow(name: string): void {
  rovingName.value = name;
  scrollEntryIntoView(name);
  void nextTick(() => focusEntry(name));
}

/** typeToFocus jumps the roving focus to the next typed-prefix match. */
function typeToFocus(char: string): void {
  if (typeTimer !== null) {
    clearTimeout(typeTimer);
  }
  typeBuffer += char;
  typeTimer = setTimeout(() => {
    typeBuffer = "";
    typeTimer = null;
  }, TYPEAHEAD_RESET_MS);
  const names = files.visibleEntries.map((entry) => entry.name);
  const current = focusedListIndex();
  const index = typeaheadIndex(names, typeBuffer, current < 0 ? 0 : current + 1);
  if (index >= 0) {
    void focusRowAt(index);
  }
}

/**
 * onListKeydown drives the roving row focus: arrows (the grid adds Left/Right),
 * Home/End, PageUp/PageDown, Space to toggle the focused row's selection,
 * Shift+Arrow to extend the range from the anchor, Ctrl/Cmd+A to select every
 * visible entry, and type-to-focus for a printable prefix. Enter opens the
 * focused row. Keys typed into a field or a dialog/menu never reach here (the
 * handler is on the list, and both are guarded), and a row control (checkbox,
 * inline action, ⋮) keeps its native Enter/Space.
 */
function onListKeydown(event: KeyboardEvent): void {
  if (event.defaultPrevented) {
    return;
  }
  const target = event.target as HTMLElement | null;
  if (
    isTextTarget(target) ||
    target?.closest(".n-modal, .n-dropdown") !== null
  ) {
    return;
  }
  const meta = event.ctrlKey || event.metaKey;
  // Ctrl/Cmd+A selects every visible entry (the header checkbox's set).
  if (meta && !event.altKey && event.key.toLowerCase() === "a") {
    event.preventDefault();
    if (!files.allVisibleSelected) {
      files.toggleSelectAll();
    }
    return;
  }
  if (meta || event.altKey) {
    return;
  }
  const grid = files.viewMode === "grid";
  const columns = Math.max(1, gridColumns.value);
  const step = grid ? columns : 1;
  const page = pageStep(
    grid ? gridPitch.value : rowHeight.value,
    grid ? columns : 1,
  );
  const current = focusedListIndex();
  let next: number;
  switch (event.key) {
    case "ArrowDown":
      next = current + step;
      break;
    case "ArrowUp":
      next = current - step;
      break;
    case "ArrowRight":
      if (!grid) {
        return;
      }
      next = current + 1;
      break;
    case "ArrowLeft":
      if (!grid) {
        return;
      }
      next = current - 1;
      break;
    case "Home":
      next = 0;
      break;
    case "End":
      next = files.visibleEntries.length - 1;
      break;
    case "PageDown":
      next = current + page;
      break;
    case "PageUp":
      next = current - page;
      break;
    case " ": {
      if (isRowControl(target)) {
        return;
      }
      event.preventDefault();
      const entry = files.visibleEntries[current];
      if (entry && isSelectable(entry)) {
        files.toggleSelection(entry.name);
      }
      return;
    }
    case "Enter": {
      if (isRowControl(target)) {
        return;
      }
      event.preventDefault();
      const entry = files.visibleEntries[current];
      if (entry) {
        activate(entry);
      }
      return;
    }
    default:
      if (
        event.key.length === 1 &&
        !event.ctrlKey &&
        !event.metaKey &&
        !event.altKey
      ) {
        event.preventDefault();
        typeToFocus(event.key);
      }
      return;
  }
  event.preventDefault();
  const targetEntry = entryAt(next);
  if (event.shiftKey) {
    if (targetEntry && isSelectable(targetEntry)) {
      files.selectRange(targetEntry.name, false);
    }
  } else {
    files.setAnchor(targetEntry?.name ?? null);
  }
  void focusRowAt(next);
}

/** restoringFocus marks a programmatic focus restore so it does not re-scroll. */
let restoringFocus = false;
/** focusedEl is the exact control that held focus, restored after a re-render. */
let focusedEl: HTMLElement | null = null;

/** focusEntry restores focus to the exact control that had it, else the row. */
function focusEntry(name: string | null): void {
  const el = scrollEl.value;
  if (name === null || el === null) {
    return;
  }
  // Prefer the very control that held focus (a row's ⋮, checkbox, action): the
  // keyed window keeps that node, so Tab continues from where it was.
  if (
    focusedEl !== null &&
    focusedEl.isConnected &&
    focusedEl.closest<HTMLElement>("[data-entry]")?.dataset.entry === name
  ) {
    restoringFocus = true;
    focusedEl.focus({ preventScroll: true });
    restoringFocus = false;
    return;
  }
  for (const node of el.querySelectorAll<HTMLElement>("[data-entry]")) {
    if (node.dataset.entry !== name) {
      continue;
    }
    const target = node.querySelector<HTMLElement>(".fm-label, .fm-tile");
    if (target !== null) {
      // The row may be off-screen (pinned): never yank the user's scroll.
      restoringFocus = true;
      target.focus({ preventScroll: true });
      restoringFocus = false;
    }
    return;
  }
}

/**
 * onListFocusIn remembers the focused row and keeps it visible. The focus pin
 * keeps the row mounted when the user scrolls it out of the window (so focus is
 * never dropped to <body>), and the scroll clears the sticky header a Shift+Tab
 * or a programmatic focus left the row under. A restored focus must not
 * re-scroll, and neither must a click: a pointer focus should never nudge the
 * list, so the scroll only runs for keyboard-initiated focus.
 */
function onListFocusIn(event: FocusEvent): void {
  const target = event.target as HTMLElement | null;
  const row = target?.closest<HTMLElement>("[data-entry]");
  const name = row?.dataset.entry ?? null;
  if (name === null) {
    return;
  }
  focusedName.value = name;
  focusedEl = target;
  if (!restoringFocus && !lastFocusWasPointer) {
    scrollEntryIntoView(name);
  }
}

/** releaseFocus drops the focus pin along with its remembered control. */
function releaseFocus(): void {
  focusedName.value = null;
  focusedEl = null;
}

/**
 * onListFocusOut keeps the pin only while focus stays in the list (another row)
 * or moves into its (teleported) row menu, and drops it when focus leaves.
 *
 * A `relatedTarget` of null is ambiguous — Chromium reports it both for a node
 * our window re-render moved or unmounted and for a press on non-focusable
 * content — so the pin is kept only when `restorePending` was armed by our own
 * render; a plain blur releases it (and never refocuses).
 */
function onListFocusOut(event: FocusEvent): void {
  const next = event.relatedTarget as Element | null;
  if (next !== null) {
    if (scrollEl.value?.contains(next) || next.closest(".n-dropdown")) {
      return;
    }
    releaseFocus();
    return;
  }
  if (!restorePending) {
    releaseFocus();
  }
}

/** closeRowMenu drops the menu's pin; a still-focused row stays pinned. */
function closeRowMenu(): void {
  menuRowName.value = null;
}

/**
 * onContextShow keeps the context dropdown's model in step and releases the menu
 * pin on every close route the dropdown itself drives (Escape, click-away), not
 * just the ones we call closeRowMenu() from.
 */
function onContextShow(show: boolean): void {
  contextOpen.value = show;
  if (!show) {
    closeRowMenu();
  }
}

// Arm a one-shot refocus for a render WE schedule, and consume it after the
// patch. A keyed window change moves or unmounts the focused node and Chromium
// blurs it; `restorePending` (armed pre-flush, before the patch) is what tells
// that blur apart from a user press on the very next focusout. The post-flush
// consumer runs whether or not the component re-rendered, so the arm can never
// go stale. A pointerdown/keydown clears the arm first, so a user gesture wins.
watch(
  [listRows, gridRows],
  () => {
    restorePending = true;
  },
  { flush: "pre" },
);
watch(
  [listRows, gridRows],
  () => {
    if (!restorePending) {
      return;
    }
    restorePending = false;
    if (document.activeElement !== document.body || focusedName.value === null) {
      return;
    }
    focusEntry(focusedName.value);
  },
  { flush: "post" },
);

// A directory, tab or root change moves every row: the focused/menu name from
// the previous listing must not survive (it could pin a same-named entry there).
watch(
  () => [files.activeTabId, files.rootName, files.path] as const,
  () => {
    releaseFocus();
    closeRowMenu();
  },
);

// The pinned entry itself disappearing (deleted, renamed, filtered out) releases
// the pin rather than leaving a name that a same-named entry could match later.
watch(pinnedIndex, (index) => {
  if (index === -1) {
    releaseFocus();
    closeRowMenu();
  }
});

// Move focus to the active tab as soon as it changes (nextTick), never after
// the tab's listing finishes loading — otherwise the roving tabindex lags
// behind and the next arrow press navigates from the stale tab. Also covers a
// mouse close: the × removes the focused button, so focus must follow the tab
// that takes over.
watch(() => files.activeTabId, () => void focusActiveTab());

// Replay the active tab's scroll offset after a switch, and after a load
// completes (a restore that landed while the rows were still unrendered would
// clamp to 0), plus back to the top on navigation (the store zeroes it). The
// windowed range reads localScrollTop, so it is kept in step here too. The
// visible-entry count is in the key so a shrinking filter re-clamps the offset.
// `scrollEntryIntoView` writes the store offset back, so a create/rename reveal
// always wins over this replay.
watch(
  () =>
    [
      files.activeTabId,
      files.path,
      files.loaded,
      files.entries.length,
      files.visibleEntries.length,
    ] as const,
  async () => {
    await nextTick();
    if (scrollEl.value) {
      scrollEl.value.scrollTop = files.scrollTop;
      localScrollTop.value = scrollEl.value.scrollTop;
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
  return rowMenuOptions(entry, {
    ...base,
    canPaste,
    symlinkTargetPath: symlinkTargetPath(entry),
  });
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
    case "go-target":
      void goToSymlinkTarget(entry);
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
  // Pin the row so its menu keeps its anchor while the list scrolls (JUS-137).
  menuRowName.value = entry.name;
  contextX.value = event.clientX;
  contextY.value = event.clientY;
  contextOpen.value = true;
}

/** openFolderContext opens the folder-level menu at the pointer. */
function openFolderContext(event: MouseEvent): void {
  contextEntry.value = null;
  closeRowMenu();
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
  menuRowName.value = entry.name;
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
 *
 * It also marks the press: a user gesture always wins over an armed refocus (the
 * blur it causes is deliberate and must not snap focus back to a stale row).
 */
function onDocumentPointerDown(event: PointerEvent): void {
  restorePending = false;
  lastFocusWasPointer = true;
  if (!contextOpen.value) {
    return;
  }
  const target = event.target as HTMLElement | null;
  if (target && target.closest(".n-dropdown")) {
    return;
  }
  contextOpen.value = false;
  closeRowMenu();
}

/** onDocumentKeyDown marks keyboard-initiated focus (and any key as a gesture). */
function onDocumentKeyDown(): void {
  restorePending = false;
  lastFocusWasPointer = false;
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointerDown, true);
  document.addEventListener("keydown", onDocumentKeyDown, true);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown, true);
  document.removeEventListener("keydown", onDocumentKeyDown, true);
});

/** onContextSelect dispatches a context-menu action and closes the menu. */
function onContextSelect(key: string): void {
  const entry = contextEntry.value;
  contextOpen.value = false;
  closeRowMenu();
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
    case "recycle-bin":
      recycleOpen.value = true;
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
          <!-- JUS-134: the agent-node selector (which decides the whole
               listing) lives next to the Root selector instead of a bar at
               the page bottom. -->
          <slot name="header-tools" />
          <span class="small muted fm-field-label">{{ $t("files.browser.rootLabel") }}</span>
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

    <!-- Tab bar (JUS-123/126): one tab per open directory. A middle-click or
         the × closes a tab; the + opens a new one at the root top and sits
         right after the last tab (the strip hugs its tabs and scrolls, so + is
         always reachable). The tablist owns only role=tab buttons (the × is a
         span inside the tab) so it satisfies aria-required-children; keyboard
         users close with Delete. -->
    <div class="fm-tabs">
      <div
        ref="tabListEl"
        class="fm-tabstrip"
        role="tablist"
        :aria-label="$t('files.tabs.list')"
      >
        <button
          v-for="tab in files.tabs"
          :key="tab.id"
          type="button"
          class="fm-tab"
          :class="{ 'is-active': tab.id === files.activeTabId }"
          role="tab"
          :id="'fm-tab-' + tab.id"
          aria-controls="fm-tabpanel"
          aria-keyshortcuts="Delete"
          :aria-selected="tab.id === files.activeTabId"
          :tabindex="tab.id === files.activeTabId ? 0 : -1"
          :title="tabTitle(tab)"
          :aria-label="tabTitle(tab)"
          @click="files.switchTab(tab.id)"
          @keydown="onTabKeydown($event, tab)"
          @auxclick="onTabAuxClick($event, tab.id)"
        >
          <span class="fm-tab-name">{{ tabLabel(tab) }}</span>
          <span
            v-if="files.tabs.length > 1"
            class="fm-tab-close"
            aria-hidden="true"
            :title="$t('files.tabs.close', { name: tabLabel(tab) })"
            @click.stop="void files.closeTab(tab.id)"
          >
            <X :size="12" :stroke-width="2" aria-hidden="true" />
          </span>
        </button>
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

    <!-- Toolbar + path bar (JUS-120/128, merged in JUS-134): one wrapping row
         holds the create/upload actions, the breadcrumb, the folder filter and
         the per-root tools; the bulk actions (Download/Copy/Cut/Compress/
         Delete/Clear) appear on it only while entries are selected, so no
         permanently disabled row is rendered. -->
    <div class="fm-bar fm-toolbar">
      <div v-if="files.canWrite" class="fm-toolbar-group">
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

      <NButton size="small" secondary :loading="files.loading" :title="$t('files.browser.refresh')" :aria-label="$t('files.browser.refresh')" @click="files.refresh()">
        <template #icon>
          <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ $t("files.browser.refresh") }}</span>
      </NButton>

      <!-- The bin belongs to the root, not the folder, so it lives on the path
           bar: any member may open it, while restore/purge inside need write. -->
      <NButton
        size="small"
        secondary
        :disabled="files.rootName === ''"
        :title="$t('files.browser.recycleBin')"
        :aria-label="$t('files.browser.recycleBin')"
        @click="recycleOpen = true"
      >
        <template #icon>
          <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        <span class="fm-btn-label">{{ $t("files.browser.recycleBin") }}</span>
      </NButton>

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

        <!-- Bulk actions (JUS-128, JUS-134): shown only while entries are
             selected, so no disabled row is ever visible. They stay mounted
             and are hidden with visibility — not removed — so the toolbar's
             height never changes when the first click of a double-click
             selects a row (JUS-130: selecting must not shift the rows). -->
        <span class="small muted fm-bulk-count">
          <template v-if="hasSelection">
            {{ $t("files.browser.selectionCount", { count: files.selectedNames.length }) }}
            · {{ formatBytes(files.selectedBytes) }}
          </template>
        </span>
        <div class="fm-selection-actions" :class="{ 'is-visible': hasSelection }">
        <NButton
          size="small"
          secondary
          :disabled="!files.canReadContent || !hasSelectedFiles"
          :title="!files.canReadContent ? $t('files.browser.fileNeedsRole') : (hasSelectedFiles ? $t('files.browser.downloadSelected') : $t('files.dialogs.compressSelect'))"
          :aria-label="$t('files.browser.downloadSelected')"
          @click="downloadSelected()"
        >
          <template #icon>
            <Download :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          <span class="fm-btn-label">{{ $t("files.browser.downloadSelected") }}</span>
        </NButton>
        <template v-if="files.canWrite">
          <NButton
            size="small"
            secondary
            :disabled="files.acting || selection.length === 0"
            :title="selection.length === 0 ? $t('files.dialogs.compressSelect') : $t('files.actions.copy')"
            :aria-label="$t('files.actions.copy')"
            @click="files.copySelection(selection.map((item) => item.name))"
          >
            <template #icon>
              <Copy :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            <span class="fm-btn-label">{{ $t("files.actions.copy") }}</span>
          </NButton>
          <NButton
            size="small"
            secondary
            :disabled="files.acting || selection.length === 0"
            :title="selection.length === 0 ? $t('files.dialogs.compressSelect') : $t('files.actions.cut')"
            :aria-label="$t('files.actions.cut')"
            @click="files.cutSelection(selection.map((item) => item.name))"
          >
            <template #icon>
              <Scissors :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            <span class="fm-btn-label">{{ $t("files.actions.cut") }}</span>
          </NButton>
          <NButton
            size="small"
            secondary
            :disabled="files.acting || selection.length === 0"
            :title="selection.length === 0 ? $t('files.dialogs.compressSelect') : $t('files.actions.compress')"
            :aria-label="$t('files.actions.compress')"
            @click="openCompress(selection)"
          >
            <template #icon>
              <Archive :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            <span class="fm-btn-label">{{ $t("files.actions.compress") }}</span>
          </NButton>
          <NButton
            size="small"
            type="error"
            secondary
            :disabled="files.acting || selection.length === 0"
            :title="selection.length === 0 ? $t('files.dialogs.compressSelect') : $t('common.actions.delete')"
            :aria-label="$t('common.actions.delete')"
            @click="openDelete(selection)"
          >
            <template #icon>
              <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            <span class="fm-btn-label">{{ $t("common.actions.delete") }}</span>
          </NButton>
        </template>
        <NButton
          :disabled="!hasSelection"
          size="small"
          quaternary
          :title="$t('files.browser.clear')"
          :aria-label="$t('files.browser.clear')"
          @click="files.clearSelection()"
        >
          <template #icon>
            <X :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          <span class="fm-btn-label">{{ $t("files.browser.clear") }}</span>
        </NButton>
        </div>
      </div>
    </div>

    <!-- The tab's content region: one panel shared by every tab (aria-controls
         points at it), labelled by the active tab. -->
    <div
      id="fm-tabpanel"
      class="fm-panel"
      role="tabpanel"
      :aria-labelledby="'fm-tab-' + files.activeTabId"
    >
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

      <div v-if="files.viewMode === 'list'" ref="scrollEl" class="table-wrap" @scroll="onListScroll" @mousedown="onListMouseDown" @keydown="onListKeydown" @focusin="onListFocusIn" @focusout="onListFocusOut" @contextmenu.prevent="openFolderContext">
        <div
          class="fm-table"
          :class="{ 'is-virtual': isListVirtual }"
          role="table"
          :aria-rowcount="files.visibleEntries.length + 1"
        >
          <div ref="headEl" class="fm-row fm-head" role="row" :aria-rowindex="1">
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

          <!-- Windowed list (JUS-137): the spacers stand in for the unrendered
               rows so the scrollbar still spans the whole listing. -->
          <div
            v-if="listWindow.padTop > 0"
            class="fm-pad"
            :style="{ height: `${listWindow.padTop}px` }"
            aria-hidden="true"
          ></div>
          <div
            v-for="{ entry, index, pinned } in listRows"
            :key="entry.name"
            class="fm-row"
            :class="{ 'is-selected': files.isSelected(entry.name), 'is-pinned': pinned }"
            role="row"
            :aria-rowindex="index + 2"
            :aria-selected="
              isSelectable(entry) ? files.isSelected(entry.name) : undefined
            "
            :data-entry="entry.name"
            :style="pinned ? pinnedListStyle(index) : undefined"
            @click="onRowClick($event, entry)"
            @dblclick="onRowDblclick($event, entry)"
            @contextmenu.stop.prevent="openRowContext($event, entry)"
            @keydown="onRowKeydown($event, entry)"
          >
            <span class="fm-select" role="cell" @click.capture="onCheckboxCapture($event, entry)">
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
              <button
                type="button"
                class="fm-label"
                :tabindex="rowTabindex(entry.name)"
              >
                <b>{{ entry.name }}</b>
                <span v-if="entryTarget(entry)" class="fm-target" :title="entryTarget(entry)">
                  {{ entryTarget(entry) }}
                </span>
              </button>
            </div>
            <div class="fm-cell" role="cell">{{ typeLabel(entry) }}</div>
            <div class="fm-cell fm-cell--size" role="cell">{{ entry.kind === "directory" ? "—" : formatBytes(entry.size) }}</div>
            <div class="fm-cell fm-date" role="cell" :title="formatTimestamp(entry.modified_at)">{{ modifiedLabel(entry) }}</div>
            <div class="fm-cell fm-mode" role="cell" :title="formatMode(entry.mode)">{{ formatOctalModeFull(entry.mode) }}</div>
            <div class="fm-actions" role="cell">
              <!-- The inline actions (.fm-action) stay hover/focus-revealed on
                   pointer devices; the ⋮ menu button below stays visible
                   whenever the row has actions, so touch and keyboard users
                   reach the row menu without a hover (JUS-133). -->
              <NButton
                v-if="entry.kind === 'directory'"
                class="fm-action"
                size="tiny"
                quaternary
                :title="$t('files.browser.openFolder', { name: entry.name })"
                :aria-label="$t('files.browser.openFolder', { name: entry.name })"
                @click="files.openDirectory(entry.name)"
              >
                <template #icon>
                  <ChevronRight :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <NButton
                v-else-if="entry.kind === 'file'"
                class="fm-action"
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
                class="fm-action"
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
                class="fm-action"
                size="tiny"
                quaternary
                :title="$t('files.browser.renameEntry', { name: entry.name })"
                :aria-label="$t('files.browser.renameEntry', { name: entry.name })"
                @click="openRename(entry)"
              >
                <template #icon>
                  <Pencil :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <NButton
                v-if="files.canWrite && isSelectable(entry)"
                class="fm-action"
                size="tiny"
                quaternary
                :title="$t('files.browser.deleteEntry', { name: entry.name })"
                :aria-label="$t('files.browser.deleteEntry', { name: entry.name })"
                @click="openDelete([entry])"
              >
                <template #icon>
                  <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
              </NButton>
              <!-- A row with no actions (a special/unspecified entry) has no
                   menu, so there is deliberately no ⋮ to open an empty menu. -->
              <NDropdown
                v-if="rowMenu(entry).length > 0"
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
                  :title="$t('files.browser.moreActions', { name: entry.name })"
                  :aria-label="$t('files.browser.moreActions', { name: entry.name })"
                >
                  <template #icon>
                    <MoreVertical :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </NDropdown>
            </div>
          </div>
          <div
            v-if="listWindow.padBottom > 0"
            class="fm-pad"
            :style="{ height: `${listWindow.padBottom}px` }"
            aria-hidden="true"
          ></div>
        </div>
      </div>

      <div
        v-else
        ref="scrollEl"
        class="fm-grid"
        :class="{ 'is-virtual': isGridVirtual }"
        @scroll="onListScroll"
        @mousedown="onListMouseDown"
        @keydown="onListKeydown"
        @focusin="onListFocusIn"
        @focusout="onListFocusOut"
        @contextmenu.prevent="openFolderContext"
      >
        <!-- The spacers span every column and stand in for the unrendered rows
             (JUS-137). A spacer, not padding: a huge padding-bottom would stop
             the flex column from shrinking the scroller to its viewport. -->
        <div
          v-if="gridSpacerTop > 0"
          class="fm-pad"
          :style="{ height: `${gridSpacerTop}px` }"
          aria-hidden="true"
        ></div>
        <div
          v-for="{ entry, index, pinned } in gridRows"
          :key="entry.name"
          class="fm-tile-wrap"
          :class="{ 'is-pinned': pinned }"
          :data-entry="entry.name"
          :style="pinned ? pinnedTileStyle(index) : undefined"
          @click="onRowClick($event, entry)"
          @dblclick="onRowDblclick($event, entry)"
          @contextmenu.stop.prevent="openRowContext($event, entry)"
          @keydown="onRowKeydown($event, entry)"
        >
          <label
            v-if="isSelectable(entry)"
            class="fm-tile-check"
            :title="$t('files.browser.selectEntry', { name: entry.name })"
            @click.capture="onCheckboxCapture($event, entry)"
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
            :tabindex="rowTabindex(entry.name)"
          >
            <span class="fm-tile-top">
              <FileTypeIcon :kind="iconKind(entry)" />
            </span>
            <span class="fm-label">
              <b>{{ entry.name }}</b>
              <span :title="entrySubtitle(entry)">{{ entrySubtitle(entry) }}</span>
            </span>
            <span class="fm-meta">
              <template v-if="entry.kind !== 'directory'">{{ formatBytes(entry.size) }} · </template>{{ formatTimestamp(entry.modified_at) }}
            </span>
          </button>
        </div>
        <div
          v-if="gridSpacerBottom > 0"
          class="fm-pad"
          :style="{ height: `${gridSpacerBottom}px` }"
          aria-hidden="true"
        ></div>
      </div>

      <div v-if="files.hasMore" class="fm-more">
        <NButton size="small" secondary :loading="files.loadingMore" @click="files.loadMore()">
          {{ files.loadingMore ? $t("files.browser.loadingMore") : $t("files.browser.loadMore") }}
        </NButton>
      </div>

      <div class="fm-foot">
        <span class="grow">{{ footSummary }}</span>
        <span v-if="files.rootName !== ''" class="fm-foot-path">{{ $t("files.browser.currentPath", { path: currentPath }) }}</span>
        <span class="fm-foot-free">{{ $t("files.browser.freeSpace", { free: formatBytes(files.freeBytes) }) }}</span>
      </div>
    </template>
    </div>

    <div v-if="dragActive" class="fm-drop-overlay" aria-hidden="true">
      <Upload :size="26" :stroke-width="1.6" />
      <span>{{ $t("files.dialogs.uploadDrop") }}</span>
    </div>

    <CreateEntryDialog v-model:show="createOpen" :kind="createKind" @created="scrollEntryIntoView" />
    <RenameEntryDialog v-model:show="renameOpen" :entry="renameTarget" @renamed="scrollEntryIntoView" />
    <PermissionsDialog v-model:show="permissionsOpen" :entry="permissionsTarget" />
    <DeleteEntriesDialog v-model:show="deleteOpen" :entries="actionTargets" />
    <RecycleBinDialog v-model:show="recycleOpen" />
    <UploadDialog v-model:show="uploadOpen" :initial-files="droppedFiles" />
    <CompressDialog v-model:show="compressOpen" :entries="compressTargets" />
    <ExtractDialog v-model:show="extractOpen" :entry="extractTarget" />
    <ConflictDialog />

    <!-- One controlled dropdown serves the row / empty-space context menu and
         the Shift+F10 / menu key; x/y place it at the pointer or the row. The
         explicit update:show handler releases the menu pin on the dropdown's
         own close routes (Escape, click-away), not just our calls. -->
    <NDropdown
      :show="contextOpen"
      trigger="manual"
      placement="bottom-start"
      :x="contextX"
      :y="contextY"
      :options="contextOptions"
      :menu-props="() => ({ role: 'menu', 'aria-label': contextAria })"
      @update:show="onContextShow"
      @select="onContextSelect"
    />
  </NCard>
</template>

<style scoped>
/* Ported from docs/design/assets/gotham-views.css (FILE MANAGER section) and
 * docs/design/files.html; only tokens from styles/tokens.css are used. */
.files-browser {
  position: relative;
  /* JUS-134: the single row-height token every list row reads. JUS-137 can
   * size its virtual rows from it, so the compact desktop density stays a
   * one-value change. */
  --fm-row-height: 36px;
  /* The panel is a flex column that fills whatever height its parent gives it
   * (JUS-118): toolbar/bar/footer stay fixed while the entry list is the
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
  /* Hug the tabs so the + button sits right after the last one (JUS-126);
     shrinking below the content keeps the strip the only scroll region. */
  flex: 0 1 auto;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: thin;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 3px;
}

.fm-tab {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 auto;
  max-width: 220px;
  border: 0;
  background: none;
  padding: 8px 11px;
  border-radius: var(--radius-sm);
  color: var(--muted);
  font-size: var(--text-sm);
  cursor: pointer;
}

.fm-tab.is-active {
  background: var(--selected-row);
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
  border-radius: var(--radius-sm);
  color: var(--muted);
}

.fm-tab-close:hover {
  background: var(--hover-row);
  color: var(--fg-2);
}

.fm-tab-add {
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
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

/* The tabpanel: one flex column shared by every tab, so the entry list stays
   the single scrolling region and the footer stays pinned (JUS-118/123). */
.fm-panel {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
}

.fm-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  flex-wrap: wrap;
  margin-bottom: var(--space-3);
}

/* Left group (New dropdown + Upload) and the right-aligned group: clipboard
   Paste plus the JUS-128 bulk actions (Download/Copy/Cut/Compress/Delete/Clear)
   that used to live in their own bar, JUS-120/128. Both groups wrap internally
   and may shrink, so a long right group never overflows/clips at narrow
   widths — every action stays reachable instead of spilling past the panel. */
.fm-toolbar-group {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  min-width: 0;
}

/* The right-hand cluster follows the filter/refresh/bin; the filter's
   margin-left:auto pushes the whole cluster right, so the group itself needs
   no auto margin (which would split the free space with the filter). */
.fm-toolbar-end {
  justify-content: flex-end;
}

.clipboard-hint {
  margin-left: var(--space-1);
}

.fm-caret {
  margin-left: 2px;
  color: var(--muted);
}

/* Small screens drop the toolbar labels, leaving icon-only buttons whose
   accessible name comes from each button's aria-label + title (JUS-120).
   JUS-135's phone layout rules live at the end of this stylesheet (after the
   base rules they override). */
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

/* Bulk selection count on the toolbar (JUS-128): the former .fm-bulk bar is
   gone; the count and its actions live in .fm-toolbar-end. A fixed width (not
   a min-width) keeps the slot the same size whether or not anything is
   selected, so the count appearing never re-wraps the toolbar and shifts the
   rows under a double-click (JUS-130). */
.fm-bulk-count {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  width: 10rem;
  /* A constant height as well as width: with nothing selected the count is
     empty, and an empty inline box would collapse and shift the rows. */
  min-height: 22px;
  flex: 0 0 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* JUS-134: the bulk actions stay mounted and are hidden with visibility (not
   display) while nothing is selected — reserving the slot keeps the toolbar
   height fixed, so the first click of a row double-click can never shift the
   rows (JUS-130). They are not rendered as a disabled row. */
.fm-selection-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: var(--space-2);
  visibility: hidden;
}

.fm-selection-actions.is-visible {
  visibility: visible;
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

/* JUS-137: the pinned (out-of-window) row is absolutely positioned, so the
   table is its containing block. */
.fm-table.is-virtual {
  position: relative;
}

/* JUS-137: the windowed list replaces the unrendered rows with these exact-px
   spacers, so the scrollbar (and cross-row select-all) still spans the listing.
   role=presentation keeps them invisible to assistive tech. */
.fm-pad {
  display: block;
  width: 100%;
}

/* In the grid a spacer spans every column so it never lands in a tile cell. */
.fm-grid > .fm-pad {
  grid-column: 1 / -1;
}

.fm-row {
  display: grid;
  /* Leading checkbox column, then the type icon beside the name. */
  grid-template-columns: 34px minmax(240px, 1.6fr) 118px 96px 148px 84px 168px;
  gap: var(--space-3);
  align-items: center;
  padding: 4px var(--space-3);
  /* JUS-134: one compact, tokenised row height instead of the old 52px. */
  min-height: var(--fm-row-height);
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

/* JUS-137: a windowed row takes exactly the token height so the fixed-size
   window maths never drifts (the un-windowed list keeps its min-height). */
.fm-table.is-virtual .fm-row:not(.fm-head) {
  height: var(--fm-row-height);
  overflow: hidden;
}

/* The pinned row (focus/menu holder) is rendered outside the flow window but at
   its true offset, so it never disturbs the spacers. */
.fm-table.is-virtual .fm-row.is-pinned {
  position: absolute;
  left: 0;
  right: 0;
  z-index: 1;
}

.fm-head {
  /* JUS-132: the column header stays pinned to the top of the entry list's
     scroll region so the active sort column is visible in long folders. The
     solid surface matches the card, so rows scroll under it cleanly. */
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--surface);
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

/* Hovering the pinned header must not reveal the rows scrolling under it:
   keep the solid card surface instead of the row hover tint. */
.fm-head:hover {
  background: var(--surface);
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

/* JUS-134: the name and its (optional) symlink target share one line so every
 * row keeps the single tokenised height and the type is not repeated (the Type
 * column carries it). */
.fm-name .fm-label {
  display: flex;
  flex-direction: row;
  align-items: baseline;
  gap: var(--space-2);
  min-width: 0;
}

.fm-label b {
  display: block;
  flex: 0 1 auto;
  min-width: 0;
  font-weight: 500;
  color: var(--fg);
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-label span {
  display: block;
  flex: 0 1 auto;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  /* A long symlink target must not wrap or spill the name column (JUS-132);
     the full text stays reachable through the span's title tooltip. */
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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

/* JUS-134: the mode cell is one line ("755"); the rwx form is its tooltip. */
.fm-mode,
.fm-date {
  white-space: nowrap;
}

.fm-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  justify-content: flex-end;
}

/* Inline row actions (open/download/extract/rename/delete) reveal on row hover
   or keyboard focus / selection; the ⋮ menu button is never hidden when the row
   has actions, so touch and keyboard users reach the row menu without a hover
   (JUS-133). */
.fm-actions .fm-action {
  opacity: 0;
  transition: opacity var(--motion-base) var(--ease-standard);
}

.fm-row:hover .fm-actions .fm-action,
.fm-row:focus-within .fm-actions .fm-action,
.fm-row.is-selected .fm-actions .fm-action {
  opacity: 1;
}

/* A disabled action (a file's Download without the content role) keeps naive's
   dimmed opacity when revealed: these selectors out-specify both the reveal
   rule above and naive's own .n-button--disabled rule. */
.fm-row:hover .fm-actions .fm-action.n-button--disabled,
.fm-row:focus-within .fm-actions .fm-action.n-button--disabled,
.fm-row.is-selected .fm-actions .fm-action.n-button--disabled {
  opacity: var(--n-opacity-disabled, 0.5);
}

.fm-grid {
  display: grid;
  --fm-tile-min: 176px;
  --fm-tile-height: 132px;
  grid-template-columns: repeat(auto-fill, minmax(var(--fm-tile-min), 1fr));
  gap: var(--space-3);
  /* Grid view scrolls internally too, keeping tiles top-aligned. */
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  align-content: start;
}

/* JUS-137: windowed tiles are pinned to the token height so the fixed row maths
   stays exact (the un-windowed grid keeps its min-height and natural flow). */
.fm-grid.is-virtual {
  position: relative;
}

.fm-grid.is-virtual .fm-tile {
  height: var(--fm-tile-height);
}

/* The pinned tile (focus holder) sits at its true row/column offset. */
.fm-grid.is-virtual .fm-tile-wrap.is-pinned {
  position: absolute;
  z-index: 1;
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
  /* JUS-137: the one tile-height source the window maths reads too. */
  min-height: var(--fm-tile-height);
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
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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

/* ── JUS-135: phone layout ────────────────────────────────────────────────
   At 390px the toolbar used to wrap to four rows and the selection cluster
   squeezed the list to ~1.5 rows. The rules below (deliberately last, so they
   win over the desktop base rules):
   - trim the card padding and the inter-block rhythm so the fixed chrome is
     a thin band;
   - put the selection cluster (count + Download/Copy/Cut/Compress/Delete/
     Clear) on its own full-width row that never wraps and scrolls
     horizontally when a long selection or touch-sized targets overflow it, so
     every action stays reachable and the row count (hence the list height) is
     the same with and without a selection (JUS-130);
   - keep Root/view toggle/tabs to one or two compact rows. */
@media (max-width: 640px) {
  .files-browser :deep(.n-card-header) {
    padding: var(--space-2) var(--space-3);
  }

  .files-browser :deep(.n-card-content) {
    padding: 0 var(--space-3) var(--space-2);
  }

  .fm-card-head {
    gap: var(--space-2);
  }

  /* JUS-135: the card heading is redundant on a phone (the page/environment
     already name the view) and keeping it forces the node/root/view controls
     onto a second row. */
  .fm-card-title {
    display: none;
  }

  .fm-card-tools {
    gap: var(--space-2);
  }

  /* The "Root" caption is redundant at this width: the control keeps its
     accessible name through aria-label. (The slotted "Agent node" caption is
     hidden by ServerFilesPage, which owns that slot content.) */
  .fm-field-label {
    display: none;
  }

  .fm-root-select {
    width: auto;
    min-width: 0;
    flex: 1 1 90px;
  }

  .fm-tabs,
  .fm-toolbar {
    margin-bottom: var(--space-1);
  }

  /* Slimmer tab strip/footer band: every pixel here is list height. */
  .fm-tab {
    padding: 6px 9px;
  }

  .fm-tab-add {
    width: 30px;
    height: 30px;
  }

  /* Row 1: create/upload group + the breadcrumb (scrolls, never wraps). */
  .fm-path {
    flex: 1 1 auto;
    min-width: 0;
    flex-wrap: nowrap;
    overflow-x: auto;
    scrollbar-width: thin;
  }

  /* Row 2: the folder filter, then Refresh and Recycle bin. */
  .fm-search {
    min-width: 0;
    max-width: none;
    flex: 1 1 120px;
    margin-left: 0;
  }

  /* Row 3: the selection cluster, full width. */
  .fm-toolbar-end {
    flex: 1 1 100%;
    justify-content: flex-start;
    flex-wrap: nowrap;
  }

  .fm-bulk-count {
    width: auto;
    min-width: 0;
    /* Keep the count at its natural width; the scrolling action row absorbs
       the shrink instead of clipping the count text (JUS-135). */
    flex: 0 0 auto;
  }

  .fm-selection-actions {
    flex: 1 1 auto;
    min-width: 0;
    flex-wrap: nowrap;
    justify-content: flex-start;
    overflow-x: auto;
    scrollbar-width: thin;
  }

  /* Every selection action is a 44px touch target on a phone (the row
     scrolls, so the larger targets never squeeze the toolbar). */
  .fm-selection-actions :deep(.n-button) {
    min-width: 44px;
    height: 44px;
  }

  /* Compact phone footer: the entry summary is the useful line; the path and
     the free-space readout duplicate the breadcrumb/root. */
  .fm-foot {
    gap: var(--space-3);
    margin-top: 2px;
    padding-top: 6px;
  }

  .fm-foot-path,
  .fm-foot-free {
    display: none;
  }
}

@media (hover: none) {
  .fm-actions .fm-action {
    opacity: 1;
  }

  .fm-actions .fm-action.n-button--disabled {
    opacity: var(--n-opacity-disabled, 0.5);
  }

  .fm-select {
    opacity: 1;
  }

  .fm-tile-check {
    opacity: 1;
    pointer-events: auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  .fm-actions .fm-action,
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
