<script setup lang="ts">
import {
  Archive,
  ArchiveRestore,
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  ClipboardPaste,
  Copy,
  Download,
  FilePlus,
  Folder,
  FolderPlus,
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
  Terminal,
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

import { useAuthStore } from "@/features/auth";
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
import { useFilesTabsPersist } from "@/features/files/composables/useFilesTabsPersist";
import { resetFolderSizes } from "@/features/files/composables/useFolderSize";
import type { FilesEntry } from "@/features/files/schemas/files";
import type { FilesTab } from "@/features/files/stores/files";
import { provideFilesPane } from "@/features/files/stores/filesPane";
import { useFilesPaneSession } from "@/features/files/stores/paneSession";
import type { FilesPaneId } from "@/features/files/stores/paneSession";
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
  symlinkTargetsDirectory,
  terminalDirectoryFor,
  typeaheadIndex,
  widestBytesLabel,
} from "@/features/files/utils/files";
import {
  folderMenuOptions,
  menuIcon,
  rowMenuOptions,
} from "@/features/files/utils/filesMenu";
import type { FilesMenuItem } from "@/features/files/utils/filesMenu";
import { isRowControlEvent } from "@/features/files/utils/rowControls";
import {
  clampScrollTop,
  gridColumnCount,
  gridWindow,
  renderedKey,
  windowRange,
} from "@/features/files/utils/virtual";
import { useServersStore } from "@/features/servers";
import { useTerminalStore } from "@/features/terminal";
import { formatBytes, relativeTime } from "@/shared/utils/format";

interface Props {
  serverId: string;
  pane?: FilesPaneId;
}

const props = withDefaults(defineProps<Props>(), { pane: "left" });

const { t, locale } = useI18n();
const message = useMessage();
const paneSession = useFilesPaneSession();
const pane = provideFilesPane(props.pane);
const files = pane.files;
const trash = pane.trash;
const editor = pane.editor;

function activatePane(): void {
  paneSession.activate(props.pane);
}
const isActivePane = computed<boolean>(
  () => paneSession.splitEnabled && paneSession.activePane === props.pane,
);
const auth = useAuthStore();
const serversStore = useServersStore();
const terminal = useTerminalStore();
const { download } = useFileDownload();
const { initBrowser } = useFilesTabsPersist(() => props.serverId, props.pane);

watch(
  () => props.serverId,
  (next) => {
    trash.reset();
    resetFolderSizes(props.pane);
    void initBrowser(next);
  },
);
const coarseNow = useCoarseNow();

watch(
  () => files.rootName,
  () => {
    trash.reset();
    resetFolderSizes(props.pane);
  },
);
onMounted(() => void initBrowser(props.serverId));

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
const recycleOpen = ref(false);
const actionTargets = ref<FilesEntry[]>([]);

const filterModel = computed<string>({
  get: () => files.query,
  set: (value) => files.setQuery(value ?? ""),
});

const recursiveModel = computed<boolean>({
  get: () => files.recursive,
  set: (value) => files.setRecursive(value === true),
});

function submitSearch(): void {
  void files.runSearch();
}

function onSearchKeydown(event: KeyboardEvent): void {
  if (event.key !== "Enter" || event.isComposing) {
    return;
  }
  event.preventDefault();
  submitSearch();
}

const rootOptions = computed(() =>
  files.roots.map((root) => ({
    value: root.name,
    label: `${root.name} · ${t(root.writable ? "files.browser.writable" : "files.browser.readOnly")}`,
  })),
);

const selectedRoot = computed<string>({
  get: () => files.rootName,
  set: (value) => void files.setRoot(value ?? ""),
});

const hasSelection = computed<boolean>(() => files.selectedNames.length > 0);

const gridBulkVisible = computed<boolean>(
  () => files.viewMode === "grid" && hasSelection.value,
);

const countReserve = computed<string>(
  () =>
    `${t("files.browser.selectionCount", { count: files.entries.length })} · ${widestBytesLabel(
      files.entries.reduce((sum, entry) => sum + entry.size, 0),
    )}`,
);

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
  resetFolderSizes(props.pane);
});

