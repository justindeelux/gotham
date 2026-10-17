<script setup lang="ts">
import {
  ArchiveRestore,
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
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
  NInputGroup,
  NSelect,
  useMessage,
} from "naive-ui";
import { computed, h, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import ConflictDialog from "@/features/files/components/ConflictDialog.vue";
import CompressDialog from "@/features/files/components/CompressDialog.vue";
import CreateEntryDialog from "@/features/files/components/CreateEntryDialog.vue";
import DeleteEntriesDialog from "@/features/files/components/DeleteEntriesDialog.vue";
import ExtractDialog from "@/features/files/components/ExtractDialog.vue";
import FileTypeIcon from "@/features/files/components/FileTypeIcon.vue";
import FilesSearchResults from "@/features/files/components/FilesSearchResults.vue";
import FilesSelectionBar from "@/features/files/components/FilesSelectionBar.vue";
import FolderSizeCell from "@/features/files/components/FolderSizeCell.vue";
import ImageViewerDialog from "@/features/files/components/ImageViewerDialog.vue";
import PermissionsDialog from "@/features/files/components/PermissionsDialog.vue";
import RecycleBinDialog from "@/features/files/components/RecycleBinDialog.vue";
import RenameEntryDialog from "@/features/files/components/RenameEntryDialog.vue";
import UploadDialog from "@/features/files/components/UploadDialog.vue";
import { useCoarseNow } from "@/features/files/composables/useCoarseNow";
import { useFileDownload } from "@/features/files/composables/useFileDownload";
import { resetFolderSizes } from "@/features/files/composables/useFolderSize";
import type { FilesEntry } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import type { FilesTab } from "@/features/files/stores/files";
import { useTrashStore } from "@/features/files/stores/trash";
import type { FilesSortKey, NameKind, TransferProblem } from "@/features/files/utils/files";
import {
  disambiguatedTabLabels,
  entryAction,
  entryIconKind,
  entryTypeLabel,
  filesFromDrop,
  formatMode,
  formatTimestamp,
  isExtractableArchive,
  isFileDrag,
  isImageFile,
  isSelectable,
  joinPath,
  ownerModeLabel,
  resolveSymlinkTarget,
  typeaheadIndex,
  widestBytesLabel,
} from "@/features/files/utils/files";
import {
  folderMenuOptions,
  rowMenuOptions,
} from "@/features/files/utils/filesMenu";
import type { FilesMenuItem } from "@/features/files/utils/filesMenu";
import { isRowControlTarget } from "@/features/files/utils/rowControls";
import {
  clampScrollTop,
  gridColumnCount,
  gridWindow,
  renderedKey,
  windowRange,
} from "@/features/files/utils/virtual";
import { useTeamsStore } from "@/features/teams";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import AppTooltip from "@/shared/ui/AppTooltip.vue";
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
    // the dialog can never show (or act on) the previous node's entries. Cached
    // folder sizes (and any in-flight walk) belong to the old node too.
    trash.reset();
    resetFolderSizes();
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
  () => {
    trash.reset();
    resetFolderSizes();
  },
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

/** recursiveModel binds the "Include subdirectories" checkbox (JUS-163). */
const recursiveModel = computed<boolean>({
  get: () => files.recursive,
  set: (value) => files.setRecursive(value === true),
});

/** submitSearch runs the input as a bounded server-side name search. */
function submitSearch(): void {
  void files.runSearch();
}

/**
 * onSearchKeydown submits on Enter, ignoring the Enter that ends an IME
 * composition (Vietnamese Telex/VNI input) so a composition confirm never
 * fires a search.
 */
function onSearchKeydown(event: KeyboardEvent): void {
  if (event.key !== "Enter" || event.isComposing) {
    return;
  }
  event.preventDefault();
  submitSearch();
}

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

/** hasSelection gates the header's bulk bar and its focus hand-off (JUS-128). */
const hasSelection = computed<boolean>(() => files.selectedNames.length > 0);

/**
 * gridBulkVisible shows the grid toolbar's overlay bulk bar (JUS-208 round 2):
 * the grid has no column-header row for the bulk bar to replace (as in list),
 * so while a selection exists the cluster overlays the toolbar row instead of
 * growing it — selecting never shifts the tiles.
 */
const gridBulkVisible = computed<boolean>(
  () => files.viewMode === "grid" && hasSelection.value,
);

/**
 * countReserve is the widest selection summary the current folder can produce:
 * its full entry count (digit-monotonic, so never fewer digits than a subset)
 * and the widest byte label any subset can format to ({@link widestBytesLabel},
 * because formatBytes is non-monotonic across unit boundaries). It is a hidden
 * sizer in the count cell in BOTH selection states, so the cell width is
 * max(reserve, live) no matter what is selected and the bar's row height can
 * never depend on the selection (JUS-134/JUS-141).
 */
const countReserve = computed<string>(
  () =>
    `${t("files.browser.selectionCount", { count: files.entries.length })} · ${widestBytesLabel(
      files.entries.reduce((sum, entry) => sum + entry.size, 0),
    )}`,
);

/**
 * selectionAnnouncement is the text of the always-mounted polite live region
 * (JUS-146). The region is in the tree before any selection (empty), so the
 * first selection is announced — unlike a region that appears with its content.
 * The text is debounced to the settled count, so a range select over thousands
 * of rows announces once instead of chattering on every step.
 */
const selectionAnnouncement = ref("");
let selectionAnnounceTimer: ReturnType<typeof setTimeout> | null = null;
watch(
  () => files.selectedNames.length,
  (count) => {
    if (selectionAnnounceTimer !== null) {
      clearTimeout(selectionAnnounceTimer);
    }
    if (count === 0) {
      selectionAnnouncement.value = "";
      return;
    }
    selectionAnnounceTimer = setTimeout(() => {
      selectionAnnouncement.value = String(
        t("files.browser.selectionCount", { count }),
      );
    }, 400);
  },
);
onBeforeUnmount(() => {
  if (selectionAnnounceTimer !== null) {
    clearTimeout(selectionAnnounceTimer);
  }
  // Leaving the page aborts any in-flight size walk and drops the cached
  // results. Done at browser level, never per row: row windowing unmounts cells
  // that must keep their state.
  resetFolderSizes();
});

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
          // The total picks the plural form: "1 of 1 entry", not "1 of 1 entries".
          count: files.entries.length,
        })
      : t("files.browser.entryCount", { count: files.entries.length }),
  ];
  // The breakdown follows the rows on screen: while filtering it tallies the
  // visible rows, never mixing a filtered count with whole-folder totals
  // (JUS-141).
  const counts = filtering ? files.visibleCounts : files.counts;
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

/**
 * pasteLabel is the Paste button's visible label ("Paste (2)"), optionally
 * followed by the problem explaining why it is disabled. The accessible name
 * must contain that visible text (WCAG label-in-name), so one computed feeds
 * the visible label and the accessible name (JUS-140).
 */
const pasteLabel = computed<string>(() => {
  const count = files.clipboard?.names.length ?? 0;
  const base = `${String(t("files.actions.paste"))} (${count})`;
  return pasteProblemText.value ? `${base} — ${pasteProblemText.value}` : base;
});

/**
 * downloadActionLabel is the row Download's tooltip (JUS-156 F1): the action
 * name normally, the role explanation while the action is blocked. It mirrors
 * {@link pasteLabel} — one value drives the tooltip — but the button keeps its
 * own aria-label (the action name), so a sighted user reads the reason and an
 * assistive-tech user keeps the label-in-name.
 */
function downloadActionLabel(entry: FilesEntry): string {
  return String(
    files.canReadContent
      ? t("files.browser.downloadFile", { name: entry.name })
      : t("files.browser.fileNeedsRole"),
  );
}

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
 * with a non-blocking warning; a transient node/network failure gets its own
 * retry copy instead (JUS-132, JUS-141).
 */