const footSummary = computed<string>(() => {
  const filtering = files.query.trim() !== "";
  const parts = [
    filtering
      ? t("files.browser.entryCountFiltered", {
          shown: files.visibleEntries.length,
          total: files.entries.length,
          count: files.entries.length,
        })
      : t("files.browser.entryCount", { count: files.entries.length }),
  ];
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

const currentPath = computed<string>(() =>
  files.path === "" ? files.rootName : `${files.rootName}/${files.path}`,
);

function pasteProblemMessage(
  problem: "cross-node" | "cross-root" | TransferProblem | null,
): string {
  switch (problem) {
    case "cross-node":
      return String(t("files.dialogs.pasteCrossNode"));
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

const pasteProblemText = computed<string>(() =>
  pasteProblemMessage(files.pasteProblem),
);

const pasteLabel = computed<string>(() => {
  const count = files.clipboard?.names.length ?? 0;
  const base = `${String(t("files.actions.paste"))} (${count})`;
  return pasteProblemText.value ? `${base} — ${pasteProblemText.value}` : base;
});

function typeLabel(entry: FilesEntry): string {
  return entryTypeLabel(entry, locale.value);
}

function iconKind(entry: FilesEntry): ReturnType<typeof entryIconKind> {
  return entryIconKind(entry);
}

function entrySubtitle(entry: FilesEntry): string {
  if (entry.kind === "symlink") {
    return entry.symlink_target
      ? `${typeLabel(entry)} → ${entry.symlink_target}`
      : typeLabel(entry);
  }
  return typeLabel(entry);
}

function entryTarget(entry: FilesEntry): string {
  return entry.kind === "symlink" && entry.symlink_target
    ? `→ ${entry.symlink_target}`
    : "";
}

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

function ariaSort(key: string): "ascending" | "descending" | "none" {
  if (files.sortKey !== key) {
    return "none";
  }
  return files.sortDir === "asc" ? "ascending" : "descending";
}

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

function isRowControl(event: Event): boolean {
  return isRowControlEvent(event);
}

function extendRange(event: MouseEvent, entry: FilesEntry): void {
  if (!isSelectable(entry)) {
    return;
  }
  files.selectRange(entry.name, event.ctrlKey || event.metaKey);
}

function onRowClick(event: MouseEvent, entry: FilesEntry): void {
  if (isRowControl(event) || !isSelectable(entry)) {
    return;
  }
  if (event.shiftKey) {
    event.preventDefault();
    extendRange(event, entry);
    focusRow(entry.name);
    return;
  }
  if (event.ctrlKey || event.metaKey) {
    files.toggleSelection(entry.name);
    return;
  }
  files.selectOnly(entry.name);
  rovingName.value = entry.name;
}

function onCheckboxCapture(event: MouseEvent, entry: FilesEntry): void {
  if (!event.shiftKey) {
    return;
  }
  event.preventDefault();
  event.stopPropagation();
  extendRange(event, entry);
  focusRow(entry.name);
}

function onListMouseDown(event: MouseEvent): void {
  const pressOutsideHeader =
    (event.target as Element | null)?.closest(".fm-head") === null;
  if (
    event.shiftKey ||
    (pressOutsideHeader && document.activeElement?.closest(".fm-head") !== null)
  ) {
    event.preventDefault();
  }
}

function onRowDblclick(event: MouseEvent, entry: FilesEntry): void {
  if (event.shiftKey || isRowControl(event)) {
    return;
  }
  activate(entry);
}

function openInEditor(entry: FilesEntry): void {
  if (entry.kind !== "file" || !files.canReadContent) {
    return;
  }
  activatePane();
  void editor.openFile(
    props.serverId,
    files.rootName,
    joinPath(files.path, entry.name),
  );
}

const tabListEl = ref<HTMLElement | null>(null);
const scrollEl = ref<HTMLElement | null>(null);

const VIRTUAL_THRESHOLD = 100;
const rowHeight = ref(36);
const tileMetrics = ref({ min: 176, gap: 12, height: 132 });
const localScrollTop = ref(0);
const headEl = ref<HTMLElement | null>(null);
const scrollSize = useElementSize(scrollEl);
const headSize = useElementSize(headEl, undefined, { box: "border-box" });
const scrollerWidthPx = computed<string>(() => `${scrollSize.width.value}px`);

const focusedName = ref<string | null>(null);
const menuRowName = ref<string | null>(null);
const pinnedName = computed<string | null>(
  () => menuRowName.value ?? focusedName.value,
);
let restorePending = false;
let lastFocusWasPointer = false;

const pinnedIndex = computed<number>(() => {
  const name = pinnedName.value;
  if (name === null) {
    return -1;
  }
  return files.visibleEntries.findIndex((entry) => entry.name === name);
});

function parsePx(value: string, fallback: number): number {
  const text = value.trim();
  if (!text.endsWith("px")) {
    return fallback;
  }
  const parsed = Number.parseFloat(text);
  return Number.isFinite(parsed) ? parsed : fallback;
}

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
    localScrollTop.value = el?.scrollTop ?? 0;
  },
  { flush: "post" },
);
watch(
  [() => scrollSize.width.value, () => scrollSize.height.value],
  () => readVirtualMetrics(),
);

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

interface RenderedRow {
  entry: FilesEntry;
  index: number;
  pinned: boolean;
}

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

const listRows = computed<RenderedRow[]>(() => {
  const entries = files.visibleEntries;
  if (!isListVirtual.value) {
    return entries.map((entry, index) => ({ entry, index, pinned: false }));
  }
  const win = listWindow.value;
  return windowRows(entries, win.start, win.end);
});

function pinnedListStyle(index: number): Record<string, string> {
  return { top: `${headSize.height.value + index * rowHeight.value}px` };
}

const gridColumns = computed<number>(() => {
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
const gridPitch = computed<number>(
  () => tileMetrics.value.height + tileMetrics.value.gap,
);
function gridContentHeight(count: number): number {
  const rows = Math.ceil(count / gridColumns.value);
  return rows > 0 ? rows * gridPitch.value - tileMetrics.value.gap : 0;
}
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
const gridRows = computed<RenderedRow[]>(() => {
  const entries = files.visibleEntries;
  if (!isGridVirtual.value) {
    return entries.map((entry, index) => ({ entry, index, pinned: false }));
  }
  const win = gridWindowRange.value;
  return windowRows(entries, win.start, win.end);
});

const renderedKeys = computed<string>(() =>
  renderedKey(
    (files.viewMode === "grid" ? gridRows.value : listRows.value).map(
      (row) => row.entry.name,
    ),
  ),
);

const gridTileWidth = computed<number>(() => {
  const cols = Math.max(1, gridColumns.value);
  return (
    (scrollSize.width.value - (cols - 1) * tileMetrics.value.gap) / cols
  );
});

function pinnedTileStyle(index: number): Record<string, string> {
  const cols = Math.max(1, gridColumns.value);
  const gap = tileMetrics.value.gap;
  return {
    top: `${Math.floor(index / cols) * gridPitch.value}px`,
    left: `${(index % cols) * (gridTileWidth.value + gap)}px`,
    width: `${gridTileWidth.value}px`,
  };
}

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

const tabLabels = computed<Map<string, string>>(() =>
  disambiguatedTabLabels(files.tabs),
);

function tabLabel(tab: FilesTab): string {
  return tabLabels.value.get(tab.id) ?? tabBasename(tab);
}

function tabBasename(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : (tab.path.split("/").pop() ?? tab.rootName);
}

function modifiedLabel(entry: FilesEntry): string {
  return relativeTime(
    entry.modified_at,
    locale.value === "vi" ? "vi" : "en",
    { long: true, now: coarseNow.value },
  );
}

function rowDetails(entry: FilesEntry): string {
  return String(
    t("files.browser.rowDetails", {
      mode: formatMode(entry.mode),
      modified: formatTimestamp(entry.modified_at),
    }),
  );
}

const ROW_DETAILS_ID = computed<string>(() =>
  props.pane === "left" ? "fm-row-details" : `fm-row-details-${props.pane}`,
);
const PANEL_ID = computed<string>(() =>
  props.pane === "left" ? "fm-tabpanel" : `fm-tabpanel-${props.pane}`,
);

const rovingEntry = computed<FilesEntry | null>(() => {
  const name = rovingTabName.value;
  if (name === null) {
    return null;
  }
  const rendered = files.viewMode === "grid" ? gridRows.value : listRows.value;
  return rendered.find((row) => row.entry.name === name)?.entry ?? null;
});

const rovingDetails = computed<string>(() => {
  const entry = rovingEntry.value;
  return entry === null ? "" : rowDetails(entry);
});

function rowDescribedBy(entry: FilesEntry): string | undefined {
  return entry.name === rovingTabName.value ? ROW_DETAILS_ID.value : undefined;
}

const NARROW_TAB_PX = 90;

const narrowTabs = ref<ReadonlySet<string>>(new Set());

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
  const card = cardEl.value?.$el as HTMLElement | undefined;
  if (card && typeof ResizeObserver !== "undefined") {
    browserWidth.value = card.getBoundingClientRect().width;
    compactObserver = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) {
        browserWidth.value = entry.contentRect.width;
      }
    });
    compactObserver.observe(card);
  }
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
  compactObserver?.disconnect();
  compactObserver = null;
});
watch(
  () => files.tabs.map((tab) => `${tab.id}:${tabLabel(tab)}`),
  () => void nextTick(() => updateNarrowTabs()),
  { flush: "post" },
);

function tabTitle(tab: FilesTab): string {
  return tab.path === "" ? tab.rootName : `${tab.rootName}/${tab.path}`;
}

function openNewTab(): void {
  void files.openTab();
}

async function focusActiveTab(): Promise<void> {
  await nextTick();
  const active = tabListEl.value?.querySelector<HTMLElement>(
    '[role="tab"][aria-selected="true"]',
  );
  active?.focus();
  active?.scrollIntoView({ block: "nearest", inline: "nearest" });
}

function neighbourTabId(id: string, delta: number): string {
  const count = files.tabs.length;
  const index = files.tabs.findIndex((tab) => tab.id === id);
  return files.tabs[(index + delta + count) % count]?.id ?? id;
}

let closeFocusArmed = false;

function closeTabFromButton(id: string): void {
  closeFocusArmed = id === files.activeTabId;
  void files.closeTab(id);
}

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
      if (files.tabs.length > 1) {
        event.preventDefault();
        closeTabFromButton(tab.id);
      }
      break;
  }
}

function onTabAuxClick(event: MouseEvent, id: string): void {
  if (event.button !== 1) {
    return;
  }
  event.preventDefault();
  closeTabFromButton(id);
}

function onListScroll(event: Event): void {
  const el = event.currentTarget as HTMLElement;
  localScrollTop.value = el.scrollTop;
  files.setScrollTop(el.scrollTop);
}

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
  const lead = grid ? 0 : headSize.height.value;
  const top = lead + row * itemSize;
  const bottom = top + itemSize;
  if (top < el.scrollTop + lead) {
    el.scrollTop = Math.max(0, top - lead);
  } else if (bottom > el.scrollTop + el.clientHeight) {
    el.scrollTop = bottom - el.clientHeight;
  }
  localScrollTop.value = el.scrollTop;
  files.setScrollTop(el.scrollTop);
}

const rovingName = ref<string | null>(null);
const TYPEAHEAD_RESET_MS = 1000;
let typeBuffer = "";
let typeTimer: ReturnType<typeof setTimeout> | null = null;

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

function rowTabindex(name: string): 0 | -1 {
  return name === rovingTabName.value ? 0 : -1;
}

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

function focusedListIndex(): number {
  const active = document.activeElement as HTMLElement | null;
  const row = active?.closest<HTMLElement>("[data-entry]");
  const name = row?.dataset.entry ?? rovingTabName.value;
  return name === null
    ? -1
    : files.visibleEntries.findIndex((entry) => entry.name === name);
}

function pageStep(itemSize: number, columns: number): number {
  const rows = Math.max(
    1,
    Math.floor(scrollSize.height.value / Math.max(1, itemSize)),
  );
  return rows * columns;
}