async function goToSymlinkTarget(entry: FilesEntry): Promise<void> {
  const target = symlinkTargetPath(entry);
  if (target === null) {
    return;
  }
  const result = await files.openSymlinkTarget(target);
  if (result === "not-folder") {
    message.warning(
      String(t("files.dialogs.symlinkTargetFailed", { name: entry.name })),
    );
  } else if (result === "error") {
    message.error(
      String(t("files.dialogs.symlinkTargetError", { name: entry.name })),
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
 * duplicating it. Images bypass the dispatch into the viewer (JUS-204).
 */
function activate(entry: FilesEntry): void {
  if (isImageFile(entry) && files.canReadContent) {
    openViewer(entry);
    return;
  }
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
  return isRowControlTarget(target);
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
  // A plain click/tap on a selectable row also moves the roving row to the
  // pressed entry, so the roving tab stop (and the shared accessible description
  // that tracks it) describes the row the user just clicked (JUS-140). It moves
  // the tab stop only — no scroll/focus nudge (JUS-130/137), unlike the
  // Shift+click arm above, which has to restore the focus its press cancelled.
  rovingName.value = entry.name;
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
  // A Shift+press starts a range gesture, not a text selection (JUS-138).
  // JUS-146: a press outside the header that would move focus OUT of a focused
  // header control is prevented too — otherwise the press blurs the control to
  // <body> BEFORE the header→bar swap runs, so the swap's focus hand-off could
  // never see it. Keeping the focus lets the hand-off move it to the roving row.
  const pressOutsideHeader =
    (event.target as Element | null)?.closest(".fm-head") === null;
  if (
    event.shiftKey ||
    (pressOutsideHeader && document.activeElement?.closest(".fm-head") !== null)
  ) {
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
/**
 * scrollerWidthPx is the visible entry-list width, exposed to the sticky bulk
 * bar as --fm-scroller-w. The bar is constrained to it so, when the table is
 * wider than the scroller, its actions scroll in their own row instead of
 * hiding off-screen behind the table scroll (JUS-146).
 */
const scrollerWidthPx = computed<string>(() => `${scrollSize.width.value}px`);

// ── Focus retention (JUS-137 review rounds 1-2; JUS-139 roving) ───────────
// Windowing unmounts off-window rows, which used to drop keyboard focus to
// <body> when the focused row scrolled away. The row that owns focus (or the
// row whose context menu is open) is PINNED into the rendered set: it stays
// mounted wherever it is, rendered absolutely at its true offset so it never
// disturbs the flow spacers, and keeps focus/menu. Keyboard focus that crosses
// the window edge advances the window through scrollEntryIntoView
// (onListFocusIn), so the next entry is already rendered; the roving tabindex
// (JUS-139) then walks the rendered rows.
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
  const tiles = {
    min: parsePx(style.getPropertyValue("--fm-tile-min"), tileMetrics.value.min),
    gap: parsePx(style.columnGap, tileMetrics.value.gap),
    height: parsePx(
      style.getPropertyValue("--fm-tile-height"),
      tileMetrics.value.height,
    ),
  };
  // Assign only on a real change: a fresh object every resize tick would
  // invalidate every metric-dependent computed for nothing.
  if (
    tiles.min !== tileMetrics.value.min ||
    tiles.gap !== tileMetrics.value.gap ||
    tiles.height !== tileMetrics.value.height
  ) {
    tileMetrics.value = tiles;
  }
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
// Re-read the row/tile metrics when the scroller is resized (a viewport or zoom
// change), not only when it mounts or swaps view.
watch(
  [() => scrollSize.width.value, () => scrollSize.height.value],
  () => readVirtualMetrics(),
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
  if (pinned >= 0 && entries[pinned] && (pinned < start || pinned >= end)) {
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

/**
 * gridColumns is the resolved auto-fill column count. Reading the used
 * grid-template-columns keeps the window maths in step with the CSS (a
 * responsive token change included) instead of re-deriving the auto-fill
 * formula; the formula is the fallback before the grid is laid out.
 */
const gridColumns = computed<number>(() => {
  // Touch the reactive width/metrics FIRST: the resolved-value path below must
  // still re-evaluate when the container resizes, and getComputedStyle alone is
  // not a reactive dependency (it would freeze the count at the first layout).
  const width = scrollSize.width.value;
  const { min, gap } = tileMetrics.value;
  const el = scrollEl.value;
  if (el !== null && files.viewMode === "grid") {
    const columns = getComputedStyle(el).gridTemplateColumns;
    const count = columns
      .trim()
      .split(/\s+/)
      .filter((part) => part !== "").length;
    if (count > 0 && columns !== "none") {
      return count;
    }
  }
  return gridColumnCount(width, min, gap);
});
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

/**
 * renderedKeys identifies the rows/tiles currently in the DOM. The restore arm
 * is keyed on this rendered-set identity, not on the data slice, so a data-only
 * change that re-renders nothing never arms a refocus.
 */
const renderedKeys = computed<string>(() =>
  renderedKey(
    (files.viewMode === "grid" ? gridRows.value : listRows.value).map(
      (row) => row.entry.name,
    ),
  ),
);

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
 * tabLabels caches every tab's unique visible label. The pure rule lives in
 * {@link disambiguatedTabLabels}; caching it as a computed recomputes the
 * labels only when the tab set (or a tab's root/path) changes, not for every
 * tab on every render.
 */
const tabLabels = computed<Map<string, string>>(() =>
  disambiguatedTabLabels(files.tabs),
);

/** tabLabel is a tab's cached, unique visible name. */
function tabLabel(tab: FilesTab): string {
  return tabLabels.value.get(tab.id) ?? tabBasename(tab);
}

/** tabBasename is a tab's own folder name (the root name at the top level). */
function tabBasename(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : (tab.path.split("/").pop() ?? tab.rootName);
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

/**
 * rowDetails is the symbolic mode and the absolute modified time — the two
 * values the Mode/Modified cells keep only in a native `title`, unreachable for
 * keyboard and touch users (JUS-140).
 */
function rowDetails(entry: FilesEntry): string {
  return String(
    t("files.browser.rowDetails", {
      mode: formatMode(entry.mode),
      modified: formatTimestamp(entry.modified_at),
    }),
  );
}

/** ROW_DETAILS_ID is the single, visually hidden description target. */
const ROW_DETAILS_ID = "fm-row-details";

/**
 * rovingEntry is the entry that owns the list's one tab stop. The description
 * is exposed through `aria-describedby` at this ONE shared node rather than an
 * `aria-description`/hidden node per row: `aria-describedby` is the widely
 * supported mechanism (ARIA 1.3 `aria-description` is not announced by every
 * screen reader), and one node keeps the windowed list at zero extra DOM per
 * rendered row (JUS-140).
 */
const rovingEntry = computed<FilesEntry | null>(() => {
  const name = rovingTabName.value;
  if (name === null) {
    return null;
  }
  const rendered = files.viewMode === "grid" ? gridRows.value : listRows.value;
  return rendered.find((row) => row.entry.name === name)?.entry ?? null;
});

/** rovingDetails is the roving row's accessible description text (localized). */
const rovingDetails = computed<string>(() => {
  const entry = rovingEntry.value;
  return entry === null ? "" : rowDetails(entry);
});

/** rowDescribedBy points only the roving row's control at the shared node. */
function rowDescribedBy(entry: FilesEntry): string | undefined {
  return entry.name === rovingTabName.value ? ROW_DETAILS_ID : undefined;
}

/**
 * NARROW_TAB_PX is the sample's icon-only threshold: a tab narrower than this
 * hides its title (and an inactive one its close), like Chrome.
 */
const NARROW_TAB_PX = 90;

/**
 * narrowTabs are the tab ids currently rendered below the icon-only width.
 * The class MUST be binding-driven (a render would wipe a manual toggle), so
 * the measured ids live in a reactive set swapped wholesale per pass.
 */
const narrowTabs = ref<ReadonlySet<string>>(new Set());

/** updateNarrowTabs re-measures every tab against the icon-only threshold. */
function updateNarrowTabs(): void {
  const strip = tabListEl.value;
  if (!strip) {
    return;
  }
  const next = new Set<string>();
  for (const node of strip.querySelectorAll<HTMLElement>(".fm-tab")) {
    if (node.getBoundingClientRect().width < NARROW_TAB_PX) {
      next.add(node.id.replace(/^fm-tab-/, ""));
    }
  }
  const prev = narrowTabs.value;
  if (prev.size !== next.size || [...next].some((id) => !prev.has(id))) {
    narrowTabs.value = next;
  }
}

let narrowObserver: ResizeObserver | null = null;
onMounted(() => {
  // jsdom (unit tests) has no ResizeObserver; the watcher below still covers
  // tab-set changes there.
  if (typeof ResizeObserver !== "undefined") {
    narrowObserver = new ResizeObserver(() => updateNarrowTabs());
    if (tabListEl.value) {
      narrowObserver.observe(tabListEl.value);
    }
  }
  void nextTick(() => updateNarrowTabs());
});
onBeforeUnmount(() => {
  narrowObserver?.disconnect();
  narrowObserver = null;
});
// A tab add/remove/rename changes every tab's share of the strip; re-measure
// once the patch lands. Showing a title only ever grows its tab, so the pass
// converges instead of flapping.
watch(
  () => files.tabs.map((tab) => `${tab.id}:${tabLabel(tab)}`),
  () => void nextTick(() => updateNarrowTabs()),
  { flush: "post" },
);

/** tabTitle is the tab tooltip / accessible name: the full root-relative path. */
function tabTitle(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : `${tab.rootName}/${tab.path}`;
}

/** openNewTab opens a tab at the active root's top level. */
function openNewTab(): void {
  // JUS-207: intentionally leaves DOM focus where it is. The active-tab
  // watcher above only refocuses after a close or when focus is already in
  // the tablist, so pressing + never pulls the keyboard into the new tab.
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

/**
 * closeFocusArmed moves DOM focus after a tab close drops it to <body>.
 * A mouse click focuses the new tab natively (never :focus-visible), and
 * keyboard arrows keep focus inside the tablist, so the watcher below only
 * steps in for the close routes — the close removes the focused button and
 * focus would otherwise strand on <body> (JUS-206 round 2 keeps the
 * reference's ring-free tabs while the keyboard ring stays).
 */
let closeFocusArmed = false;

/** closeTabFromButton closes one tab from its × or middle-click. */
function closeTabFromButton(id: string): void {
  // JUS-207: arm the close refocus only when the close retires the active
  // tab. Closing an inactive tab leaves activeTabId untouched, so the flag
  // must not stay armed for a later switch from outside the tablist (the +
  // button) — that switch would otherwise steal keyboard focus into the tab.
  closeFocusArmed = id === files.activeTabId;
  void files.closeTab(id);
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
        closeTabFromButton(tab.id);
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
  closeTabFromButton(id);
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
 * rovingTabName is the entry that owns tabindex 0. It follows the focused row
 * when that row is RENDERED (the windowed slice plus any pinned row), so the
 * listing always has exactly one tab stop; when the roving row has scrolled out
 * of the rendered slice the stop falls back to the first rendered row, and to
 * null for an empty list. It never points at an unrendered row (that used to
 * leave the list with zero tab stops after a blur + scroll).
 */
const rovingTabName = computed<string | null>(() => {
  const entries = files.visibleEntries;
  if (entries.length === 0) {
    return null;
  }
  const rendered = files.viewMode === "grid" ? gridRows.value : listRows.value;
  if (
    rovingName.value !== null &&
    rendered.some((row) => row.entry.name === rovingName.value)
  ) {
    return rovingName.value;
  }
  return rendered[0]?.entry.name ?? null;
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

/** clearTypeTimer stops a pending type-to-focus reset timer. */
function clearTypeTimer(): void {
  if (typeTimer !== null) {
    clearTimeout(typeTimer);
    typeTimer = null;
  }
}

/**
 * resetTypeahead drops any pending type-to-focus run. It runs on unmount and
 * whenever the listing identity changes (folder/tab/root, filter, sort), so a
 * prefix typed in one view never matches into the next.
 */
function resetTypeahead(): void {
  clearTypeTimer();
  typeBuffer = "";
}

/** typeToFocus jumps the roving focus to the next typed-prefix match. */
function typeToFocus(char: string): void {
  clearTypeTimer();
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
 * focused row. It acts ONLY on a key whose target is inside a row (or a
 * row-owned control), so the sticky header's sort buttons, the select-all
 * checkbox and any other control inside the scroller keep their native
 * Enter/Space/arrow behaviour; keys typed into a field or a dialog/menu never
 * reach here either (the handler is on the list, and both are guarded). A row
 * control (checkbox, inline action, ⋮) keeps its native Enter/Space too, via
 * {@link isRowControl}.
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
  // Only a row target drives row keys: the header's sort buttons and the
  // select-all checkbox sit inside the scroller, so Enter/Space/arrows there
  // must pass straight through.
  if (!(target instanceof Element) || target.closest("[data-entry]") === null) {
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
    // The store resets the range anchor on a sort/filter/folder/tab change, so
    // the first Shift+Arrow after one would otherwise drop the origin row and
    // select only the target. Seed the anchor at the focused row first, so the
    // run always includes the row the cursor is on.
    if (files.activeTab.anchor === null && current >= 0) {
      files.setAnchor(files.visibleEntries[current]?.name ?? null);
    }
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
  // The flag describes THIS focus transition only: read it, then reset it, so a
  // later programmatic focus is not misread as the earlier click's.
  const pointer = lastFocusWasPointer;
  lastFocusWasPointer = false;
  if (name === null) {
    // Focus moved to an in-scroller control that is not a row (a header sort
    // button, the select-all checkbox): the pin no longer guards focus, so drop
    // the stale row instead of leaving focusedName/focusedEl pointing at it.
    releaseFocus();
    return;
  }
  focusedName.value = name;
  focusedEl = target;
  if (!restoringFocus && !pointer) {
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
// patch. The arm is keyed on the RENDERED-set identity, not the data slice, so a
// data-only change that re-renders nothing never arms it. A keyed window change
// moves or unmounts the focused node and Chromium blurs it; `restorePending`
// (armed pre-flush, before the patch) is what tells that blur apart from a user
// press on the very next focusout. The post-flush consumer runs whether or not
// the component re-rendered, so the arm can never go stale. A pointerdown/keydown
// clears the arm first, so a user gesture wins.
watch(
  renderedKeys,
  () => {
    restorePending = true;
  },
  { flush: "pre" },
);
watch(
  renderedKeys,
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

// A directory, tab or root change, a filter or a sort moves every row: the
// focused/menu name from the previous listing must not survive (it could pin a
// same-named entry there), and neither must the roving tab stop or a pending
// type-to-focus run (JUS-139) — a same-named entry in the new view would
// otherwise inherit the stop.
watch(
  () =>
    [
      files.activeTabId,
      files.rootName,
      files.path,
      files.query,
      files.sortKey,
      files.sortDir,
    ] as const,
  () => {
    releaseFocus();
    closeRowContext();
    rovingName.value = null;
    resetTypeahead();
  },
);

// The pinned entry itself disappearing (deleted, renamed, filtered out) releases
// the pin rather than leaving a name that a same-named entry could match later.
watch(pinnedIndex, (index) => {
  if (index === -1) {
    releaseFocus();
    closeRowContext();
  }
});

// Either variant of the header row can hide with the selection: the bulk bar
// when the last row is unchecked (Clear, or the bar collapsing), the column
// headers when the first row is selected (the bar replaces them). Both are
// hidden with `visibility` — not removed — so Chromium blurs the focused
// control and focus falls to <body>; hand it to the roving row instead
// (JUS-140/146). The pre-flush watcher reads who held focus BEFORE the swap; the
// post-flush one moves it, keeping the JUS-139 one-tab-stop rule (focusEntry
// targets the roving row). The leading select-all checkbox lives outside both
// variants, so focusing it is unaffected.
let swapHadFocus = false;
watch(
  hasSelection,
  (active, previous) => {
    if (previous === active) {
      return;
    }
    // The variant about to hide: the column headers when the selection appears,
    // the whole bulk bar when it clears. The selector must cover BOTH hosts of
    // the selection bar — the list's `.fm-head-bulk` (count + sort + actions) and
    // the grid's bar inside the toolbar, which only exposes `.fm-selection-actions`
    // (JUS-148 fix round 1: narrowing it to `.fm-head-bulk` dropped grid focus to
    // <body>).
    const hiding = active ? ".fm-head-cols" : ".fm-head-bulk, .fm-selection-actions";
    swapHadFocus = Boolean(document.activeElement?.closest(hiding));
  },
  { flush: "pre" },
);
watch(
  hasSelection,
  async (active, previous) => {
    if (previous === active || !swapHadFocus) {
      return;
    }
    swapHadFocus = false;
    await nextTick();
    focusEntry(rovingTabName.value);
  },
  { flush: "post" },
);

// Move focus to the active tab only when the switch needs it (JUS-206 round 2;
// see the watcher body).
watch(() => files.activeTabId, async () => {
  await nextTick();
  const active = tabListEl.value?.querySelector<HTMLElement>(
    '[role="tab"][aria-selected="true"]',
  );
  if (!active || document.activeElement === active) {
    // A mouse click focuses the tab natively (never :focus-visible, so no
    // ring); a fresh page leaves focus alone too. Only a keyboard switch
    // (focus still inside the tablist) or a close (focus dropped to <body>,
    // armed by the close routes) moves DOM focus — JUS-206 round 2 keeps the
    // reference's ring-free tabs while the keyboard ring stays (JUS-152).
    return;
  }
  if (tabListEl.value?.contains(document.activeElement) === true) {
    await focusActiveTab();
    return;
  }
  if (closeFocusArmed) {
    closeFocusArmed = false;
    await focusActiveTab();
  }
});

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

/** fileOpsOpen tracks the File Operations dropdown disclosure state (aria-expanded). */
const fileOpsOpen = ref(false);

/**
 * fileOpsSelection are the writable-safe selected entries the toolbar's File
 * Operations dropdown acts on — the same set the bulk bar (FilesSelectionBar)
 * uses, so both surfaces target identical entries (JUS-160).
 */
const fileOpsSelection = computed<FilesEntry[]>(() =>
  files.selectedEntries.filter(isSelectable),
);

/** fileOpsExtractTarget is the first selected extractable archive, or null. */
const fileOpsExtractTarget = computed<FilesEntry | null>(
  () =>
    fileOpsSelection.value.find((entry) => isExtractableArchive(entry)) ??
    null,
);

/** fileOpsPermissionsTarget is the first selected chmod-able entry, or null. */
const fileOpsPermissionsTarget = computed<FilesEntry | null>(
  () =>
    fileOpsSelection.value.find(
      (entry) => entry.kind === "file" || entry.kind === "directory",
    ) ?? null,
);

/**
 * fileOpsOptions lists the selection-scoped file operations (JUS-160): cut,
 * copy, paste (into the directory on screen), compress, extract and
 * permissions. Disabled states mirror the row menu (write-gated, paste validity
 * from the store) and the bulk bar (a selectable selection, no mutation
 * running), so no surface offers an action another would refuse.
 */
const fileOpsOptions = computed(() => {
  const names = fileOpsSelection.value.map((entry) => entry.name);
  const canAct = files.canWrite && !files.acting && names.length > 0;
  const item = (label: string, key: string, disabled: boolean) => ({
    label,
    key,
    disabled,
    props: { role: "menuitem" },
  });
  return [
    item(String(t("files.actions.cut")), "cut", !canAct),
    item(String(t("files.actions.copy")), "copy", !canAct),
    // Paste lands in the directory on screen; its label carries the blocked
    // reason exactly like the old toolbar button ({@link pasteLabel}). With an
    // empty clipboard there is nothing to paste, so it stays disabled too.
    item(
      files.hasClipboard
        ? pasteLabel.value
        : String(t("files.actions.paste")),
      "paste",
      files.acting || !files.hasClipboard || files.pasteProblem !== null,
    ),
    item(String(t("files.actions.compress")), "compress", !canAct),
    item(
      String(t("files.actions.extract")),
      "extract",
      !canAct || fileOpsExtractTarget.value === null,
    ),
    item(
      String(t("files.menu.permissions")),
      "permissions",
      !canAct || fileOpsPermissionsTarget.value === null,
    ),
  ];
});

/** onFileOps dispatches one File Operations dropdown action on the selection. */
function onFileOps(key: string): void {
  const names = fileOpsSelection.value.map((entry) => entry.name);
  switch (key) {
    case "cut":
      if (names.length > 0) {
        files.cutSelection(names);
      }
      break;
    case "copy":
      if (names.length > 0) {
        files.copySelection(names);
      }
      break;
    case "paste":
      void pasteAction();
      break;
    case "compress":
      openCompress(fileOpsSelection.value);
      break;
    case "extract": {
      const target = fileOpsExtractTarget.value;
      if (target !== null) {
        openExtract(target);
      }
      break;
    }
    case "permissions": {
      const target = fileOpsPermissionsTarget.value;
      if (target !== null) {
        openPermissions(target);
      }
      break;
    }
  }
}

/**
 * JUS-145: the list headers carry the sort controls, but the compact layout
 * hides the Modified / Owner-Mode headers — and the grid has no headers at
 * all — so this toolbar control keeps every column sortable there. It is shown
 * only when the headers are hidden and mirrors the active column + direction.
 */
const sortMenuOpen = ref(false);
/**
 * bulkSortMenuOpen is the bulk bar's sort dropdown open state, kept separate
 * from `sortMenuOpen` (the toolbar toggle's) so the two can never open together
 * if a future layout ever mounts both (JUS-148 fix round 1).
 */
const bulkSortMenuOpen = ref(false);
const isCompactList = useMediaQuery("(max-width: 860px)");
/**
 * showSortControl keeps the toolbar sort control reachable exactly where the
 * column headers cannot carry the sort: the compact layout (headers hidden
 * ≤860px) and the grid (no headers at all). It is RENDERED only in those states
 * (v-if), never merely reserved — a hidden control must not occupy a toolbar row
 * (JUS-147). It never depends on the selection, so selecting never reflows the
 * toolbar (JUS-130/134); while a selection hides the headers the bulk bar
 * carries the sort instead.
 */
const showSortControl = computed<boolean>(
  () => isCompactList.value || files.viewMode === "grid",
);

/**
 * columnCount is the number of columns the table exposes to assistive tech
 * (JUS-148): the leading select-all cell plus the columns not collapsed at this
 * width. It keeps aria-colcount and the bulk bar's aria-colspan honest against
 * the rendered body rows. JUS-161: the desktop table is select + name +
 * owner/mode + size + modified + actions.
 */
const columnCount = computed<number>(() => {
  if (isCompactList.value) {
    return 4; // select + name + size + actions
  }
  return 6;
});

/** sortColumnLabel maps a sort key to its column header copy. */
function sortColumnLabel(key: FilesSortKey): string {
  switch (key) {
    case "name":
      return String(t("files.browser.columnName"));
    case "size":
      return String(t("files.browser.columnSize"));
    case "kind":
      return String(t("files.browser.columnType"));
    case "modified":
      return String(t("files.browser.columnModified"));
    case "mode":
      return String(t("files.browser.columnMode"));
  }
}

/** sortDirection is the localized active sort direction. */
const sortDirection = computed<string>(() =>
  String(
    t(
      files.sortDir === "asc"
        ? "files.browser.sortAscending"
        : "files.browser.sortDescending",
    ),
  ),
);

/** sortLabel mirrors the active sort in the control's visible + accessible name. */
const sortLabel = computed<string>(
  () =>
    `${t("files.browser.sortBy")}: ${sortColumnLabel(files.sortKey)} — ${sortDirection.value}`,
);

/**
 * sortMenuOptions lists the five sortable columns plus both directions, with
 * the active column and direction checked. Choosing a different column goes
 * through the store's setSort (which resets the range anchor); re-choosing the
 * active column is a no-op so the direction only changes from a direction item.
 */
const sortMenuOptions = computed(() => {
  const check = (): ReturnType<typeof h> => h(Check, { size: 14, "aria-hidden": "true" });
  return [
    ...(["name", "size", "kind", "modified", "mode"] as FilesSortKey[]).map(
      (key) => ({
        label: sortColumnLabel(key),
        key: `key:${key}`,
        icon: key === files.sortKey ? check : undefined,
        props: { role: "menuitem" },
      }),
    ),
    { type: "divider" as const, key: "divider" },
    {
      label: String(t("files.browser.sortAscending")),
      key: "dir:asc",
      icon: files.sortDir === "asc" ? check : undefined,
      props: { role: "menuitem" },
    },
    {
      label: String(t("files.browser.sortDescending")),
      key: "dir:desc",
      icon: files.sortDir === "desc" ? check : undefined,
      props: { role: "menuitem" },
    },
  ];
});

/** onSortMenu applies a sort-menu choice (a column, or a direction). */
function onSortMenu(key: string): void {
  if (key === "dir:asc") {
    files.sortDir = "asc";
    return;
  }
  if (key === "dir:desc") {
    files.sortDir = "desc";
    return;
  }
  if (key.startsWith("key:")) {
    const next = key.slice(4) as FilesSortKey;
    if (next !== files.sortKey) {
      files.setSort(next);
    }
  }
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

/** viewerOpen/viewerEntry own the image viewer (JUS-204). */
const viewerOpen = ref(false);
const viewerEntry = ref<FilesEntry | null>(null);

/** openViewer shows one image in the viewer (owner/admin only). */
function openViewer(entry: FilesEntry): void {
  if (entry.kind !== "file" || !files.canReadContent) {
    return;
  }
  viewerEntry.value = entry;
  viewerOpen.value = true;
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
    case "view":
      openViewer(entry);
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

/** rowMenuOpenName is the row whose ⋮ dropdown is open (aria-expanded). */
const rowMenuOpenName = ref<string | null>(null);

/**
 * closeRowContext closes the context / Shift+F10 row menu and releases its pin.
 * The context menu is a separate controlled dropdown that shares the row's
 * `rowMenu` options, so it must be closed through the same reset path.
 */
function closeRowContext(): void {
  contextOpen.value = false;
  contextEntry.value = null;
  closeRowMenu();
}

/**
 * resetRowMenu closes EVERY row-menu surface — the ⋮ dropdown, the context /
 * Shift+F10 menu, and the menu pin — because they all show the same `rowMenu`
 * options for one row. It runs on a listing LAYOUT change (view-mode flip,
 * folder / tab / root change) and when the open row leaves the rendered set, so
 * a menu can never survive into a listing where it would act on the wrong file.
 *
 * A reorder / filter that keeps the same row mounted is deliberately excluded:
 * the ⋮ dropdown stays open with `aria-expanded="true"` (documented keep-open).
 * The context menu, whose pin cannot survive a reorder, is closed separately by
 * the listing-identity watcher via {@link closeRowContext}.
 */
function resetRowMenu(): void {
  rowMenuOpenName.value = null;
  closeRowContext();
}

/**
 * layoutKey identifies the listing LAYOUT — a list↔grid flip or a folder / tab /
 * root change — as opposed to a reorder / filter that keeps the same rows. It is
 * bound as the per-row ⋮ dropdown's `key`, so a layout change remounts the
 * uncontrolled dropdowns closed (they have no imperative close) while the
 * watcher clears the tracked state, keeping the popup and `aria-expanded` in
 * step (JUS-140).
 */
const layoutKey = computed<string>(
  () =>
    `${files.viewMode}:${files.activeTabId}:${files.rootName}:${files.path}`,
);
watch(layoutKey, resetRowMenu);

/**
 * resetSortMenu clears the sort dropdowns' tracked open state. The NDropdowns are
 * controlled through `v-model:show`, so clearing these refs closes them; a layout
 * change that remounts a trigger (a compact↔desktop or list↔grid flip) or hides
 * the bulk bar (all selections cleared) would otherwise leave one open — and the
 * remounted trigger would reopen its menu by itself (JUS-148, mirroring
 * resetRowMenu).
 */
function resetSortMenu(): void {
  sortMenuOpen.value = false;
  bulkSortMenuOpen.value = false;
}
watch([layoutKey, showSortControl, hasSelection], resetSortMenu);

// The open row can leave the rendered set without its dropdown closing (drawn
// out of the window, deleted or renamed by a refresh): close the menus as soon
// as that row is no longer rendered, so a later render of the same name is never
// stale. Both surfaces count — the ⋮ dropdown and the context menu.
watch(renderedKeys, () => {
  const openName =
    rowMenuOpenName.value ??
    (contextOpen.value ? (contextEntry.value?.name ?? null) : null);
  if (openName === null) {
    return;
  }
  const rendered = files.viewMode === "grid" ? gridRows.value : listRows.value;
  if (!rendered.some((row) => row.entry.name === openName)) {
    resetRowMenu();
  }
});

/**
 * isRowMenuOpen reports whether a row's menu is open by EITHER trigger — the ⋮
 * dropdown or the right-click / Shift+F10 context route (they show the same
 * menu), so the ⋮'s aria-expanded reflects the real open state (JUS-140).
 */
function isRowMenuOpen(entry: FilesEntry): boolean {
  return (
    rowMenuOpenName.value === entry.name ||
    (contextOpen.value && contextEntry.value?.name === entry.name)
  );
}

/** onRowMenuShow tracks one row's ⋮ dropdown open/close (Escape, click-away, select). */
function onRowMenuShow(name: string, show: boolean): void {
  rowMenuOpenName.value = show
    ? name
    : rowMenuOpenName.value === name
      ? null
      : rowMenuOpenName.value;
}

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
  resetTypeahead();
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
    role="region"
    :aria-label="$t('files.browser.heading')"
    :class="{ 'is-dragging': dragActive }"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
  >
    <!-- Header band (JUS-206 round 4): browser-style tabs on the left, the
         agent-node switcher at the right end of the same row. -->
    <div class="fm-topbar">
    <!-- Tab bar (JUS-123/126): one tab per open directory. A middle-click or
         the × closes a tab; the + lives at the strip's end in the tab flow and
         stays visible there while tabs scroll under it (sticky), so + is
         always reachable. The tablist owns only role=tab buttons (the × is a
         span inside the tab) so it satisfies aria-required-children; keyboard
         users close with Delete. -->
    <div class="fm-tabs">
      <div ref="tabListEl" class="fm-tabstrip">
        <!-- The tablist owns only the tabs (the + stays a sibling inside the
             scroll strip so it can stick to the visible end); the strip
             itself carries no role. -->
        <div class="fm-tablist" role="tablist" :aria-label="$t('files.tabs.list')">
        <button
          v-for="tab in files.tabs"
          :key="tab.id"
          type="button"
          class="fm-tab"
          :class="{
            'is-active': tab.id === files.activeTabId,
            'is-narrow': narrowTabs.has(tab.id),
          }"
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
          <Folder class="fm-tab-icon" :size="16" :stroke-width="1.8" aria-hidden="true" />
          <span class="fm-tab-name">{{ tabLabel(tab) }}</span>
          <span
            v-if="files.tabs.length > 1"
            class="fm-tab-close"
            aria-hidden="true"
            :title="$t('files.tabs.close', { name: tabLabel(tab) })"
            @click.stop="closeTabFromButton(tab.id)"
          >
            <X :size="12" :stroke-width="2" aria-hidden="true" />
          </span>
          <span
            v-if="tab.id === files.activeTabId"
            class="fm-tab-corner"
            aria-hidden="true"
          />
        </button>
        </div>
        <AppTooltip :label="$t('files.tabs.new')">
          <button
            type="button"
            class="fm-tab-add"
            :disabled="files.rootName === ''"
            :aria-label="$t('files.tabs.new')"
            @click="openNewTab()"
          >
            <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
          </button>
        </AppTooltip>
      </div>
    </div>
      <div class="fm-contextbar">
        <slot name="header-tools" />
      </div>
    </div>

    <!-- Navigation row (JUS-159/JUS-206): one Naive NInputGroup row merges
         back, breadcrumb and an attached refresh button (32px); long paths
         truncate from the left (the crumb strip clips at its start edge). The
         right side of the row keeps the folder filter + JUS-163 search. -->
    <div class="fm-bar fm-pathbar">
      <NInputGroup class="fm-navfield">
        <AppTooltip :label="$t('files.browser.parent')">
          <NButton
            size="small"
            class="fm-navback"
            :disabled="files.path === ''"
            :aria-label="$t('files.browser.parent')"
            @click="files.goUp()"
          >
            <template #icon>
              <ArrowUp :size="16" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
        </AppTooltip>
        <nav class="fm-path fm-navcrumbs" :aria-label="$t('files.browser.crumbs')">
          <template v-for="(crumb, index) in files.breadcrumb" :key="crumb.path">
            <ChevronRight v-if="index > 0" class="sep" :size="14" :stroke-width="1.8" aria-hidden="true" />
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
        <AppTooltip :label="$t('files.browser.refresh')">
          <NButton
            size="small"
            class="fm-navrefresh"
            :loading="files.loading"
            :aria-label="$t('files.browser.refresh')"
            @click="files.refresh()"
          >
            <template #icon>
              <RefreshCw :size="16" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
        </AppTooltip>
      </NInputGroup>

      <div class="fm-search-group">
        <NInput
          v-model:value="filterModel"
          class="fm-search"
          size="small"
          clearable
          :placeholder="recursiveModel ? $t('files.search.placeholder') : $t('files.browser.filter')"
          :aria-label="recursiveModel ? $t('files.search.placeholder') : $t('files.browser.filter')"
          @keydown="onSearchKeydown"
        >
          <template #prefix>
            <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NInput>
        <AppTooltip :label="$t('files.search.includeSubdirs')">
          <NCheckbox v-model:checked="recursiveModel" size="small" class="fm-search-recursive">
            {{ $t("files.search.includeSubdirs") }}
          </NCheckbox>
        </AppTooltip>
        <AppTooltip :label="$t('files.search.action')">
          <NButton
            size="small"
            secondary
            class="fm-search-submit"
            :aria-label="$t('files.search.action')"
            @click="submitSearch"
          >
            <template #icon>
              <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
        </AppTooltip>
      </div>
    </div>

    <!-- Toolbar (JUS-160): the left group carries Upload, the New menu, a
         separator, the Root selector and the File Operations dropdown (cut,
         copy, paste, compress, extract, permissions); the right group pins
         the recycle bin and the list/grid toggle to the right edge.
         Favorite/Terminal slots stay hidden until those features are approved
         (no buttons rendered). Both groups always have content, so the toolbar
         can never wrap onto an empty second row (JUS-147). Labels collapse to
         icon-only <=640px, each keeping a tooltip + aria-label.
         JUS-208 round 2: in grid view the selection cluster overlays this row
         while a selection exists (see .fm-grid-bulk below) instead of joining
         it, so selecting never grows the toolbar or shifts the tiles. -->
    <div class="fm-bar fm-toolbar" :class="{ 'is-grid': files.viewMode === 'grid' }">
      <div class="fm-toolbar-scroll">
        <div class="fm-toolbar-group">
          <AppTooltip v-if="files.canWrite" :label="$t('files.actions.upload')">
            <NButton
              size="small"
              :disabled="files.acting"
              :aria-label="$t('files.actions.upload')"
              @click="openUpload()"
            >
              <template #icon>
                <Upload :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              <span class="fm-btn-label">{{ $t("files.actions.upload") }}</span>
            </NButton>
          </AppTooltip>
          <NDropdown
            v-if="files.canWrite"
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
            <AppTooltip :label="$t('files.actions.new')">
              <NButton
                size="small"
                aria-haspopup="menu"
                :aria-expanded="newMenuOpen"
                :disabled="files.acting"
                :aria-label="$t('files.actions.new')"
              >
                <template #icon>
                  <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                <span class="fm-btn-label">{{ $t("files.actions.new") }}</span>
                <ChevronDown class="fm-caret" :size="12" :stroke-width="1.8" aria-hidden="true" />
              </NButton>
            </AppTooltip>
          </NDropdown>
          <span v-if="files.canWrite" class="fm-toolbar-sep" aria-hidden="true" />
          <span class="small muted fm-field-label">{{ $t("files.browser.rootLabel") }}</span>
          <NSelect
            v-model:value="selectedRoot"
            class="fm-root-select"
            size="small"
            :options="rootOptions"
            :aria-label="$t('files.browser.rootLabel')"
          />
          <NDropdown
            v-model:show="fileOpsOpen"
            trigger="click"
            placement="bottom-start"
            :options="fileOpsOptions"
            :menu-props="() => ({
              role: 'menu',
              'aria-label': $t('files.actions.fileOperations'),
            })"
            @select="onFileOps"
          >
            <AppTooltip :label="$t('files.actions.fileOperations')">
              <NButton
                size="small"
                aria-haspopup="menu"
                :aria-expanded="fileOpsOpen"
                :disabled="files.acting"
                :aria-label="$t('files.actions.fileOperations')"
              >
                <template #icon>
                  <MoreVertical :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                <span class="fm-btn-label">{{ $t("files.actions.fileOperations") }}</span>
                <ChevronDown class="fm-caret" :size="12" :stroke-width="1.8" aria-hidden="true" />
              </NButton>
            </AppTooltip>
          </NDropdown>
          <span v-if="files.hasClipboard" class="small muted clipboard-hint">
            {{ files.clipboardMode === "cut" ? $t("files.clipboard.cut") : $t("files.clipboard.copy") }}
          </span>
          <!-- JUS-145/147: compact sort control, RENDERED only where the column
               headers cannot carry the sort — the compact layout (Modified / Owner-Mode
               headers hidden) and the grid (no headers at all). It is never
               reserved while hidden, so it can never tip the first toolbar row onto
               an empty second row (JUS-147); while a selection hides the headers the
               bulk bar carries its own sort control (below). -->
          <NDropdown
            v-if="showSortControl"
            v-model:show="sortMenuOpen"
            trigger="click"
            placement="bottom-end"
            :options="sortMenuOptions"
            :menu-props="() => ({
              role: 'menu',
              'aria-label': $t('files.browser.sortBy'),
            })"
            @select="onSortMenu"
          >
            <AppTooltip :label="sortLabel">
              <NButton
                size="small"
                class="fm-sort-toggle"
                aria-haspopup="menu"
                :aria-expanded="sortMenuOpen"
                :aria-label="sortLabel"
              >
                <template #icon>
                  <ArrowUp v-if="files.sortDir === 'asc'" :size="14" :stroke-width="1.8" aria-hidden="true" />
                  <ArrowDown v-else :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                <span class="fm-btn-label">{{ sortColumnLabel(files.sortKey) }}</span>
              </NButton>
            </AppTooltip>
          </NDropdown>
        </div>

        <div class="fm-toolbar-group fm-toolbar-end">
          <!-- The bin belongs to the root, not the folder: any member may open
               it, while restore/purge inside need write. -->
          <AppTooltip :label="$t('files.browser.recycleBin')">
            <NButton
              size="small"
              :disabled="files.rootName === ''"
              :aria-label="$t('files.browser.recycleBin')"
              @click="recycleOpen = true"
            >
              <template #icon>
                <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              <span class="fm-btn-label">{{ $t("files.browser.recycleBin") }}</span>
            </NButton>
          </AppTooltip>
          <span class="fm-toolbar-sep" aria-hidden="true" />
          <span class="segmented" role="group">
            <AppTooltip :label="$t('files.browser.viewList')">
              <button
                type="button"
                class="seg-btn"
                :class="{ 'is-active': files.viewMode === 'list' }"
                :aria-label="$t('files.browser.viewList')"
                :aria-pressed="files.viewMode === 'list'"
                @click="files.setViewMode('list')"
              >
                <List :size="15" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </AppTooltip>
            <AppTooltip :label="$t('files.browser.viewGrid')">
              <button
                type="button"
                class="seg-btn"
                :class="{ 'is-active': files.viewMode === 'grid' }"
                :aria-label="$t('files.browser.viewGrid')"
                :aria-pressed="files.viewMode === 'grid'"
                @click="files.setViewMode('grid')"
              >
                <LayoutGrid :size="15" :stroke-width="1.8" aria-hidden="true" />
              </button>
            </AppTooltip>
          </span>
        </div>
      </div>

      <!-- JUS-208 round 2: the grid has no column-header row for the bulk bar
           to replace (as in list view), so while a selection exists the shared
           cluster overlays this toolbar row as a contextual action bar instead
           of joining it: the toolbar keeps its single 32px row and the tiles
           never shift. Idle it is visibility-hidden and inert (out of the a11y
           tree and tab order). The overlay itself never takes pointer events —
           only its action row does — so every covered toolbar control keeps
           working exactly as before (hover, focus, menus); the bar is purely a
           visual layer, never a modal one. -->
      <div
        v-if="files.viewMode === 'grid'"
        class="fm-grid-bulk"
        :class="{ 'is-visible': gridBulkVisible }"
        :inert="!gridBulkVisible"
      >
        <FilesSelectionBar
          :count-reserve="countReserve"
          @download="() => void downloadSelected()"
          @compress="openCompress"
          @remove="openDelete"
          @clear="files.clearSelection()"
        />
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

    <!-- Search results (JUS-163): while a name search is on screen it takes
         over the content region. Any navigation or Clear ends it. -->
    <FilesSearchResults
      v-else-if="files.searchActive"
      :results="files.searchResults"
      :truncated="files.searchTruncated"
      :loading="files.searchLoading"
      :query="files.searchQuery"
      :path="files.path"
      @open="files.openSearchHit($event)"
      @clear="files.clearSearch()"
    />

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
          :aria-colcount="columnCount"
        >
          <div
            ref="headEl"
            class="fm-row fm-head"
            role="row"
            :aria-rowindex="1"
            :style="{ '--fm-scroller-w': scrollerWidthPx }"
          >
            <span class="fm-select" role="columnheader">
              <NCheckbox
                size="small"
                :checked="files.allVisibleSelected"
                :indeterminate="files.someVisibleSelected"
                :aria-label="$t('files.browser.selectAll')"
                @update:checked="files.toggleSelectAll()"
              />
            </span>
            <!-- JUS-146: the sticky header row swaps between the column
                 headers and the bulk bar while entries are selected. Both
                 variants are always mounted and overlap the SAME grid cells
                 (one row), so the row's height is max(columns, bar) in every
                 selection state — selecting never reflows the list (JUS-130/
                 134) and no row is permanently reserved. The variants hide
                 with visibility, so hidden controls are neither focusable nor
                 announced; the JUS-140 focus hand-off still finds the focused
                 bulk control. The leading select-all checkbox stays visible in
                 both states, so select-all/deselect-all stay reachable; the
                 sort buttons (and their aria-sort) come back on Clear. -->
            <div
              class="fm-head-cols"
              :class="{ 'is-hidden': hasSelection }"
              role="presentation"
            >
              <div class="fm-name" role="columnheader" :aria-sort="ariaSort('name')">
                <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'name' }" @click="files.setSort('name')">
                  {{ $t("files.browser.columnName") }}
                  <ChevronDown v-if="files.sortKey === 'name'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
                </button>
              </div>
              <!-- JUS-161: Name, Owner/Mode, Size, Modified, Operation. The
                   cell shows owner/mode (JUS-214); the owner is empty from an
                   older agent, so the cell falls back to the octal mode. -->
              <div class="fm-cell--mode" role="columnheader" :aria-sort="ariaSort('mode')">
                <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'mode' }" @click="files.setSort('mode')">
                  {{ $t("files.browser.columnMode") }}
                  <ChevronDown v-if="files.sortKey === 'mode'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
                </button>
              </div>
              <div role="columnheader" class="fm-cell--size" :aria-sort="ariaSort('size')">
                <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'size' }" @click="files.setSort('size')">
                  {{ $t("files.browser.columnSize") }}
                  <ChevronDown v-if="files.sortKey === 'size'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
                </button>
              </div>
              <div class="fm-cell--modified" role="columnheader" :aria-sort="ariaSort('modified')">
                <button type="button" class="fm-sort" :class="{ 'is-active': files.sortKey === 'modified' }" @click="files.setSort('modified')">
                  {{ $t("files.browser.columnModified") }}
                  <ChevronDown v-if="files.sortKey === 'modified'" class="fm-sort-arrow" :class="{ 'is-desc': files.sortDir === 'desc' }" :size="12" :stroke-width="1.8" aria-hidden="true" />
                </button>
              </div>
              <div role="columnheader" class="right fm-actions-cell">{{ $t("files.browser.columnActions") }}</div>
            </div>

            <!-- The bulk bar (the former toolbar cluster, JUS-128/134/146):
                 it replaces the column headers in the same sticky row while
                 entries are selected. It is always mounted (hidden with
                 visibility), so the row height stays put and the JUS-141 count
                 reserve keeps the count cell content-derived. Its count is
                 announced politely (aria-live). JUS-148: it spans every column
                 but the leading select-all cell, so aria-colspan keeps the row's
                 semantics consistent with the body rows. -->
            <div
              class="fm-head-bulk"
              :class="{ 'is-visible': hasSelection }"
              role="columnheader"
              :aria-colspan="columnCount - 1"
            >
              <!-- JUS-147: while the bar replaces the headers the sort must live
                   here. It is the desktop-list affordance only (≥861px); the
                   compact layout and the grid keep the toolbar control above, so
                   exactly one sort control is on screen at every width. -->
              <NDropdown
                v-if="!isCompactList"
                v-model:show="bulkSortMenuOpen"
                trigger="click"
                placement="bottom-end"
                :options="sortMenuOptions"
                :menu-props="() => ({
                  role: 'menu',
                  'aria-label': $t('files.browser.sortBy'),
                })"
                @select="onSortMenu"
              >
                <AppTooltip :label="sortLabel">
                  <NButton
                    size="small"
                    secondary
                    class="fm-bulk-sort"
                    aria-haspopup="menu"
                    :aria-expanded="bulkSortMenuOpen"
                    :aria-label="sortLabel"
                  >
                    <template #icon>
                      <ArrowUp v-if="files.sortDir === 'asc'" :size="14" :stroke-width="1.8" aria-hidden="true" />
                      <ArrowDown v-else :size="14" :stroke-width="1.8" aria-hidden="true" />
                    </template>
                    <span class="fm-btn-label">{{ sortColumnLabel(files.sortKey) }}</span>
                  </NButton>
                </AppTooltip>
              </NDropdown>
              <FilesSelectionBar
                :count-reserve="countReserve"
                @download="() => void downloadSelected()"
                @compress="openCompress"
                @remove="openDelete"
                @clear="files.clearSelection()"
              />
            </div>
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
                :aria-describedby="rowDescribedBy(entry)"
              >
                <b>{{ entry.name }}</b>
              </button>
              <!-- JUS-134/148: on the desktop row the name and its (optional)
                   symlink target share one line (`.fm-name-sub` is
                   `display: contents` there). On the compact row the target
                   moves to the secondary line beside the relative modified time,
                   so the name keeps priority instead of both truncating
                   ("br… → /none…"). -->
              <span class="fm-name-sub">
                <span v-if="entryTarget(entry)" class="fm-target" :title="entryTarget(entry)">
                  {{ entryTarget(entry) }}
                </span>
                <span class="fm-row-sub">{{ modifiedLabel(entry) }}</span>
              </span>
            </div>
            <div class="fm-cell fm-mode fm-cell--mode" role="cell" :title="formatMode(entry.mode)">{{ ownerModeLabel(entry) }}</div>
            <div class="fm-cell fm-cell--size" role="cell">
              <FolderSizeCell
                v-if="entry.kind === 'directory'"
                :entry="entry"
                :path="joinPath(files.path, entry.name)"
              />
              <template v-else>{{ formatBytes(entry.size) }}</template>
            </div>
            <div class="fm-cell fm-date fm-cell--modified" role="cell" :title="formatTimestamp(entry.modified_at)">{{ modifiedLabel(entry) }}</div>
            <div class="fm-actions" role="cell">
              <!-- The inline actions (.fm-action) stay hover/focus-revealed on
                   pointer devices; the ⋮ menu button below stays visible
                   whenever the row has actions, so touch and keyboard users
                   reach the row menu without a hover (JUS-133). -->
              <AppTooltip
                v-if="entry.kind === 'directory'"
                :label="$t('files.browser.openFolder', { name: entry.name })"
              >
                <NButton
                  class="fm-action"
                  size="tiny"
                  quaternary
                  :aria-label="$t('files.browser.openFolder', { name: entry.name })"
                  @click="files.openDirectory(entry.name)"
                >
                  <template #icon>
                    <ChevronRight :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </AppTooltip>
              <AppTooltip
                v-else-if="entry.kind === 'file'"
                :label="downloadActionLabel(entry)"
              >
                <NButton
                  class="fm-action"
                  size="tiny"
                  quaternary
                  :aria-disabled="!files.canReadContent"
                  :aria-label="$t('files.browser.downloadFile', { name: entry.name })"
                  @click="startDownload(entry)"
                >
                  <template #icon>
                    <Download :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </AppTooltip>
              <AppTooltip
                v-if="files.canWrite && isExtractableArchive(entry)"
                :label="$t('files.actions.extract')"
              >
                <NButton
                  class="fm-action"
                  size="tiny"
                  quaternary
                  :aria-label="$t('files.actions.extract')"
                  @click="openExtract(entry)"
                >
                  <template #icon>
                    <ArchiveRestore :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </AppTooltip>
              <AppTooltip
                v-if="files.canWrite && isSelectable(entry)"
                :label="$t('files.browser.renameEntry', { name: entry.name })"
              >
                <NButton
                  class="fm-action"
                  size="tiny"
                  quaternary
                  :aria-label="$t('files.browser.renameEntry', { name: entry.name })"
                  @click="openRename(entry)"
                >
                  <template #icon>
                    <Pencil :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </AppTooltip>
              <AppTooltip
                v-if="files.canWrite && isSelectable(entry)"
                :label="$t('files.browser.deleteEntry', { name: entry.name })"
              >
                <NButton
                  class="fm-action"
                  size="tiny"
                  quaternary
                  :aria-label="$t('files.browser.deleteEntry', { name: entry.name })"
                  @click="openDelete([entry])"
                >
                  <template #icon>
                    <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </AppTooltip>
              <!-- A row with no actions (a special/unspecified entry) has no
                   menu, so there is deliberately no ⋮ to open an empty menu. -->
              <NDropdown
                v-if="rowMenu(entry).length > 0"
                :key="`${layoutKey}:${entry.name}`"
                trigger="click"
                placement="bottom-end"
                :options="rowMenu(entry)"
                :menu-props="() => ({
                  role: 'menu',
                  'aria-label': $t('files.browser.moreActions', { name: entry.name }),
                })"
                @update:show="(show: boolean) => onRowMenuShow(entry.name, show)"
                @select="(key: string) => onRowMenu(key, entry)"
              >
                <AppTooltip :label="$t('files.browser.moreActions', { name: entry.name })">
                  <NButton
                    size="tiny"
                    quaternary
                    aria-haspopup="menu"
                    :aria-expanded="isRowMenuOpen(entry)"
                    :aria-label="$t('files.browser.moreActions', { name: entry.name })"
                  >
                    <template #icon>
                      <MoreVertical :size="14" :stroke-width="1.8" aria-hidden="true" />
                    </template>
                  </NButton>
                </AppTooltip>
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
        role="list"
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
          role="listitem"
          :aria-posinset="index + 1"
          :aria-setsize="files.visibleEntries.length"
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
            :aria-describedby="rowDescribedBy(entry)"
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
          <!-- JUS-145: the tile's ⋮ row menu — the same trigger contract as the
               list (aria-haspopup/expanded, one open at a time, closes on a
               layout change), as a sibling of the tile button (never nested
               inside it) so a tap opens the menu without a hover. -->
          <div v-if="rowMenu(entry).length > 0" class="fm-tile-actions">
            <NDropdown
              :key="`${layoutKey}:${entry.name}`"
              trigger="click"
              placement="bottom-end"
              :options="rowMenu(entry)"
              :menu-props="() => ({
                role: 'menu',
                'aria-label': $t('files.browser.moreActions', { name: entry.name }),
              })"
              @update:show="(show: boolean) => onRowMenuShow(entry.name, show)"
              @select="(key: string) => onRowMenu(key, entry)"
            >
              <AppTooltip :label="$t('files.browser.moreActions', { name: entry.name })">
                <NButton
                  size="small"
                  quaternary
                  aria-haspopup="menu"
                  :aria-expanded="isRowMenuOpen(entry)"
                  :aria-label="$t('files.browser.moreActions', { name: entry.name })"
                >
                  <template #icon>
                    <MoreVertical :size="16" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                </NButton>
              </AppTooltip>
            </NDropdown>
          </div>
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

    <!-- One shared, visually hidden description target for the roving row's
         symbolic mode + absolute modified time (aria-describedby; JUS-140). A
         single node — never one per row. -->
    <span :id="ROW_DETAILS_ID" class="fm-sr-only">{{ rovingDetails }}</span>

    <!-- JUS-146: the selection count is announced from an always-mounted polite
         live region (empty until the selection settles), so the first selection
         is announced and a range select does not chatter. -->
    <span
      class="fm-sr-only"
      role="status"
      aria-live="polite"
      aria-atomic="true"
    >{{ selectionAnnouncement }}</span>

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
    <ImageViewerDialog
      v-model:show="viewerOpen"
      :server-id="props.serverId"
      :team-id="teams.activeTeamId"
      :root="files.rootName"
      :directory="files.path"
      :entry="viewerEntry"
    />
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
/* File manager styles; only tokens from styles/tokens.css are used. */
.files-browser {
  position: relative;
  /* JUS-134: the single row-height token every list row reads. JUS-137 can
   * size its virtual rows from it. JUS-206: 42px, the reference row height.
   * JUS-207: the remaining sample-geometry values with no token fit live
   * here too, defined once — every rule below reads these vars: the 34px
   * header height, the 34px checkbox column, the 25px file-type mark, the
   * 6px toolbar-button radius and the 6px row vertical padding. */
  --fm-row-height: 42px;
  --fm-head-h: 34px;
  --fm-check-col: 34px;
  --fm-mark-size: 25px;
  --fm-btn-radius: 6px;
  --fm-row-pad-y: 6px;
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

/* JUS-154: the header slot renders inside Naive's `.n-card-header`, whose
 * `.n-card-header__main` sets the card-title size (18px). The captions had no
 * `.small` rule of their own, so they inherited that 18px; this scoped rule
 * sets the mockup's `--text-xs` (12px) caption directly. */
.small {
  font-size: var(--text-xs);
}

.fm-root-select {
  width: 220px;
}

/* Tab bar (JUS-123/150, JUS-206 round 2-5): the sample's Chrome-style tabs,
   mapped to tokens. Inactive tabs are transparent; hovering paints an inset
   rounded fill behind the content; 1px dividers separate neighbours (hidden
   around the active/hovered tab via :has). The active tab uses the card body
   colour with rounded TOP corners only and concave flares joining it to the
   content below — no ring, no outline, no accent border. The strip scrolls
   horizontally at narrow widths instead of wrapping the layout. */
/* Header band (JUS-206 round 4): the dark strip from the card top to the tab
   row. It bleeds to the card's side edges (the card content has no top
   padding, so no top compensation is needed) and keeps the 16px rhythm below
   the tabs; the top corners follow the card radius. End-aligned like the
   sample so the active tab sits flush on the content. */
.fm-topbar {
  display: flex;
  /* Round 10: centred on the tab row's horizontal axis (the tabs define the
     row height, so the shorter switcher centres on the 34px tab middle). */
  align-items: center;
  gap: var(--space-3);
  flex: 0 0 auto;
  background: var(--surface-warm);
  /* Round 7: zero below the tabs — the band ends exactly at the tab bottom
     so the active tab touches the card body and flows into the content. */
  margin: 0 calc(-1 * var(--space-6)) 0;
  padding: var(--space-2) var(--space-2) 0;
  border-radius: var(--n-border-radius, var(--radius-md)) var(--n-border-radius, var(--radius-md)) 0 0;
}

/* Context cluster (JUS-159/JUS-206 round 4): the agent-node switcher pinned to
   the right end of the tab row. Plain, no border of its own. */
.fm-contextbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  min-width: 0;
  margin-left: auto;
}

.fm-tabs {
  /* Sample geometry, defined once: the 34px tab height, 240px flex basis,
     56px shrink floor and 28px + button have no token fit. Everything else
     below reads space/radius tokens. */
  --fm-tab-h: 34px;
  --fm-tab-w: 240px;
  --fm-tab-min: 56px;
  --fm-add-size: 28px;
  display: flex;
  align-items: flex-end;
  gap: 0;
  flex: 1 1 auto;
  min-width: 0;
  margin-top: 0;
  margin-bottom: 0;
}

.fm-tabstrip {
  display: flex;
  align-items: flex-end;
  gap: 0;
  /* The strip fills the row like the sample's .tabs (flex 1 1 auto): tabs
     keep their 240px basis while there is room and shrink (scrolling the
     strip) when there is not. A hugging strip would collapse to the tabs'
     content minimum instead of their basis (round 6 probe). */
  flex: 1 1 auto;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: thin;
}

/* The tablist owns only the tabs (the + is a strip-level sibling in the tab
   flow): it is display:contents so the tabs lay out directly in the strip —
   the round-6/9 verified behaviour (240px basis kept while there is room,
   shrinking to the icon-only floor, strip scrolling past that). A real flex
   box here would either collapse the basis (hug) or push + to the far right
   (fill). */
.fm-tablist {
  display: contents;
}

.fm-tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 1 var(--fm-tab-w);
  min-width: var(--fm-tab-min);
  height: var(--fm-tab-h);
  border: 0;
  background: none;
  padding: 0 var(--space-1) 0 var(--space-3);
  border-radius: var(--radius-md);
  color: var(--muted);
  font-size: var(--text-sm);
  text-align: left;
  cursor: pointer;
  isolation: isolate;
}

/* Inactive hover (sample ::before): an inset rounded fill behind the content,
   gapped on all four sides. */
.fm-tab:not(.is-active)::before {
  content: "";
  position: absolute;
  inset: var(--space-1);
  border-radius: var(--radius-md);
  background: transparent;
  z-index: -1;
  transition: background var(--motion-fast) var(--ease-standard);
}

.fm-tab:not(.is-active):hover::before {
  background: var(--accent-softer);
}

.fm-tab:hover {
  color: var(--fg-2);
}

/* Divider between neighbours: a 1px x 16px rule centred in the tab height.
   Hidden at the strip end and around the hovered/active tab (sample :has). */
.fm-tab::after {
  content: "";
  position: absolute;
  right: 0;
  top: calc((var(--fm-tab-h) - var(--space-4)) / 2);
  width: 1px;
  height: var(--space-4);
  background: var(--border);
}

.fm-tab:last-child::after,
.fm-tab:hover::after,
.fm-tab:has(+ .fm-tab:hover)::after,
.fm-tab:has(+ .fm-tab.is-active)::after {
  display: none;
}

.fm-tab-icon {
  flex: 0 0 auto;
  color: var(--warn-ink);
}

/* The active tab is the card body colour with rounded TOP corners only, in
   the normal text colour — no ring, no outline, no accent border. Its concave
   bottom flares (the card body colour over the warm band, sample geometry)
   join it to the content below. Like the sample, the left flare is ::before
   and the right flare is a real element (.fm-tab-corner): ::after is the
   divider, which the :has()/:hover rules above hide next to a hovered tab and
   would take the flare with it. */
.fm-tab.is-active {
  background: var(--n-color);
  border-radius: var(--radius-md) var(--radius-md) 0 0;
  color: var(--fg);
  font-weight: 600;
  z-index: 2;
}

.fm-tab.is-active::after {
  display: none;
}

.fm-tab.is-active::before,
.fm-tab-corner {
  content: "";
  position: absolute;
  bottom: 0;
  width: var(--radius-md);
  height: var(--radius-md);
  pointer-events: none;
}

.fm-tab.is-active::before {
  left: calc(-1 * var(--radius-md));
  background: radial-gradient(circle at 0 0, transparent var(--radius-md), var(--n-color) calc(var(--radius-md) + 0.5px));
}

.fm-tab-corner {
  right: calc(-1 * var(--radius-md));
  background: radial-gradient(circle at 100% 0, transparent var(--radius-md), var(--n-color) calc(var(--radius-md) + 0.5px));
}

/* Icon-only narrow tabs (sample .narrow, measured in updateNarrowTabs). The
   title stays in the DOM (clipped and transparent) so the tab keeps its
   accessible name and never reads as an empty icon-only button. */
.fm-tab.is-narrow {
  justify-content: center;
  gap: 0;
  padding: 0 var(--space-2);
}

.fm-tab.is-narrow .fm-tab-name {
  max-width: 0;
  opacity: 0;
}

.fm-tab.is-narrow:not(.is-active) .fm-tab-close {
  display: none;
}

/* The strip scrolls (overflow-x: auto) and clips its descendants at its edge,
   and the shared focus outline would read as a loud box here — keyboard focus
   (only; mouse clicks and fresh loads never move DOM focus, see the watcher)
   draws a thin ring just inside the tab instead (JUS-152). */
.fm-tab:focus-visible {
  outline: 1px solid var(--accent-ink);
  outline-offset: -1px;
}

.fm-tab-name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  /* Sample fade mask instead of an ellipsis. */
  -webkit-mask-image: linear-gradient(90deg, #000 80%, transparent);
  mask-image: linear-gradient(90deg, #000 80%, transparent);
}

.fm-tab-close {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  width: var(--space-5);
  height: var(--space-5);
  margin-right: var(--space-1);
  border-radius: var(--radius-pill);
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
  width: var(--fm-add-size);
  height: var(--fm-add-size);
  /* Round 10: in the tab flow right after the last tab (sample gap 6px —
     no token fits, so the literal stands with this comment), bottom-centred
     in the tab height. Sticky at the strip's visible end so tabs scroll under
     it; the band-coloured fill keeps scrolled tabs from showing through
     (invisible against the band itself). Above an open active tab (z-index 2).
     */
  margin: 0 0 calc((var(--fm-tab-h) - var(--fm-add-size)) / 2) 0;
  margin-left: 6px;
  position: sticky;
  right: 0;
  z-index: 3;
  border: 0;
  background: var(--surface-warm);
  border-radius: var(--radius-pill);
  color: var(--muted);
  cursor: pointer;
}

.fm-tab-add:hover:not(:disabled) {
  background: var(--hover-row);
  color: var(--fg-2);
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

/* Round 10: the header band is flush with the card top — Naive gives the
   first-child content 20px of top padding, which would push the band down. */
.files-browser :deep(.n-card-content:first-child) {
  padding-top: 0;
}

/* The single shared aria-describedby target for the roving row (JUS-140):
   clipped out of view but still in the accessibility tree. */
.fm-sr-only {
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

.fm-bar.fm-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}

/* Left group (Upload, New menu, Root selector, File Operations) wraps
   internally and may shrink, so a long group never overflows/clips at narrow
   widths — every action stays reachable instead of spilling past the panel.
   The right-aligned group never wraps internally (JUS-208); both groups always
   have content, so the toolbar can never wrap onto an empty second row
   (JUS-147/JUS-160). */
.fm-toolbar-group {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  min-width: 0;
}

/* The toolbar's scrolling content row: transparent on desktop (display:
   contents, so the two groups lay out as the toolbar's own flex items exactly
   as before), the horizontal scrollport on the phone (<=480px rules below).
   Splitting the scrollport out of the toolbar box lets the grid bulk overlay
   (JUS-208 round 2) anchor to the visible row instead of scrolling with the
   content. */
.fm-toolbar-scroll {
  display: contents;
}

/* JUS-160: the divider between the New menu and the Root selector in the left
   group. A token-only 1px rule, hidden with the create actions on read-only
   roots so it never leads a row. */
.fm-toolbar-sep {
  width: 1px;
  height: var(--space-5);
  flex: 0 0 auto;
  background: var(--border);
}

/* The toolbar's Root selector keeps its desktop width but never forces the
   toolbar past the panel: it may shrink to its content on narrow rows (the
   phone rule below takes over <=640px). */
.fm-toolbar .fm-root-select {
  max-width: 100%;
}

/* JUS-153: the right-hand cluster now follows the per-root actions directly
   (the filter/refresh that used to push it right moved to the path bar), so the
   group carries its own auto margin to stay pinned to the right edge. */
/* JUS-208: the end group never wraps internally — when the row genuinely
   cannot fit, the whole group moves to the next row and the selection actions
   scroll inside it, instead of the group splitting across two rows and the
   toolbar growing a third one. It ties with .fm-toolbar-group above, so this
   rule must stay after it. */
.fm-toolbar-end {
  margin-left: auto;
  justify-content: flex-end;
  flex-wrap: nowrap;
}

/* JUS-208 round 2: the grid bulk bar overlays the toolbar row while a
   selection exists (mirroring how the list bar replaces the column headers),
   so selecting never grows the toolbar or shifts the tiles. The row keeps its
   height from the in-flow groups; the overlay is absolutely positioned over it
   with the card colour, its action row scrolling inside when it cannot fit.
   Idle it stays mounted but visibility-hidden and inert — out of the layout,
   the a11y tree and the tab order. The overlay itself is transparent to the
   pointer (only its action row takes events), so covered toolbar controls keep
   working exactly as before and the bar never traps input. */
.fm-bar.fm-toolbar.is-grid {
  position: relative;
}

.fm-grid-bulk {
  position: absolute;
  inset: 0;
  /* Above every in-flow toolbar control (naive's select creates its own
     stacking context): the bar's own buttons must win the hit test against the
     covered controls. Teleported menus and dialogs live outside this stacking
     context with far higher z-indexes, and the later-DOM drop overlay wins
     ties, so nothing else is buried. */
  z-index: 5;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  overflow: hidden;
  background: var(--n-color);
  visibility: hidden;
  pointer-events: none;
}

.fm-grid-bulk.is-visible {
  visibility: visible;
}

/* The overlay's own action row stays interactive over the covered controls
   (:deep — the row is rendered by the child FilesSelectionBar, so the parent
   scope alone would never match it). */
.fm-grid-bulk:deep(.fm-selection-actions) {
  pointer-events: auto;
}

/* The clipboard hint (Paste (N) / Cut clipboard) is its own compact chip so
   the words never wrap vertically when the toolbar is narrow (JUS-146): it
   refuses to shrink and never breaks inside a word. */
.clipboard-hint {
  margin-left: var(--space-1);
  flex: 0 0 auto;
  white-space: nowrap;
}

.fm-caret {
  margin-left: 2px;
  color: var(--muted);
}

/* Small screens drop the toolbar labels, leaving icon-only buttons whose
   accessible name comes from each button's aria-label, and its tooltip is
   the shared AppTooltip (JUS-156).
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

/* The path field owns its border; space separates the chrome bands.
   Round 8: the breathing room above the row is n-colored PADDING inside the
   body (margins cannot paint, so a margin gap could read as a dark seam) —
   the tab bottom touches the body top and flows into it, then 16px of the
   same body colour before the row, like the sample's toolbar band. */
.fm-bar.fm-pathbar {
  margin-bottom: var(--space-8);
  padding-top: var(--space-4);
  background: var(--n-color);
}

.fm-toolbar :deep(.n-button) {
  height: var(--space-8);
  padding: 0 var(--space-3);
  border-radius: var(--fm-btn-radius);
}

/* JUS-206: default outlined toolbar buttons — 32px high, 12px labels, the
   reference padding. The 6px radius is the reference value. */
.fm-toolbar :deep(.n-button .n-button__content) {
  font-size: var(--text-xs);
}

.fm-toolbar :deep(.n-button:not(.n-button--icon)) {
  padding: 0 var(--space-3);
}

/* Context bar (JUS-159): the home of the header-tools slot (the agent-node
   selector) after the card header went away. Plain row, no border of its own;
   the Root selector and the view toggle moved into the toolbar (JUS-160). */
/* Merged navigation field (JUS-159/JUS-206): a Naive NInputGroup row, 32px
   high — a back icon button, the breadcrumb strip and an attached refresh
   button joined into one bordered row. The group collapses the buttons'
   adjoining borders; the borderless crumb strip draws only its top/bottom so
   the row reads as one field. Long paths truncate from the left (the crumb
   strip clips at its start edge) while back and refresh stay pinned. */
.fm-navfield {
  /* JUS-206 round 1: naive's .n-input-group is width:100% — reset to auto so
     the row shares the desktop path bar with the search block (~half each);
     the phone rules below still take their own full-width rows. */
  width: auto;
  flex: 1 1 50%;
  min-width: 0;
  height: var(--space-8);
  min-height: var(--space-8);
  padding: 0;
  border: 0;
  background: none;
  gap: 0;
}

.fm-navback,
.fm-navrefresh {
  flex: 0 0 auto;
  width: var(--space-8);
  height: var(--space-8);
}

.fm-navrefresh {
  margin-left: 0;
}

.fm-navcrumbs {
  flex: 1 1 auto;
  align-self: stretch;
  min-width: 0;
  flex-wrap: nowrap;
  /* JUS-206 round 1: crumbs start right after the back button (reference:
     [back] | Root dir > www > ... [refresh]), truncating from the left. */
  justify-content: flex-start;
  overflow: hidden;
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  border-radius: 0;
  /* Vertical rhythm comes from the `.fm-path` base; only widen the sides. The
     strip stretches the row's full 32px (its own borders complete it). */
  padding-block: 0;
  padding-inline: var(--space-2);
}

.fm-navcrumbs .fm-crumb {
  flex: 0 0 auto;
}

.fm-path {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-wrap: wrap;
  /* JUS-150: the breadcrumb rides the card surface — no background or border —
     but keeps its own box metrics so the shared :focus-visible ring has room. */
  padding: var(--space-1);
  min-height: var(--space-8);
}

.fm-crumb {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-2);
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
  color: var(--fg-2);
  font-weight: 600;
}

.fm-path .sep {
  flex: 0 0 auto;
  color: var(--meta);
}

/* The nav-row right slot hosts the search input with its "Include
   subdirectories" switch and submit button (JUS-163). The group takes the
   right edge; the input keeps its own width. */
.fm-search-group {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 1 auto;
  min-width: 0;
  margin-left: auto;
}

.fm-search-group .fm-search {
  margin-left: 0;
}

/* The Include-subdirectories switch shrinks with its label ellipsized, so the
   group never overflows the nav row at a phone width where it shares the row
   with Refresh (JUS-163 fix round 2). */
.fm-search-recursive {
  flex: 0 1 auto;
  min-width: 0;
}

.fm-search-recursive :deep(.n-checkbox__label) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-search-submit {
  flex: 0 0 auto;
}

.fm-search {
  max-width: 260px;
  min-width: 180px;
  margin-left: auto;
}

/* The count cell and the bulk actions live in FilesSelectionBar (scoped there),
   because the same cluster is hosted by the list header and the grid toolbar. */

.fm-state {
  /* A state view owns the whole entry area, which is why it scrolls
     internally instead of growing the panel. */
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-8) var(--space-4);
  color: var(--muted);
  /* JUS-151: the state view fills the panel, so center its content on both
     axes (the mockup's .empty component). `safe` keeps the top reachable if
     the panel is ever shorter than the state. */
  display: grid;
  justify-items: center;
  align-content: safe center;
  gap: var(--space-2);
  text-align: center;
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
  /* The entry list is the panel's single scrolling region (JUS-118). JUS-161:
     the framed card around the table — a thin token border + radius. */
  flex: 1 1 auto;
  min-height: 0;
  overflow-x: auto;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.fm-table {
  min-width: 760px;
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
  /* JUS-161: checkbox, Name, Owner/Mode, Size, Modified, Operation. */
  grid-template-columns: var(--fm-check-col) minmax(240px, 1.6fr) 110px 96px 148px 168px;
  gap: var(--space-3);
  align-items: center;
  /* JUS-206: the reference cell padding. */
  padding: var(--fm-row-pad-y) var(--space-2);
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
   window maths never drifts (the un-windowed list keeps its min-height).
   JUS-145: `overflow: clip`, not `hidden` — `hidden` would make the row (not
   `.table-wrap`) the scroll container, so the pinned actions cell could not
   stay pinned inside a windowed row. `clip` still clips to the exact height. */
.fm-table.is-virtual .fm-row:not(.fm-head) {
  height: var(--fm-row-height);
  overflow: clip;
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
     header band matches the reference (a tone below the card) so rows scroll
     under it cleanly. */
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--surface-warm);
  color: var(--muted);
  border-bottom: 1px solid var(--border-soft);
  /* JUS-146: the row height is max(column headers, bulk bar). Both variants
     are always mounted and overlap the same cells, so the height never depends
     on the selection — no reserved band, no row shift (JUS-130/134). */
  /* JUS-206: the reference header is 34px (--fm-head-h). */
  min-height: var(--fm-head-h);
  padding-block: 0;
  align-items: center;
}

/* JUS-146: the column-header variant occupies columns 2-7 (the leading
   select-all checkbox cell, column 1, stays visible in both states). The bulk
   bar below reuses exactly the same cells, so the two overlap and the row
   keeps one constant height. */
.fm-head-cols {
  grid-column: 2 / -1;
  grid-row: 1;
  display: grid;
  grid-template-columns: minmax(240px, 1.6fr) 110px 96px 148px 168px;
  gap: var(--space-3);
  align-items: center;
  /* JUS-206: the reference headers are normal-case 12px/500 body text with
     sort arrows — not tiny uppercase mono. */
  font-family: var(--font-body);
  font-size: var(--text-xs);
  font-weight: 500;
  letter-spacing: normal;
  text-transform: none;
}

.fm-head-cols.is-hidden {
  visibility: hidden;
}

/* JUS-145/146: mirror the compact row grid inside the column-header variant so
   the header columns line up with the rows at ≤860px (Modified/Owner-Mode are
   hidden there; the name/size/actions tracks match .fm-row). */
@media (max-width: 860px) {
  .fm-head-cols {
    grid-template-columns: minmax(0, 1fr) 64px 64px;
    gap: var(--space-2);
  }
}

/* JUS-146: the bulk bar (count + bulk actions) replaces the column headers in
   the same sticky row while entries are selected. Its controls self-hide with
   visibility, so the row keeps the same height and the hidden controls are
   neither focusable nor announced. */
.fm-head-bulk {
  grid-column: 2 / -1;
  grid-row: 1;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  /* JUS-146: the bar overlaps the column headers (same cells), so while nothing
     is selected it is hidden with `visibility` — not just transparent — so it is
     neither clickable nor an extra empty columnheader in the accessibility tree.
     `visibility` keeps the layout, so the row height never changes. */
  visibility: hidden;
}

.fm-head-bulk.is-visible {
  visibility: visible;
  background: var(--surface-warm);
}

/* JUS-147: the bulk bar's sort control keeps the desktop list sortable while the
   bar replaces the column headers. It never shrinks — the action row scrolls. */
.fm-bulk-sort {
  flex: 0 0 auto;
}

/* JUS-146: above the compact breakpoint the desktop table can be wider than the
   scroller, so the bar is pinned to the scroller's left edge and constrained to
   the VISIBLE scroller width (--fm-scroller-w) — its actions then scroll in
   their OWN row instead of hiding off-screen behind the table scroll. Below 861px
   the table fits (min-width: 0) so the bar just fills its cell. 58px = the
   header inline padding + the select-all cell + the grid gap. */
@media (min-width: 861px) {
  .fm-head-bulk {
    position: sticky;
    left: calc(var(--space-3) + var(--fm-check-col) + var(--space-3));
    justify-self: start;
    width: calc(var(--fm-scroller-w, 100%) - var(--space-3) - var(--fm-check-col) - var(--space-3));
  }
}

.fm-head .right {
  text-align: right;
}

/* Hovering the pinned header must not reveal the rows scrolling under it:
   keep the solid header band instead of the row hover tint. */
.fm-head:hover {
  background: var(--surface-warm);
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

/* JUS-134/148: on the desktop row the name and its (optional) symlink target
 * share one line so every row keeps the single tokenised height and the type is
 * not repeated (the Type column carries it). `.fm-name-sub` dissolves into the
 * name's flex row on the desktop (`display: contents`), so the target sits right
 * after the name; on the compact row it becomes the secondary line.
 * The name↔target offset keeps its original --space-2 (the flex gap that used to
 * live inside `.fm-label`): the row gap is --space-3 (icon↔name), so trim the
 * label's trailing gap back by 4px. */
.fm-name .fm-label {
  display: block;
  flex: 0 1 auto;
  min-width: 0;
  margin-right: calc(var(--space-2) - var(--space-3));
}

.fm-name-sub {
  display: contents;
}

.fm-label b {
  display: block;
  min-width: 0;
  /* JUS-206: the reference rows are 12px/400 body text. */
  font-weight: 400;
  color: var(--fg);
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-target {
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

/* JUS-145/148: the compact row's secondary modified line. Hidden on the desktop
   layout, where the Modified column shows it. */
.fm-row-sub {
  display: none;
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
  /* JUS-206: the reference cells are normal 12px body text, not mono. */
  font-family: var(--font-body);
  font-size: var(--text-xs);
  color: var(--muted);
}

.fm-cell--size {
  text-align: right;
}

/* JUS-134: the mode and date cells are one line; the rwx form is the mode
   cell's tooltip. */
.fm-mode,
.fm-date {
  white-space: nowrap;
}

/* JUS-214: the Owner/Mode track is a fixed 110px, so a long owner
   ("systemd-network/755" or a 10-digit uid) must clip instead of spilling into
   the right-aligned Size cell. min-width: 0 lets the grid item shrink below its
   min-content width; the full owner/mode text stays in the DOM for screen
   readers (no title is added — JUS-210 removes file-manager tooltips). */
.fm-mode {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.fm-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  justify-content: flex-end;
}

/* JUS-145: the action cell (the ⋮ and its inline buttons) is pinned to the
   right edge of the list's scrollport, so the row menu is reachable at EVERY
   width where the desktop table overflows horizontally — the row's track
   min-content is ~984px, far above the compact breakpoint the columns collapse
   at. The background is OPAQUE: the card surface plus the row-state tint
   composited on top (the tokens are translucent), so the columns scrolling
   under the pinned cell never show through; the hairline marks the pinned
   edge. */
.fm-row > .fm-actions {
  position: sticky;
  right: 0;
  z-index: 1;
  background-color: var(--surface);
  box-shadow: -1px 0 0 var(--border);
}

.fm-head .fm-actions-cell {
  position: sticky;
  right: 0;
  z-index: 1;
  background-color: var(--surface-warm);
  box-shadow: -1px 0 0 var(--border);
}

.fm-row:hover > .fm-actions {
  background-image: linear-gradient(var(--hover-row), var(--hover-row));
}

.fm-row.is-selected > .fm-actions {
  background-image: linear-gradient(var(--accent-soft), var(--accent-soft));
}

.fm-row.is-selected:hover > .fm-actions {
  background-image: linear-gradient(var(--accent-softer), var(--accent-softer));
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

/* A blocked action (a file's Download without the content role) keeps naive's
   dimmed opacity when revealed: these selectors out-specify both the reveal
   rule above and naive's own .n-button--disabled rule. The action stays
   focusable ([aria-disabled], not [disabled]) so its tooltip can explain why. */
.fm-row:hover .fm-actions .fm-action.n-button--disabled,
.fm-row:hover .fm-actions .fm-action[aria-disabled="true"],
.fm-row:focus-within .fm-actions .fm-action.n-button--disabled,
.fm-row:focus-within .fm-actions .fm-action[aria-disabled="true"],
.fm-row.is-selected .fm-actions .fm-action.n-button--disabled,
.fm-row.is-selected .fm-actions .fm-action[aria-disabled="true"] {
  opacity: var(--n-opacity-disabled, 0.5);
}

.fm-actions .fm-action[aria-disabled="true"] {
  cursor: not-allowed;
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

/* JUS-145: the tile's ⋮ row menu sits in the top-right corner as a sibling of
   the tile button (never nested inside it). The selection checkbox shifts left
   of it so both stay tappable; on touch both grow to a 44px target (see the
   compact media block below). */
.fm-tile-actions {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 3;
}

.fm-tile-actions :deep(.n-button) {
  min-width: 30px;
  height: 30px;
}

.fm-tile-check {
  position: absolute;
  top: 6px;
  right: 42px;
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

/* ── JUS-206 round 3: desktop one-row path bar ───────────────────────────
   At >=1024px the path group (~half the row) and the search block share ONE
   row: the bar never wraps, the breadcrumb clips from the left and the filter
   input shrinks instead (its 120px floor plus the compact search controls fit
   every desktop width). Below 1024px the bar may wrap; the phone rules below
   take over at <=640px. */
@media (min-width: 1024px) {
  .fm-bar.fm-pathbar {
    flex-wrap: nowrap;
  }

  .fm-search {
    min-width: 120px;
  }
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

  /* Keep the roomier band gaps while reducing the path-to-toolbar gap on phones. */
  .fm-toolbar,
  .fm-bar.fm-pathbar {
    row-gap: var(--space-2);
    margin-bottom: var(--space-4);
  }

  /* Path bar row 1: the breadcrumb takes the whole row (scrolls, never wraps). */
  .fm-path {
    flex: 1 1 100%;
    min-width: 0;
    flex-wrap: nowrap;
    overflow-x: auto;
    scrollbar-width: thin;
  }

  /* Path bar: at phone the navigation field gives the search group room so the
     three controls share ONE row with Refresh (JUS-135 ≥60% list). The
     breadcrumb scrolls, so it can shrink; the search group keeps the input and
     the switch readable instead of overflowing (JUS-163 fix round 2). */
  .fm-navfield {
    flex: 1 1 110px;
  }

  .fm-search-group {
    flex: 1 1 190px;
    min-width: 0;
    margin-left: 0;
  }

  /* The switch keeps only its box on a phone: a full label would crowd the
     input to nothing. The label text still names the control for assistive tech
     (aria-labelledby) and the tooltip shows it for a pointer (JUS-163 round 2). */
  .fm-search-recursive :deep(.n-checkbox__label) {
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

  .fm-search {
    min-width: 0;
    max-width: none;
    flex: 1 1 120px;
    margin-left: 0;
  }

  /* Toolbar: the clipboard (Paste + its nowrap hint chip) takes its own
     full-width row. The bulk actions moved into the header bar (JUS-146), so
     nothing competes for this row. */
  .fm-toolbar-end {
    flex: 1 1 100%;
    justify-content: flex-start;
    flex-wrap: nowrap;
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

@media (max-width: 480px) {
  /* JUS-206 round 7: single band row on the phone too — the compact
     switcher (icon hidden, truncated select) shares the row while the strip
     keeps its own horizontal scroll, so nothing overflows and no second band
     row appears between the tabs and the body. */
  .fm-topbar {
    flex-wrap: nowrap;
    margin: 0 calc(-1 * var(--space-3)) 0;
    padding: var(--space-2) var(--space-3) 0;
  }

  .fm-topbar .fm-node__icon {
    display: none;
  }

  /* JUS-206 round 9: the horizontal strip must never reserve scrollbar layout
     space on the phone (a classic scrollbar gutter sits the tabs above the
     band bottom and clips the feet). The strip stays scrollable by touch,
     wheel and keyboard; the + button lives outside it. Desktop keeps its
     thin scrollbar and is untouched. */
  .fm-tabstrip {
    scrollbar-width: none;
  }

  .fm-tabstrip::-webkit-scrollbar {
    display: none;
  }

  .fm-navfield,
  .fm-search-group {
    flex-basis: 100%;
  }

  /* JUS-135 phone toolbar: the groups ride one scrolling row (the outer
     toolbar keeps the row box, so the grid bulk overlay anchors to the visible
     area instead of the scrolled content). */
  .fm-toolbar-scroll {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1 1 auto;
    min-width: 0;
    flex-wrap: nowrap;
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
  }

  .fm-toolbar-group,
  .fm-toolbar-end {
    flex: 0 0 auto;
    flex-wrap: nowrap;
  }

  .fm-toolbar .fm-root-select {
    width: calc(var(--space-12) * 3);
    flex: 0 0 auto;
  }

  /* Keep keyboard focus visible inside the horizontal scrollport. */
  .fm-toolbar :deep(button:focus-visible) {
    outline-offset: calc(-1 * var(--space-1));
  }
}

/* ── JUS-145: compact list (a density optimisation, not the reachability fix) ─
   The Actions cell is pinned sticky at every width the table overflows (see
   .fm-row > .fm-actions), so the ⋮ is always on screen. Below 860px the
   low-value columns (Modified, Owner/Mode) additionally collapse so Name, Size
   and the ⋮ fit a phone scrollport without a sideways scroll and the rows stay
   dense. Modified is not lost — it rides under the name as a muted line — and
   the toolbar sort control keeps Modified / Owner-Mode sortable because their
   headers are hidden here. */
@media (max-width: 860px) {
  .fm-table {
    min-width: 0;
  }

  .files-browser {
    --fm-row-height: 52px;
  }

  .fm-row {
    grid-template-columns: 30px minmax(0, 1fr) 64px 64px;
    gap: var(--space-2);
    /* 3px block padding so the 44px ⋮ fits inside a 52px windowed row (the
       4px base would leave 43px of content and clip it by 1px). */
    padding-block: 3px;
  }

  .fm-cell--modified,
  .fm-cell--mode {
    display: none;
  }

  /* The name cell becomes a two-row grid: the type icon spans both rows, the
     name takes the first line and the secondary line carries the symlink target
     next to the relative modified time (JUS-145/148). The name no longer shares
     its line with the target, so it is never squeezed to "br…". */
  .fm-name {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    grid-template-areas:
      "icon label"
      "icon sub";
    align-items: center;
    column-gap: var(--space-3);
    row-gap: 0;
  }

  .fm-name > .fm-mark {
    grid-area: icon;
  }

  .fm-name .fm-label {
    grid-area: label;
    /* The desktop name↔target trim does not apply to the grid cell. */
    margin-right: 0;
  }

  /* The secondary line: the target (when any) then the relative time. */
  .fm-name-sub {
    grid-area: sub;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .fm-row-sub {
    display: block;
    flex: 0 1 auto;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The inline buttons fold into the ⋮ menu (same actions) and the ⋮ is a 44px
     target in every compact layout. */
  .fm-actions .fm-action {
    display: none;
  }

  .fm-actions :deep(.n-button) {
    min-width: 44px;
    height: 44px;
  }
}

/* JUS-145: above the compact range a coarse pointer still needs a 44px ⋮ and a
   row tall enough for it; the inline actions keep their density, because a set
   of 44px inline buttons would overflow the fixed actions track. */
@media (hover: none) and (min-width: 861px) {
  .files-browser {
    --fm-row-height: 52px;
  }

  .fm-row:not(.fm-head) {
    padding-block: 3px;
  }

  .fm-actions :deep(.n-button:not(.fm-action)) {
    min-width: 44px;
    height: 44px;
  }
}

@media (hover: none) {
  .fm-tile-actions :deep(.n-button) {
    min-width: 44px;
    height: 44px;
  }

  /* The tile checkbox clears the wider 44px ⋮. */
  .fm-tile-check {
    right: 56px;
  }
}

@media (hover: none) {
  .fm-actions .fm-action {
    opacity: 1;
  }

  .fm-actions .fm-action.n-button--disabled,
  .fm-actions .fm-action[aria-disabled="true"] {
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