function entryAt(index: number): FilesEntry | null {
  const entries = files.visibleEntries;
  if (entries.length === 0) {
    return null;
  }
  return entries[Math.min(Math.max(index, 0), entries.length - 1)] ?? null;
}

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

function focusRow(name: string): void {
  rovingName.value = name;
  scrollEntryIntoView(name);
  void nextTick(() => focusEntry(name));
}

function clearTypeTimer(): void {
  if (typeTimer !== null) {
    clearTimeout(typeTimer);
    typeTimer = null;
  }
}

function resetTypeahead(): void {
  clearTypeTimer();
  typeBuffer = "";
}

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
  if (!(target instanceof Element) || target.closest("[data-entry]") === null) {
    return;
  }
  const meta = event.ctrlKey || event.metaKey;
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
      if (isRowControl(event)) {
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
      if (isRowControl(event)) {
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

let restoringFocus = false;
let focusedEl: HTMLElement | null = null;

function focusEntry(name: string | null): void {
  const el = scrollEl.value;
  if (name === null || el === null) {
    return;
  }
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
      restoringFocus = true;
      target.focus({ preventScroll: true });
      restoringFocus = false;
    }
    return;
  }
}

function onListFocusIn(event: FocusEvent): void {
  const target = event.target as HTMLElement | null;
  const row = target?.closest<HTMLElement>("[data-entry]");
  const name = row?.dataset.entry ?? null;
  const pointer = lastFocusWasPointer;
  lastFocusWasPointer = false;
  if (name === null) {
    releaseFocus();
    return;
  }
  focusedName.value = name;
  focusedEl = target;
  if (!restoringFocus && !pointer) {
    scrollEntryIntoView(name);
  }
}

function releaseFocus(): void {
  focusedName.value = null;
  focusedEl = null;
}

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

function closeRowMenu(): void {
  menuRowName.value = null;
}

function onContextShow(show: boolean): void {
  contextOpen.value = show;
  if (!show) {
    closeRowMenu();
  }
}

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

watch(pinnedIndex, (index) => {
  if (index === -1) {
    releaseFocus();
    closeRowContext();
  }
});

let swapHadFocus = false;
watch(
  hasSelection,
  (active, previous) => {
    if (previous === active) {
      return;
    }
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

watch(() => files.activeTabId, async () => {
  await nextTick();
  const active = tabListEl.value?.querySelector<HTMLElement>(
    '[role="tab"][aria-selected="true"]',
  );
  if (!active || document.activeElement === active) {
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

async function startDownload(entry: FilesEntry): Promise<void> {
  if (!files.canReadContent) {
    return;
  }
  await download(
    props.serverId,
    files.rootName,
    files.path,
    entry,
  );
}

async function downloadSelected(): Promise<void> {
  for (const entry of files.selectedEntries) {
    if (entry.kind !== "file") {
      continue;
    }
    await download(
      props.serverId,
      files.rootName,
      files.path,
      entry,
    );
  }
}

function openCreate(kind: NameKind): void {
  createKind.value = kind;
  createOpen.value = true;
}

const newMenuOpen = ref(false);

const newMenuOptions = computed(() =>
  [
    {
      label: String(t("files.actions.newFile")),
      key: "file",
      icon: menuIcon(FilePlus),
    },
    {
      label: String(t("files.actions.newFolder")),
      key: "directory",
      icon: menuIcon(FolderPlus),
    },
  ].map((option) => ({ ...option, props: { role: "menuitem" } })),
);

function onNewMenu(key: string): void {
  openCreate(key === "directory" ? "directory" : "file");
}

const fileOpsOpen = ref(false);

const fileOpsSelection = computed<FilesEntry[]>(() =>
  files.selectedEntries.filter(isSelectable),
);

const fileOpsExtractTarget = computed<FilesEntry | null>(
  () =>
    fileOpsSelection.value.find((entry) => isExtractableArchive(entry)) ??
    null,
);

const fileOpsPermissionsTarget = computed<FilesEntry | null>(
  () =>
    fileOpsSelection.value.find(
      (entry) => entry.kind === "file" || entry.kind === "directory",
    ) ?? null,
);

const fileOpsOptions = computed(() => {
  const names = fileOpsSelection.value.map((entry) => entry.name);
  const canAct = files.canWrite && !files.acting && names.length > 0;
  const item = (label: string, key: string, disabled: boolean) => ({
    label,
    key,
    disabled,
    props: { role: "menuitem" },
  });
  const iconFor = (key: string) => {
    switch (key) {
      case "cut":
        return menuIcon(Scissors);
      case "copy":
        return menuIcon(Copy);
      case "paste":
        return menuIcon(ClipboardPaste);
      case "compress":
        return menuIcon(Archive);
      case "extract":
        return menuIcon(ArchiveRestore);
      case "permissions":
        return menuIcon(Lock);
      default:
        return undefined;
    }
  };
  return [
    item(String(t("files.actions.cut")), "cut", !canAct),
    item(String(t("files.actions.copy")), "copy", !canAct),
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
  ].map((option) => ({ ...option, icon: iconFor(option.key) }));
});

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

const sortMenuOpen = ref(false);
const bulkSortMenuOpen = ref(false);
const cardEl = ref<InstanceType<typeof NCard> | null>(null);
const browserWidth = ref(0);
let compactObserver: ResizeObserver | null = null;
const isCompactList = computed<boolean>(
  () => browserWidth.value > 0 && browserWidth.value < 860,
);
const showSortControl = computed<boolean>(
  () => isCompactList.value || files.viewMode === "grid",
);

const columnCount = computed<number>(() => {
  if (isCompactList.value) {
    return 4;
  }
  return 6;
});

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

const sortDirection = computed<string>(() =>
  String(
    t(
      files.sortDir === "asc"
        ? "files.browser.sortAscending"
        : "files.browser.sortDescending",
    ),
  ),
);

const sortLabel = computed<string>(
  () =>
    `${t("files.browser.sortBy")}: ${sortColumnLabel(files.sortKey)} — ${sortDirection.value}`,
);

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

function openCompress(entries: FilesEntry[]): void {
  if (entries.length === 0) {
    return;
  }
  compressTargets.value = entries;
  compressOpen.value = true;
}

function openExtract(entry: FilesEntry): void {
  if (!isExtractableArchive(entry)) {
    return;
  }
  extractTarget.value = entry;
  extractOpen.value = true;
}

const viewerOpen = ref(false);
const viewerEntry = ref<FilesEntry | null>(null);

function openViewer(entry: FilesEntry): void {
  if (entry.kind !== "file" || !files.canReadContent) {
    return;
  }
  viewerEntry.value = entry;
  viewerOpen.value = true;
}

function onDragOver(event: DragEvent): void {
  if (!isFileDrag(event.dataTransfer?.types)) {
    return;
  }
  event.preventDefault();
  if (files.canWrite) {
    dragActive.value = true;
  }
}

function onDragLeave(): void {
  dragActive.value = false;
}

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

const serverName = computed<string>(
  () =>
    serversStore.servers.find((server) => server.id === props.serverId)?.name ??
    props.serverId,
);

const terminalRequesting = ref(false);

const terminalTarget = computed<string | null>(() => {
  if (
    !auth.isOwner ||
    props.serverId === "" ||
    files.currentRoot === null
  ) {
    return null;
  }
  const tab = files.activeTab;
  if (!tab.loaded || tab.lastLoadedPath !== tab.path) {
    return null;
  }
  return tab.lastLoadedPath;
});

const canOpenTerminal = computed<boolean>(() => terminalTarget.value !== null);

const terminalOpening = computed<boolean>(() => {
  if (terminalTarget.value === null || files.rootName === "") {
    return false;
  }
  return terminal.isWindowOpening({
    serverId: props.serverId,
    target: "host",
    root: files.rootName,
    cwd: terminalTarget.value,
  });
});

function symlinkDirectory(entry: FilesEntry): boolean {
  return symlinkTargetsDirectory(
    entry,
    files.activeTab.lastLoadedPath,
    files.entries,
    files.currentRoot?.path ?? "",
  );
}

async function openTerminalHere(entry: FilesEntry | null = null): Promise<void> {
  if (
    !canOpenTerminal.value ||
    terminalOpening.value ||
    terminalRequesting.value
  ) {
    return;
  }
  const directory = terminalDirectoryFor(entry, files.activeTab.lastLoadedPath, {
    entries: files.entries,
    rootPath: files.currentRoot?.path ?? "",
  });
  const root = files.rootName;
  if (directory === null || root === "") {
    return;
  }
  terminalRequesting.value = true;
  try {
    await terminal.openWindowSession({
      serverId: props.serverId,
      serverName: serverName.value,
      target: "host",
      root,
      cwd: directory,
    });
  } finally {
    terminalRequesting.value = false;
  }
}

const menuContext = computed(() => ({
  canWrite: files.canWrite,
  canReadContent: files.canReadContent,
  canPaste: files.hasClipboard && files.pasteProblem === null,
  canOpenTerminal: canOpenTerminal.value,
  t: (key: string, named?: Record<string, unknown>) =>
    String(t(key, named ?? {})),
}));

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
    symlinkTargetDirectory: symlinkDirectory(entry),
  });
}

function onRowMenu(key: string, entry: FilesEntry): void {
  switch (key) {
    case "open":
      void files.openDirectory(entry.name);
      break;
    case "open-tab":
      void files.openTab({ root: files.rootName, path: joinPath(files.path, entry.name) });
      break;
    case "open-terminal":
      void openTerminalHere(entry);
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

const contextOpen = ref(false);
const contextX = ref(0);
const contextY = ref(0);
const contextEntry = ref<FilesEntry | null>(null);

const contextOptions = computed<FilesMenuItem[]>(() =>
  contextEntry.value
    ? rowMenu(contextEntry.value)
    : folderMenuOptions(menuContext.value),
);

const contextAria = computed<string>(() =>
  contextEntry.value
    ? String(t("files.browser.moreActions", { name: contextEntry.value.name }))
    : String(t("files.browser.folderActions")),
);

const rowMenuOpenName = ref<string | null>(null);

function closeRowContext(): void {
  contextOpen.value = false;
  contextEntry.value = null;
  closeRowMenu();
}

function resetRowMenu(): void {
  rowMenuOpenName.value = null;
  closeRowContext();
}

const layoutKey = computed<string>(
  () =>
    `${files.viewMode}:${files.activeTabId}:${files.rootName}:${files.path}`,
);
watch(layoutKey, resetRowMenu);

function resetSortMenu(): void {
  sortMenuOpen.value = false;
  bulkSortMenuOpen.value = false;
}
watch([layoutKey, showSortControl, hasSelection], resetSortMenu);

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

function isRowMenuOpen(entry: FilesEntry): boolean {
  return (
    rowMenuOpenName.value === entry.name ||
    (contextOpen.value && contextEntry.value?.name === entry.name)
  );
}

function onRowMenuShow(name: string, show: boolean): void {
  rowMenuOpenName.value = show
    ? name
    : rowMenuOpenName.value === name
      ? null
      : rowMenuOpenName.value;
}

function openRowContext(event: MouseEvent, entry: FilesEntry): void {
  if (rowMenu(entry).length === 0) {
    return;
  }
  files.selectForRowContext(entry);
  contextEntry.value = entry;
  menuRowName.value = entry.name;
  contextX.value = event.clientX;
  contextY.value = event.clientY;
  contextOpen.value = true;
}

function openFolderContext(event: MouseEvent): void {
  contextEntry.value = null;
  closeRowMenu();
  contextX.value = event.clientX;
  contextY.value = event.clientY;
  contextOpen.value = true;
}

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
    case "open-terminal":
      void openTerminalHere();
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
    ref="cardEl"
    class="files-browser"
    role="region"
    :data-pane="props.pane"
    :aria-current="isActivePane ? 'true' : undefined"
    :aria-label="
      $t('files.browser.heading') +
      ' — ' +
      $t(props.pane === 'left' ? 'files.split.leftPane' : 'files.split.rightPane')
    "
    :class="{
      'is-dragging': dragActive,
      'is-compact': isCompactList,
      'is-active-pane': isActivePane,
    }"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
    @pointerdown.capture="activatePane"
    @focusin="activatePane"
  >
    <div class="fm-topbar">
    <div class="fm-tabs">
      <div ref="tabListEl" class="fm-tabstrip">
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
          :aria-controls="PANEL_ID"
          aria-keyshortcuts="Delete"
          :aria-selected="tab.id === files.activeTabId"
          :tabindex="tab.id === files.activeTabId ? 0 : -1"
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
        <button
          type="button"
          class="fm-tab-add"
          :disabled="files.rootName === ''"
          :aria-label="$t('files.tabs.new')"
          @click="openNewTab()"
        >
          <Plus :size="14" :stroke-width="1.8" aria-hidden="true" />
        </button>
      </div>
    </div>
      <div class="fm-contextbar">
        <span v-if="isActivePane" class="fm-active-tag" aria-hidden="true">
          {{ $t("files.split.active") }}
        </span>
        <slot name="header-tools" />
      </div>
    </div>

    <div class="fm-bar fm-pathbar">
      <NInputGroup class="fm-navfield">
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
        <NCheckbox v-model:checked="recursiveModel" size="small" class="fm-search-recursive">
          {{ $t("files.search.includeSubdirs") }}
        </NCheckbox>
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
      </div>
    </div>

    <div class="fm-bar fm-toolbar" :class="{ 'is-grid': files.viewMode === 'grid' }">
      <div class="fm-toolbar-scroll" :inert="gridBulkVisible || undefined">
        <div class="fm-toolbar-group">
          <NButton
            v-if="files.canWrite"
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
          </NDropdown>
          <span v-if="canOpenTerminal" class="fm-terminal-action-wrap">
            <NButton
              size="small"
              class="fm-terminal-action"
              :loading="terminalOpening || terminalRequesting"
              :aria-busy="terminalOpening || terminalRequesting"
              :aria-label="$t('files.terminal.openHere')"
              @click="openTerminalHere()"
            >
              <template #icon>
                <Terminal :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              <span class="fm-btn-label">{{ $t("files.terminal.label") }}</span>
            </NButton>
          </span>
          <span v-if="files.hasClipboard" class="small muted clipboard-hint">
            {{ files.clipboardMode === "cut" ? $t("files.clipboard.cut") : $t("files.clipboard.copy") }}
          </span>
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
          </NDropdown>
        </div>

        <div class="fm-toolbar-group fm-toolbar-end">
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
          <span class="fm-toolbar-sep" aria-hidden="true" />
          <span class="segmented" role="group">
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
          </span>
        </div>
      </div>

      <div
        v-if="files.viewMode === 'grid'"
        class="fm-grid-bulk"
        :class="{ 'is-visible': gridBulkVisible }"
        :inert="!gridBulkVisible || undefined"
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

    <div
      :id="PANEL_ID"
      class="fm-panel"
      role="tabpanel"
      :aria-labelledby="'fm-tab-' + files.activeTabId"
    >
    <div v-if="files.disabled" class="empty fm-state" @contextmenu.prevent="openFolderContext">
      <Lock :size="26" :stroke-width="1.6" aria-hidden="true" />
      <h4>{{ $t("files.browser.disabledTitle") }}</h4>
      <p class="small">
        {{ $t(files.disabledOnNode ? "files.browser.nodeDisabledHint" : "files.browser.disabledHint") }}
      </p>
      <NButton size="small" secondary class="fm-state-action" @click="files.retry()">
        {{ $t("common.actions.retry") }}
      </NButton>
    </div>

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

            <div
              class="fm-head-bulk"
              :class="{ 'is-visible': hasSelection }"
              role="columnheader"
              :aria-colspan="columnCount - 1"
            >
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
              <NButton
                v-if="entry.kind === 'directory'"
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
              <NButton
                v-else-if="entry.kind === 'file'"
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
              <NButton
                v-if="files.canWrite && isExtractableArchive(entry)"
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
              <NButton
                v-if="files.canWrite && isSelectable(entry)"
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
              <NButton
                v-if="files.canWrite && isSelectable(entry)"
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
              <span class="fm-label fm-tile-name">
                <b>{{ entry.name }}</b>
              </span>
            </span>
            <span class="fm-tile-bottom">
              <span class="fm-tile-lines">
                <span class="fm-tile-type">{{ entrySubtitle(entry) }}</span>
                <span class="fm-tile-ownermode">{{ ownerModeLabel(entry) }}</span>
                <span v-if="entry.kind !== 'directory'" class="fm-tile-size">{{ formatBytes(entry.size) }}</span>
              </span>
            </span>
          </button>
          <span class="fm-tile-foot">
            <span class="fm-tile-ago">{{ modifiedLabel(entry) }}</span>
          </span>
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

    <span :id="ROW_DETAILS_ID" class="fm-sr-only">{{ rovingDetails }}</span>

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
        :root="files.rootName"
      :directory="files.path"
      :entry="viewerEntry"
    />
    <ConflictDialog />

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
.files-browser {
  position: relative;
  --fm-row-height: 42px;
  --fm-head-h: 34px;
  --fm-check-col: 34px;
  --fm-mark-size: 25px;
  --fm-btn-radius: 6px;
  --fm-row-pad-y: 6px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  max-height: none;
}

.files-browser.is-active-pane {
  outline: 1px solid var(--accent);
  outline-offset: -1px;
}

.files-browser :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
}

.small {
  font-size: var(--text-xs);
}

.fm-root-select {
  width: 220px;
}

.fm-topbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 0 0 auto;
  background: var(--surface-warm);
  margin: 0 calc(-1 * var(--space-6)) 0;
  padding: var(--space-2) var(--space-2) 0;
  border-radius: var(--n-border-radius, var(--radius-md)) var(--n-border-radius, var(--radius-md)) 0 0;
}

.fm-contextbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
  min-width: 0;
  margin-left: auto;
}

.fm-active-tag {
  flex: 0 0 auto;
  padding: 1px var(--space-2);
  border-radius: var(--radius-sm);
  background: color-mix(in oklab, var(--accent) 16%, transparent);
  color: var(--accent-ink);
  font-size: var(--text-xs);
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: uppercase;
}

.fm-tabs {
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
  flex: 1 1 auto;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: thin;
}

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

.fm-tab:focus-visible {
  outline: 1px solid var(--accent-ink);
  outline-offset: -1px;
}

.fm-tab-name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
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

.fm-panel {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
}

.files-browser :deep(.n-card-content:first-child) {
  padding-top: 0;
}

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

.fm-toolbar-group {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  min-width: 0;
}

.fm-toolbar-scroll {
  display: contents;
}

.fm-toolbar-sep {
  width: 1px;
  height: var(--space-5);
  flex: 0 0 auto;
  background: var(--border);
}

.fm-toolbar .fm-root-select {
  max-width: 100%;
}

@media (max-width: 1400px) {
  .fm-toolbar .fm-root-select {
    width: calc(var(--space-12) * 4 - var(--space-2));
  }
}

.fm-terminal-action {
  flex: 0 0 auto;
}

.fm-terminal-action-wrap {
  display: inline-flex;
  align-items: center;
}

.fm-toolbar-end {
  margin-left: auto;
  justify-content: flex-end;
  flex-wrap: nowrap;
}

.fm-bar.fm-toolbar.is-grid {
  position: relative;
}

.fm-grid-bulk {
  position: absolute;
  inset: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  overflow: hidden;
  background: var(--n-color);
  visibility: hidden;
}

.fm-grid-bulk.is-visible {
  visibility: visible;
}

.clipboard-hint {
  margin-left: var(--space-1);
  flex: 0 0 auto;
  white-space: nowrap;
}

.fm-caret {
  margin-left: 2px;
  color: var(--muted);
}

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

.fm-toolbar :deep(.n-button .n-button__content) {
  font-size: var(--text-xs);
}

.fm-toolbar :deep(.n-button:not(.n-button--icon)) {
  padding: 0 var(--space-3);
}

.fm-navfield {
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
  justify-content: flex-start;
  overflow: hidden;
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  border-radius: 0;
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

.fm-state {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-8) var(--space-4);
  color: var(--muted);
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

.fm-table.is-virtual {
  position: relative;
}

.fm-pad {
  display: block;
  width: 100%;
}

.fm-grid > .fm-pad {
  grid-column: 1 / -1;
}

.fm-row {
  display: grid;
  grid-template-columns: var(--fm-check-col) minmax(240px, 1.6fr) 110px 96px 148px 168px;
  gap: var(--space-3);
  align-items: center;
  padding: var(--fm-row-pad-y) var(--space-2);
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

.fm-table.is-virtual .fm-row:not(.fm-head) {
  height: var(--fm-row-height);
  overflow: clip;
}

.fm-table.is-virtual .fm-row.is-pinned {
  position: absolute;
  left: 0;
  right: 0;
  z-index: 1;
}

.fm-head {
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--surface-warm);
  color: var(--muted);
  border-bottom: 1px solid var(--border-soft);
  min-height: var(--fm-head-h);
  padding-block: 0;
  align-items: center;
}

.fm-head-cols {
  grid-column: 2 / -1;
  grid-row: 1;
  display: grid;
  grid-template-columns: minmax(240px, 1.6fr) 110px 96px 148px 168px;
  gap: var(--space-3);
  align-items: center;
  font-family: var(--font-body);
  font-size: var(--text-xs);
  font-weight: 500;
  letter-spacing: normal;
  text-transform: none;
}

.fm-head-cols.is-hidden {
  visibility: hidden;
}

.files-browser.is-compact .fm-head-cols {
  grid-template-columns: minmax(0, 1fr) 64px 64px;
  gap: var(--space-2);
}

.fm-head-bulk {
  grid-column: 2 / -1;
  grid-row: 1;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  visibility: hidden;
}

.fm-head-bulk.is-visible {
  visibility: visible;
  background: var(--surface-warm);
}

.fm-bulk-sort {
  flex: 0 0 auto;
}

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
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-row-sub {
  display: none;
}

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
  font-family: var(--font-body);
  font-size: var(--text-xs);
  color: var(--muted);
}

.fm-cell--size {
  text-align: right;
}

.fm-mode,
.fm-date {
  white-space: nowrap;
}

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

.fm-actions .fm-action {
  opacity: 0;
  transition: opacity var(--motion-base) var(--ease-standard);
}

.fm-row:hover .fm-actions .fm-action,
.fm-row:focus-within .fm-actions .fm-action,
.fm-row.is-selected .fm-actions .fm-action {
  opacity: 1;
}

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
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  align-content: start;
}

.fm-grid.is-virtual {
  position: relative;
}

.fm-grid.is-virtual .fm-tile {
  height: var(--fm-tile-height);
}

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
  min-width: 0;
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  padding-bottom: 28px;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
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
  gap: var(--space-2);
  min-width: 0;
  padding-right: 26px;
}

.fm-tile-name {
  flex: 1 1 auto;
  min-width: 0;
}

.fm-tile .fm-tile-name b {
  display: block;
  font-weight: 500;
  color: var(--fg);
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fm-tile-ago {
  flex: 0 0 auto;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  white-space: nowrap;
}

.fm-tile-bottom {
  display: flex;
  align-items: flex-end;
  gap: var(--space-2);
  min-width: 0;
  flex: 1 1 auto;
}

.fm-tile-lines {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1 1 auto;
  min-width: 0;
}

.fm-tile-type,
.fm-tile-ownermode,
.fm-tile-size {
  display: block;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

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

.fm-tile-foot {
  position: absolute;
  left: 6px;
  right: 6px;
  bottom: 6px;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  pointer-events: none;
}

.fm-tile-check {
  position: absolute;
  left: 6px;
  bottom: 6px;
  z-index: 2;
  display: grid;
  place-items: center;
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

@media (min-width: 1024px) {
  .fm-bar.fm-pathbar {
    flex-wrap: nowrap;
  }

  .fm-search {
    min-width: 120px;
  }
}

@media (max-width: 640px) {
  .files-browser :deep(.n-card-header) {
    padding: var(--space-2) var(--space-3);
  }

  .files-browser :deep(.n-card-content) {
    padding: 0 var(--space-3) var(--space-2);
  }

  .fm-field-label {
    display: none;
  }

  .fm-root-select {
    width: auto;
    min-width: 0;
    flex: 1 1 90px;
  }

  .fm-toolbar,
  .fm-bar.fm-pathbar {
    row-gap: var(--space-2);
    margin-bottom: var(--space-4);
  }

  .fm-path {
    flex: 1 1 100%;
    min-width: 0;
    flex-wrap: nowrap;
    overflow-x: auto;
    scrollbar-width: thin;
  }

  .fm-navfield {
    flex: 1 1 110px;
  }

  .fm-search-group {
    flex: 1 1 190px;
    min-width: 0;
    margin-left: 0;
  }

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

  .fm-toolbar-end {
    flex: 1 1 100%;
    justify-content: flex-start;
    flex-wrap: nowrap;
  }

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
  .fm-topbar {
    flex-wrap: nowrap;
    margin: 0 calc(-1 * var(--space-3)) 0;
    padding: var(--space-2) var(--space-3) 0;
  }

  .fm-topbar .fm-node__icon {
    display: none;
  }

  .fm-tabstrip {
    scrollbar-width: none;
  }

  .fm-tabstrip::-webkit-scrollbar {
    display: none;
  }

  .fm-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .fm-navfield,
  .fm-search-group {
    flex-basis: 100%;
  }

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

  .fm-toolbar :deep(button:focus-visible) {
    outline-offset: calc(-1 * var(--space-1));
  }
}

.files-browser.is-compact {
  --fm-row-height: 52px;
}

.files-browser.is-compact .fm-table {
  min-width: 0;
}

.files-browser.is-compact .fm-row {
  grid-template-columns: 30px minmax(0, 1fr) 64px 64px;
  gap: var(--space-2);
  padding-block: 3px;
}

.files-browser.is-compact .fm-cell--modified,
.files-browser.is-compact .fm-cell--mode {
  display: none;
}

.files-browser.is-compact .fm-name {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  grid-template-areas:
    "icon label"
    "icon sub";
  align-items: center;
  column-gap: var(--space-3);
  row-gap: 0;
}

.files-browser.is-compact .fm-name > .fm-mark {
  grid-area: icon;
}

.files-browser.is-compact .fm-name .fm-label {
  grid-area: label;
  margin-right: 0;
}

.files-browser.is-compact .fm-name-sub {
  grid-area: sub;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.files-browser.is-compact .fm-row-sub {
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

.files-browser.is-compact .fm-actions .fm-action {
  display: none;
}

.files-browser.is-compact .fm-actions :deep(.n-button) {
  min-width: 44px;
  height: 44px;
}

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
