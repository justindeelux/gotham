import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  chmodEntry,
  compressEntries,
  copyEntry,
  createFile,
  createFolder,
  deleteEntry,
  describeFilesError,
  extractEntry,
  filesErrorKey,
  isCanceledError,
  isConflictError,
  isFilesDisabled,
  listEntries,
  listRoots,
  moveEntry,
  renameEntry,
  searchEntries,
} from "@/features/files/api/files";
import type { ArchiveFormat } from "@/features/files/api/files";
import type {
  FilesEntry,
  FilesRoot,
  FilesSearchHit,
  FileKind,
} from "@/features/files/schemas/files";
import { FILES_TABS_MAX_TABS } from "@/features/files/schemas/tabsPersist";
import type { FilesTabsSnapshot } from "@/features/files/schemas/tabsPersist";
import type { NameKind } from "@/features/files/utils/files";
import {
  breadcrumbSegments,
  countByKind,
  destinationProblem,
  entryMatchesQuery,
  isSelectable,
  joinPath,
  normalizeEntryName,
  parentPath,
  rangeNames,
  sortEntries,
} from "@/features/files/utils/files";
import type {
  BreadcrumbSegment,
  FilesSortDirection,
  FilesSortKey,
  FilesViewMode,
  TransferProblem,
} from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";
import { resetFolderSizes } from "@/features/files/composables/useFolderSize";

export interface FilesClipboard {
  mode: "copy" | "cut";
  root: string;
  path: string;
  names: string[];
  directories: string[];
}

export interface ConflictPrompt {
  op: "copy" | "move";
  root: string;
  tabId: string;
  source: string;
  destination: string;
  names: string[];
  cutClipboard: boolean;
}

export interface TransferSummary {
  done: string[];
  conflicts: string[];
  failed: Array<{ name: string; error: string }>;
}

export type SymlinkTargetResult = "opened" | "not-folder" | "error";

export interface FilesTab {
  id: string;
  rootName: string;
  path: string;
  entries: FilesEntry[];
  cursor: string;
  truncated: boolean;
  loading: boolean;
  loadingMore: boolean;
  loaded: boolean;
  disabled: boolean;
  permissionDenied: boolean;
  errorRaw: unknown;
  viewMode: FilesViewMode;
  sortKey: FilesSortKey;
  sortDir: FilesSortDirection;
  query: string;
  recursive: boolean;
  searchActive: boolean;
  searchQuery: string;
  searchResults: FilesSearchHit[];
  searchTruncated: boolean;
  searchLoading: boolean;
  searchGeneration: number;
  selected: Set<string>;
  anchor: string | null;
  scrollTop: number;
  lastLoadedPath: string;
  generation: number;
}

interface EditorSync {
  applyRename: (_root: string, _dir: string, _oldName: string, _newName: string) => void;
  applyMove: (_root: string, _fromDir: string, _toDir: string, _names: string[]) => void;
  applyDelete: (_root: string, _dir: string, _names: string[]) => void;
}

async function withEditor(sync: (_editor: EditorSync) => void): Promise<void> {
  const { useEditorStore } = await import("@/features/files/stores/editor");
  sync(useEditorStore());
}

let tabSeq = 0;

function nextTabId(): string {
  tabSeq += 1;
  return `ft-${Date.now().toString(36)}-${tabSeq}`;
}

export const useFilesStore = defineStore("files", () => {
  const serverId = ref("");
  const roots = ref<FilesRoot[]>([]);
  const rootsServerId = ref("");

  function createTab(rootName = "", path = "", id = nextTabId()): FilesTab {
    return {
      id,
      rootName,
      path,
      entries: [],
      cursor: "",
      truncated: false,
      loading: false,
      loadingMore: false,
      loaded: false,
      disabled: false,
      permissionDenied: false,
      errorRaw: null,
      viewMode: "list",
      sortKey: "name",
      sortDir: "asc",
      query: "",
      recursive: false,
      searchActive: false,
      searchQuery: "",
      searchResults: [],
      searchTruncated: false,
      searchLoading: false,
      searchGeneration: 0,
      selected: new Set(),
      anchor: null,
      scrollTop: 0,
      lastLoadedPath: "",
      generation: 0,
    };
  }

  const initialTab = createTab();
  const tabs = ref<FilesTab[]>([initialTab]);
  const activeTabId = ref(initialTab.id);

  function findTab(id: string): FilesTab | null {
    return tabs.value.find((tab) => tab.id === id) ?? null;
  }

  const activeTab = computed<FilesTab>(
    () => findTab(activeTabId.value) ?? tabs.value[0],
  );

  const rootName = computed<string>({
    get: () => activeTab.value.rootName,
    set: (value) => {
      activeTab.value.rootName = value;
    },
  });
  const path = computed<string>({
    get: () => activeTab.value.path,
    set: (value) => {
      activeTab.value.path = value;
    },
  });
  const entries = computed<FilesEntry[]>({
    get: () => activeTab.value.entries,
    set: (value) => {
      activeTab.value.entries = value;
    },
  });
  const cursor = computed<string>({
    get: () => activeTab.value.cursor,
    set: (value) => {
      activeTab.value.cursor = value;
    },
  });
  const truncated = computed<boolean>({
    get: () => activeTab.value.truncated,
    set: (value) => {
      activeTab.value.truncated = value;
    },
  });
  const loading = computed<boolean>({
    get: () => activeTab.value.loading,
    set: (value) => {
      activeTab.value.loading = value;
    },
  });
  const loadingMore = computed<boolean>({
    get: () => activeTab.value.loadingMore,
    set: (value) => {
      activeTab.value.loadingMore = value;
    },
  });
  const loaded = computed<boolean>({
    get: () => activeTab.value.loaded,
    set: (value) => {
      activeTab.value.loaded = value;
    },
  });
  const disabled = computed<boolean>({
    get: () => activeTab.value.disabled,
    set: (value) => {
      activeTab.value.disabled = value;
    },
  });
  const permissionDenied = computed<boolean>({
    get: () => activeTab.value.permissionDenied,
    set: (value) => {
      activeTab.value.permissionDenied = value;
    },
  });
  const errorRaw = computed<unknown>({
    get: () => activeTab.value.errorRaw,
    set: (value) => {
      activeTab.value.errorRaw = value;
    },
  });
  const viewMode = computed<FilesViewMode>({
    get: () => activeTab.value.viewMode,
    set: (value) => {
      activeTab.value.viewMode = value;
    },
  });
  const sortKey = computed<FilesSortKey>({
    get: () => activeTab.value.sortKey,
    set: (value) => {
      activeTab.value.sortKey = value;
    },
  });
  const sortDir = computed<FilesSortDirection>({
    get: () => activeTab.value.sortDir,
    set: (value) => {
      activeTab.value.sortDir = value;
    },
  });
  const query = computed<string>({
    get: () => activeTab.value.query,
    set: (value) => {
      activeTab.value.query = value;
    },
  });
  const recursive = computed<boolean>({
    get: () => activeTab.value.recursive,
    set: (value) => {
      activeTab.value.recursive = value;
    },
  });
  const searchActive = computed<boolean>(() => activeTab.value.searchActive);
  const searchQuery = computed<string>(() => activeTab.value.searchQuery);
  const searchResults = computed<FilesSearchHit[]>(
    () => activeTab.value.searchResults,
  );
  const searchTruncated = computed<boolean>(
    () => activeTab.value.searchTruncated,
  );
  const searchLoading = computed<boolean>(() => activeTab.value.searchLoading);
  const selected = computed<Set<string>>({
    get: () => activeTab.value.selected,
    set: (value) => {
      activeTab.value.selected = value;
    },
  });
  const scrollTop = computed<number>({
    get: () => activeTab.value.scrollTop,
    set: (value) => {
      activeTab.value.scrollTop = value;
    },
  });

  const acting = ref(false);
  const clipboard = ref<FilesClipboard | null>(null);
  const conflictPrompt = ref<ConflictPrompt | null>(null);
  const searchAborts = new Map<string, AbortController>();
  const pendingRestore = new Set<string>();

  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeFilesError(errorRaw.value),
  );

  const currentRoot = computed<FilesRoot | null>(
    () => roots.value.find((root) => root.name === rootName.value) ?? null,
  );

  const writable = computed<boolean>(() => currentRoot.value?.writable ?? false);

  const freeBytes = computed<number>(() => currentRoot.value?.free_bytes ?? 0);

  const breadcrumb = computed<BreadcrumbSegment[]>(() =>
    breadcrumbSegments(rootName.value, path.value),
  );

  const hasMore = computed<boolean>(() => cursor.value !== "");

  const counts = computed(() => countByKind(entries.value));

  const visibleEntries = computed<FilesEntry[]>(() => {
    const filtering = !recursive.value && query.value.trim() !== "";
    const filtered = filtering
      ? entries.value.filter((entry) => entryMatchesQuery(entry, query.value))
      : entries.value;
    return sortEntries(filtered, sortKey.value, sortDir.value);
  });

  const selectedNames = computed<string[]>(() => [...selected.value]);

  const visibleCounts = computed<Record<FileKind, number>>(() =>
    countByKind(visibleEntries.value),
  );

  const selectedEntries = computed<FilesEntry[]>(() =>
    entries.value.filter((entry) => selected.value.has(entry.name)),
  );

  const selectedBytes = computed<number>(() =>
    selectedEntries.value.reduce((sum, entry) => sum + entry.size, 0),
  );

  const selectableVisible = computed<FilesEntry[]>(() =>
    visibleEntries.value.filter(isSelectable),
  );

  const allVisibleSelected = computed<boolean>(
    () =>
      selectableVisible.value.length > 0 &&
      selectableVisible.value.every((entry) => selected.value.has(entry.name)),
  );

  const someVisibleSelected = computed<boolean>(
    () =>
      !allVisibleSelected.value &&
      selectableVisible.value.some((entry) => selected.value.has(entry.name)),
  );

  const canReadContent = computed<boolean>(() => {
    const role = useTeamsStore().activeTeam?.role;
    return role === "owner" || role === "admin";
  });

  const canWrite = computed<boolean>(() => canReadContent.value && writable.value);

  const hasClipboard = computed<boolean>(
    () => (clipboard.value?.names.length ?? 0) > 0,
  );

  const clipboardMode = computed<"copy" | "cut" | null>(
    () => clipboard.value?.mode ?? null,
  );

  function pasteProblemAt(
    destination: string,
  ): "cross-root" | TransferProblem | null {
    const clip = clipboard.value;
    if (!clip || clip.names.length === 0) {
      return null;
    }
    if (clip.root !== rootName.value) {
      return "cross-root";
    }
    return destinationProblem(
      clip.path,
      destination,
      clip.directories.map((name) => ({ name, kind: "directory" as FileKind })),
    );
  }

  const pasteProblem = computed<"cross-root" | TransferProblem | null>(() =>
    pasteProblemAt(path.value),
  );

  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  function applyErrorTo(tab: FilesTab, err: unknown): void {
    if (isFilesDisabled(err)) {
      tab.disabled = true;
      tab.errorRaw = null;
      tab.permissionDenied = false;
      return;
    }
    if (filesErrorKey(err) === "files.errors.forbidden") {
      tab.permissionDenied = true;
      tab.errorRaw = null;
      return;
    }
    tab.errorRaw = err;
  }

  function applyListingTo(
    tab: FilesTab,
    entriesPage: FilesEntry[],
    nextCursor: string,
    isTruncated: boolean,
  ): void {
    tab.entries = entriesPage;
    tab.cursor = nextCursor;
    tab.truncated = isTruncated;
    tab.loaded = true;
    tab.disabled = false;
    tab.permissionDenied = false;
    tab.lastLoadedPath = tab.path;
    pruneSelectionToEntries(tab);
  }

  function pruneSelectionToEntries(tab: FilesTab): void {
    if (tab.selected.size === 0 && tab.anchor === null) {
      return;
    }
    const present = new Set(tab.entries.map((entry) => entry.name));
    if (tab.anchor !== null && !present.has(tab.anchor)) {
      tab.anchor = null;
    }
    if (tab.selected.size === 0) {
      return;
    }
    const next = new Set<string>();
    for (const name of tab.selected) {
      if (present.has(name)) {
        next.add(name);
      }
    }
    if (next.size !== tab.selected.size) {
      tab.selected = next;
    }
  }

  function renameSelection(tab: FilesTab, oldName: string, nextName: string): void {
    if (tab.anchor === oldName) {
      tab.anchor = nextName;
    }
    if (!tab.selected.has(oldName)) {
      return;
    }
    const next = new Set(tab.selected);
    next.delete(oldName);
    next.add(nextName);
    tab.selected = next;
  }

  function pruneClipboard(names: string[], root: string, dir: string): void {
    const clip = clipboard.value;
    if (!clip || names.length === 0) {
      return;
    }
    if (clip.root !== root || clip.path !== dir) {
      return;
    }
    const gone = new Set(names);
    const kept = clip.names.filter((name) => !gone.has(name));
    if (kept.length === clip.names.length) {
      return;
    }
    clipboard.value =
      kept.length === 0
        ? null
        : {
            ...clip,
            names: kept,
            directories: clip.directories.filter((name) => !gone.has(name)),
          };
  }

  function revalidateRoots(list: FilesRoot[]): void {
    for (const item of tabs.value) {
      if (!list.some((root) => root.name === item.rootName)) {
        item.rootName = list[0]?.name ?? "";
        item.path = "";
        item.query = "";
        item.anchor = null;
        item.loaded = false;
        resetSearch(item);
      }
    }
  }

  async function loadAll(): Promise<void> {
    const tab = activeTab.value;
    const server = serverId.value;
    if (!server) {
      return;
    }
    const gen = ++tab.generation;
    tab.loading = true;
    tab.errorRaw = null;
    tab.disabled = false;
    resetFolderSizes();
    try {
      const list = await listRoots(server, teamId());
      if (gen !== tab.generation || server !== serverId.value) {
        return;
      }
      roots.value = list;
      rootsServerId.value = server;
      revalidateRoots(list);
      if (tab.rootName === "") {
        applyListingTo(tab, [], "", false);
        return;
      }
      const listing = await listEntries(server, teamId(), {
        root: tab.rootName,
        path: tab.path,
      });
      if (gen !== tab.generation || server !== serverId.value) {
        return;
      }
      applyListingTo(tab, listing.entries, listing.cursor ?? "", listing.truncated);
    } catch (err) {
      if (gen !== tab.generation || server !== serverId.value) {
        return;
      }
      applyErrorTo(tab, err);
    } finally {
      if (gen === tab.generation) {
        tab.loading = false;
      }
    }
  }

  async function loadEntriesFor(tab: FilesTab): Promise<void> {
    if (!serverId.value || tab.rootName === "") {
      tab.entries = [];
      tab.cursor = "";
      tab.truncated = false;
      return;
    }
    const gen = ++tab.generation;
    tab.loading = true;
    tab.errorRaw = null;
    resetFolderSizes();
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: tab.rootName,
        path: tab.path,
      });
      if (gen !== tab.generation) {
        return;
      }
      applyListingTo(tab, listing.entries, listing.cursor ?? "", listing.truncated);
    } catch (err) {
      if (gen !== tab.generation) {
        return;
      }
      applyErrorTo(tab, err);
      if (tab.path !== tab.lastLoadedPath) {
        tab.entries = [];
        tab.cursor = "";
        tab.truncated = false;
      }
    } finally {
      if (gen === tab.generation) {
        tab.loading = false;
      }
    }
  }

  async function loadEntries(): Promise<void> {
    await loadEntriesFor(activeTab.value);
  }

  async function refreshTabsShowing(
    root: string,
    dirs: string[],
    skipId: string | null,
  ): Promise<void> {
    const targets = tabs.value.filter(
      (tab) =>
        tab.id !== skipId &&
        tab.rootName === root &&
        dirs.includes(tab.path),
    );
    await Promise.all(targets.map((tab) => loadEntriesFor(tab)));
  }

  let initPromise: Promise<void> | null = null;

  function init(
    nextServerId: string,
    snapshot: FilesTabsSnapshot | null = null,
  ): Promise<void> {
    if (serverId.value !== nextServerId) {
      resetBrowser();
      serverId.value = nextServerId;
      initPromise = null;
    }
    if (initPromise === null) {
      const started = initialLoad(snapshot);
      initPromise = started;
      void started.finally(() => {
        if (initPromise === started) {
          initPromise = null;
        }
      });
    }
    return initPromise;
  }

  let touchedDuringInit = false;

  function rebuildTabsFromSnapshot(
    snapshot: FilesTabsSnapshot,
  ): { tabs: FilesTab[]; activeId: string } | null {
    const defaultRoot = roots.value[0]?.name ?? "";
    if (defaultRoot === "") {
      return null;
    }
    const seen = new Set<string>();
    const rebuilt: FilesTab[] = [];
    for (const item of snapshot.tabs.slice(0, FILES_TABS_MAX_TABS)) {
      const known = roots.value.some((root) => root.name === item.root);
      const tab = createTab(
        known ? item.root : defaultRoot,
        known ? item.path : "",
        item.id !== "" && !seen.has(item.id) ? item.id : nextTabId(),
      );
      seen.add(tab.id);
      tab.viewMode = snapshot.viewMode;
      rebuilt.push(tab);
    }
    if (rebuilt.length === 0) {
      return null;
    }
    return {
      tabs: rebuilt,
      activeId: seen.has(snapshot.activeTabId)
        ? snapshot.activeTabId
        : rebuilt[0].id,
    };
  }

  function activateRebuiltTabs(
    rebuilt: FilesTab[],
    activeId: string,
  ): FilesTab {
    for (const discarded of tabs.value) {
      discarded.generation += 1;
      searchAborts.get(discarded.id)?.abort();
      searchAborts.delete(discarded.id);
      pendingRestore.delete(discarded.id);
    }
    tabs.value = rebuilt;
    activeTabId.value = rebuilt.some((tab) => tab.id === activeId)
      ? activeId
      : rebuilt[0].id;
    for (const tab of rebuilt) {
      if (tab.id !== activeTabId.value) {
        pendingRestore.add(tab.id);
      }
    }
    return findTab(activeTabId.value) ?? rebuilt[0];
  }

  async function initialLoad(snapshot: FilesTabsSnapshot | null): Promise<void> {
    const server = serverId.value;
    if (server === "") {
      return;
    }
    touchedDuringInit = false;
    const tab = activeTab.value;
    const gen = ++tab.generation;
    tab.loading = true;
    tab.errorRaw = null;
    tab.disabled = false;
    resetFolderSizes();
    try {
      const list = await listRoots(server, teamId());
      if (gen !== tab.generation || server !== serverId.value) {
        return;
      }
      const alreadyLoaded = rootsServerId.value === server;
      roots.value = list;
      rootsServerId.value = server;
      revalidateRoots(list);
      const rebuilt =
        snapshot === null || touchedDuringInit || alreadyLoaded
          ? null
          : rebuildTabsFromSnapshot(snapshot);
      if (rebuilt === null) {
        const current = findTab(tab.id) ?? activeTab.value;
        if (current.id !== tab.id) {
          tab.loading = false;
        }
        await loadEntriesFor(current);
        return;
      }
      const active = activateRebuiltTabs(rebuilt.tabs, rebuilt.activeId);
      await loadRestoredTab(active);
    } catch (err) {
      if (gen !== tab.generation || server !== serverId.value) {
        return;
      }
      applyErrorTo(tab, err);
    } finally {
      if (gen === tab.generation) {
        tab.loading = false;
      }
    }
  }

  async function refresh(): Promise<void> {
    resetSearch(activeTab.value);
    await loadEntries();
  }

  async function refreshDirectory(root: string, dir: string): Promise<void> {
    const matching = tabs.value.filter(
      (tab) => tab.rootName === root && tab.path === dir,
    );
    if (matching.length === 0) {
      await loadEntries();
      return;
    }
    await Promise.all(matching.map((tab) => loadEntriesFor(tab)));
  }

  async function retry(): Promise<void> {
    if (roots.value.length === 0) {
      await loadAll();
      return;
    }
    await loadEntries();
  }

  async function loadMore(): Promise<void> {
    const tab = activeTab.value;
    if (
      !serverId.value ||
      tab.rootName === "" ||
      tab.cursor === "" ||
      tab.loadingMore
    ) {
      return;
    }
    const gen = tab.generation;
    tab.loadingMore = true;
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: tab.rootName,
        path: tab.path,
        cursor: tab.cursor,
      });
      if (gen !== tab.generation) {
        return;
      }
      tab.entries = [...tab.entries, ...listing.entries];
      tab.cursor = listing.cursor ?? "";
      tab.truncated = listing.truncated;
    } catch (err) {
      if (gen !== tab.generation) {
        return;
      }
      applyErrorTo(tab, err);
    } finally {
      tab.loadingMore = false;
    }
  }

  async function openTab(
    options: { root?: string; path?: string } = {},
  ): Promise<void> {
    touchedDuringInit = true;
    const root = options.root ?? rootName.value;
    if (!serverId.value || root === "") {
      return;
    }
    const created = createTab(root, options.path ?? "");
    tabs.value = [...tabs.value, created];
    activeTabId.value = created.id;
    await loadEntriesFor(findTab(created.id) ?? created);
  }

  async function closeTab(id: string): Promise<void> {
    touchedDuringInit = true;
    const index = tabs.value.findIndex((tab) => tab.id === id);
    if (index === -1) {
      return;
    }
    searchAborts.get(id)?.abort();
    searchAborts.delete(id);
    pendingRestore.delete(id);
    const wasActive = activeTabId.value === id;
    const closedRoot = tabs.value[index].rootName;
    const remaining = tabs.value.filter((tab) => tab.id !== id);
    if (remaining.length === 0) {
      const created = createTab(closedRoot);
      tabs.value = [created];
      activeTabId.value = created.id;
      const tab = findTab(created.id) ?? created;
      if (serverId.value && tab.rootName !== "") {
        await loadEntriesFor(tab);
      }
      return;
    }
    tabs.value = remaining;
    if (wasActive) {
      await switchTab(remaining[Math.min(index, remaining.length - 1)].id);
    }
  }

  async function switchTab(id: string): Promise<void> {
    touchedDuringInit = true;
    const tab = findTab(id);
    if (!tab) {
      return;
    }
    activeTabId.value = tab.id;
    tab.anchor = null;
    if (pendingRestore.delete(tab.id)) {
      await loadRestoredTab(tab);
    } else if (!tab.loaded && !tab.loading) {
      await loadEntriesFor(tab);
    }
  }

  function collectTabsSnapshot(): FilesTabsSnapshot | null {
    if (serverId.value === "" || roots.value.length === 0) {
      return null;
    }
    const kept = tabs.value.filter((tab) => tab.rootName !== "");
    if (kept.length === 0) {
      return null;
    }
    return {
      version: 1,
      tabs: kept.slice(0, FILES_TABS_MAX_TABS).map((tab) => ({
        id: tab.id,
        root: tab.rootName,
        path: tab.path,
      })),
      activeTabId: activeTabId.value,
      viewMode: activeTab.value.viewMode,
    };
  }

  async function loadRestoredTab(tab: FilesTab): Promise<void> {
    if (serverId.value === "" || tab.rootName === "") {
      tab.entries = [];
      tab.cursor = "";
      tab.truncated = false;
      return;
    }
    const gen = ++tab.generation;
    tab.errorRaw = null;
    resetFolderSizes();
    let target = tab.path;
    for (;;) {
      if (gen !== tab.generation) {
        return;
      }
      tab.loading = true;
      try {
        const listing = await listEntries(serverId.value, teamId(), {
          root: tab.rootName,
          path: target,
        });
        if (gen !== tab.generation) {
          return;
        }
        tab.path = target;
        applyListingTo(tab, listing.entries, listing.cursor ?? "", listing.truncated);
        return;
      } catch (err) {
        if (gen !== tab.generation || isCanceledError(err)) {
          return;
        }
        if (isFilesDisabled(err)) {
          applyErrorTo(tab, err);
          return;
        }
        const key = filesErrorKey(err);
        const gone =
          key === "files.errors.notFound" ||
          key === "files.errors.invalid" ||
          key === "files.errors.forbidden";
        if (gone && target !== "") {
          target = parentPath(target);
          continue;
        }
        if (gone) {
          tab.path = "";
        }
        applyErrorTo(tab, err);
        return;
      } finally {
        if (gen === tab.generation) {
          tab.loading = false;
        }
      }
    }
  }

  async function restorePersistedTabs(snapshot: FilesTabsSnapshot): Promise<boolean> {
    if (serverId.value === "" || roots.value.length === 0) {
      return false;
    }
    const rebuilt = rebuildTabsFromSnapshot(snapshot);
    if (rebuilt === null) {
      return false;
    }
    await loadRestoredTab(activateRebuiltTabs(rebuilt.tabs, rebuilt.activeId));
    return true;
  }

  async function activateRelative(delta: number): Promise<void> {
    if (tabs.value.length < 2) {
      return;
    }
    const index = tabs.value.findIndex((tab) => tab.id === activeTabId.value);
    const next = (index + delta + tabs.value.length) % tabs.value.length;
    await switchTab(tabs.value[next].id);
  }

  async function nextTab(): Promise<void> {
    await activateRelative(1);
  }

  async function prevTab(): Promise<void> {
    await activateRelative(-1);
  }

  function setScrollTop(top: number): void {
    activeTab.value.scrollTop = top;
  }

  async function runMutation<T>(task: () => Promise<T>): Promise<T> {
    acting.value = true;
    try {
      return await task();
    } finally {
      acting.value = false;
    }
  }

  async function createEntry(name: string, kind: NameKind): Promise<FilesEntry> {
    const tab = activeTab.value;
    const root = tab.rootName;
    const dir = tab.path;
    const target = joinPath(dir, normalizeEntryName(name));
    return runMutation(async () => {
      const created =
        kind === "directory"
          ? await createFolder(serverId.value, teamId(), {
              root,
              path: target,
            })
          : await createFile(serverId.value, teamId(), {
              root,
              path: target,
            });
      await loadEntriesFor(tab);
      await refreshTabsShowing(root, [dir], tab.id);
      return created;
    });
  }

  async function rename(entry: FilesEntry, nextName: string): Promise<void> {
    const tab = activeTab.value;
    const root = tab.rootName;
    const dir = tab.path;
    const next = normalizeEntryName(nextName);
    await runMutation(async () => {
      await renameEntry(serverId.value, teamId(), {
        root,
        source: joinPath(dir, entry.name),
        destination: joinPath(dir, next),
      });
      renameSelection(tab, entry.name, next);
      await withEditor((editor) =>
        editor.applyRename(root, dir, entry.name, next),
      );
      const clip = clipboard.value;
      if (
        clip &&
        clip.root === root &&
        clip.path === dir &&
        clip.names.includes(entry.name)
      ) {
        clipboard.value = {
          ...clip,
          names: clip.names.map((name) => (name === entry.name ? next : name)),
          directories: clip.directories.map((name) =>
            name === entry.name ? next : name,
          ),
        };
      }
      await loadEntriesFor(tab);
      await refreshTabsShowing(root, [dir], tab.id);
    });
  }

  function isDirectory(entry: FilesEntry): boolean {
    return entry.kind === "directory";
  }

  async function chmod(entry: FilesEntry, mode: number): Promise<void> {
    const tab = activeTab.value;
    const root = tab.rootName;
    const dir = tab.path;
    await runMutation(async () => {
      await chmodEntry(serverId.value, teamId(), {
        root,
        path: joinPath(dir, entry.name),
        mode,
      });
      await loadEntriesFor(tab);
      await refreshTabsShowing(root, [dir], tab.id);
    });
  }

  async function compress(
    entries: FilesEntry[],
    name: string,
    format: ArchiveFormat,
  ): Promise<FilesEntry> {
    const tab = activeTab.value;
    const root = tab.rootName;
    const dir = tab.path;
    const destination = joinPath(dir, normalizeEntryName(name));
    return runMutation(async () => {
      const created = await compressEntries(serverId.value, teamId(), {
        root,
        sources: entries.map((entry) => joinPath(dir, entry.name)),
        destination,
        format,
      });
      await loadEntriesFor(tab);
      await refreshTabsShowing(root, [dir], tab.id);
      return created;
    });
  }

  async function extract(entry: FilesEntry, overwrite: boolean): Promise<void> {
    const tab = activeTab.value;
    const root = tab.rootName;
    const dir = tab.path;
    await runMutation(async () => {
      await extractEntry(serverId.value, teamId(), {
        root,
        source: joinPath(dir, entry.name),
        destination: dir,
        overwrite,
      });
      await loadEntriesFor(tab);
      await refreshTabsShowing(root, [dir], tab.id);
    });
  }

  async function remove(entries: FilesEntry[]): Promise<TransferSummary> {
    const tab = activeTab.value;
    const root = tab.rootName;
    const dir = tab.path;
    return runMutation(async () => {
      const summary: TransferSummary = { done: [], conflicts: [], failed: [] };
      for (const entry of entries) {
        try {
          await deleteEntry(
            serverId.value,
            teamId(),
            root,
            joinPath(dir, entry.name),
            isDirectory(entry),
          );
          summary.done.push(entry.name);
        } catch (err) {
          summary.failed.push({ name: entry.name, error: describeFilesError(err) });
        }
      }
      pruneClipboard(summary.done, root, dir);
      await withEditor((editor) => editor.applyDelete(root, dir, summary.done));
      await loadEntriesFor(tab);
      await refreshTabsShowing(root, [dir], tab.id);
      return summary;
    });
  }

  async function runTransfer(
    tab: FilesTab,
    op: "copy" | "move",
    source: string,
    destination: string,
    names: string[],
    overwrite: boolean,
  ): Promise<TransferSummary> {
    const root = tab.rootName;
    const summary: TransferSummary = { done: [], conflicts: [], failed: [] };
    const call = op === "copy" ? copyEntry : moveEntry;
    for (const name of names) {
      try {
        await call(serverId.value, teamId(), {
          root,
          source: joinPath(source, name),
          destination: joinPath(destination, name),
          overwrite,
        });
        summary.done.push(name);
      } catch (err) {
        if (isConflictError(err)) {
          summary.conflicts.push(name);
        } else {
          summary.failed.push({ name, error: describeFilesError(err) });
        }
      }
    }
    if (op === "move" && summary.done.length > 0) {
      await withEditor((editor) =>
        editor.applyMove(root, source, destination, summary.done),
      );
    }
    await loadEntriesFor(tab);
    if (op === "move" && summary.done.length > 0) {
      await refreshTabsShowing(root, [source, destination], tab.id);
    }
    return summary;
  }

  async function promptTransfer(
    op: "copy" | "move",
    source: string,
    destination: string,
    names: string[],
    cutClipboard = false,
  ): Promise<TransferSummary> {
    const tab = activeTab.value;
    const summary = await runMutation(() =>
      runTransfer(tab, op, source, destination, names, false),
    );
    conflictPrompt.value =
      summary.conflicts.length > 0
        ? {
            op,
            root: tab.rootName,
            tabId: tab.id,
            source,
            destination,
            names: [...summary.conflicts],
            cutClipboard,
          }
        : null;
    if (cutClipboard && op === "move") {
      pruneClipboard(summary.done, tab.rootName, source);
    }
    return summary;
  }

  async function resolveConflictPrompt(overwrite: boolean): Promise<TransferSummary> {
    const prompt = conflictPrompt.value;
    conflictPrompt.value = null;
    if (!prompt) {
      return { done: [], conflicts: [], failed: [] };
    }
    const tab = findTab(prompt.tabId) ?? activeTab.value;
    if (!overwrite) {
      return { done: [], conflicts: prompt.names, failed: [] };
    }
    const summary = await runMutation(() =>
      runTransfer(tab, prompt.op, prompt.source, prompt.destination, prompt.names, true),
    );
    if (prompt.cutClipboard && prompt.op === "move") {
      pruneClipboard(summary.done, tab.rootName, prompt.source);
    }
    return summary;
  }

  function copySelection(names: string[] = selectedNames.value): void {
    markClipboard("copy", names);
  }

  function cutSelection(names: string[] = selectedNames.value): void {
    markClipboard("cut", names);
  }

  function markClipboard(mode: "copy" | "cut", names: string[]): void {
    if (names.length === 0) {
      return;
    }
    const wanted = new Set(names);
    const directories = entries.value
      .filter((entry) => entry.kind === "directory" && wanted.has(entry.name))
      .map((entry) => entry.name);
    clipboard.value = {
      mode,
      root: rootName.value,
      path: path.value,
      names: [...names],
      directories,
    };
  }

  function clearClipboard(): void {
    clipboard.value = null;
  }

  async function pasteInto(destination: string): Promise<TransferSummary> {
    const clip = clipboard.value;
    if (
      !clip ||
      clip.names.length === 0 ||
      pasteProblemAt(destination) !== null
    ) {
      return { done: [], conflicts: [], failed: [] };
    }
    return promptTransfer(
      clip.mode === "copy" ? "copy" : "move",
      clip.path,
      destination,
      clip.names,
      clip.mode === "cut",
    );
  }

  async function paste(): Promise<TransferSummary> {
    return pasteInto(path.value);
  }

  async function reload(): Promise<void> {
    resetBrowser();
    await loadAll();
  }

  async function setRoot(name: string): Promise<void> {
    touchedDuringInit = true;
    const tab = activeTab.value;
    if (name === tab.rootName) {
      return;
    }
    tab.rootName = name;
    tab.path = "";
    tab.scrollTop = 0;
    tab.query = "";
    clearSearch();
    clearSelection();
    await loadEntriesFor(tab);
  }

  async function openDirectory(name: string): Promise<void> {
    touchedDuringInit = true;
    const tab = activeTab.value;
    tab.path = joinPath(tab.path, name);
    tab.scrollTop = 0;
    tab.query = "";
    clearSearch();
    clearSelection();
    await loadEntriesFor(tab);
  }

  async function openAt(root: string, dir: string): Promise<void> {
    touchedDuringInit = true;
    const existing = tabs.value.find(
      (tab) => tab.rootName === root && tab.path === dir,
    );
    if (existing) {
      if (existing.id !== activeTabId.value) {
        await switchTab(existing.id);
      }
      return;
    }
    await openTab({ root, path: dir });
  }

  async function goToPath(next: string): Promise<void> {
    touchedDuringInit = true;
    const tab = activeTab.value;
    if (next === tab.path) {
      tab.query = "";
      tab.anchor = null;
      clearSearch();
      return;
    }
    tab.path = next;
    tab.scrollTop = 0;
    tab.query = "";
    clearSearch();
    clearSelection();
    await loadEntriesFor(tab);
  }

  async function openSymlinkTarget(target: string): Promise<SymlinkTargetResult> {
    const tab = activeTab.value;
    if (!serverId.value || tab.rootName === "" || target === tab.path) {
      tab.query = "";
      tab.anchor = null;
      return "opened";
    }
    const gen = ++tab.generation;
    tab.loading = true;
    resetFolderSizes();
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: tab.rootName,
        path: target,
      });
      if (gen !== tab.generation) {
        return "opened";
      }
      touchedDuringInit = true;
      tab.path = target;
      tab.scrollTop = 0;
      tab.query = "";
      tab.errorRaw = null;
      clearSelection();
      applyListingTo(tab, listing.entries, listing.cursor ?? "", listing.truncated);
      return "opened";
    } catch (err) {
      if (gen !== tab.generation || isCanceledError(err)) {
        return "opened";
      }
      const key = filesErrorKey(err);
      return key === "files.errors.notFound" ||
        key === "files.errors.invalid" ||
        key === "files.errors.forbidden"
        ? "not-folder"
        : "error";
    } finally {
      if (gen === tab.generation) {
        tab.loading = false;
      }
    }
  }

  async function goUp(): Promise<void> {
    if (path.value === "") {
      return;
    }
    await goToPath(parentPath(path.value));
  }

  function setSort(key: FilesSortKey): void {
    activeTab.value.anchor = null;
    if (sortKey.value === key) {
      sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
      return;
    }
    sortKey.value = key;
    sortDir.value = "asc";
  }

  function setViewMode(mode: FilesViewMode): void {
    touchedDuringInit = true;
    viewMode.value = mode;
  }

  function setQuery(next: string): void {
    activeTab.value.anchor = null;
    query.value = next;
  }

  function resetSearch(tab: FilesTab): void {
    tab.searchGeneration += 1;
    searchAborts.get(tab.id)?.abort();
    searchAborts.delete(tab.id);
    tab.searchActive = false;
    tab.searchQuery = "";
    tab.searchResults = [];
    tab.searchTruncated = false;
    tab.searchLoading = false;
  }

  function clearSearch(): void {
    const tab = activeTab.value;
    resetSearch(tab);
    tab.query = "";
  }

  function setRecursive(next: boolean): void {
    const tab = activeTab.value;
    tab.recursive = next;
    clearSearch();
    tab.anchor = null;
  }

  async function runSearch(): Promise<void> {
    const tab = activeTab.value;
    const submitted = tab.query.trim();
    if (!tab.recursive || !serverId.value || tab.rootName === "" || submitted === "") {
      resetSearch(tab);
      return;
    }
    const gen = ++tab.searchGeneration;
    const controller = new AbortController();
    searchAborts.get(tab.id)?.abort();
    searchAborts.set(tab.id, controller);

    tab.searchActive = true;
    tab.searchLoading = true;
    tab.searchQuery = submitted;
    tab.searchResults = [];
    tab.searchTruncated = false;
    tab.errorRaw = null;
    try {
      const listing = await searchEntries(serverId.value, teamId(), {
        root: tab.rootName,
        path: tab.path,
        query: submitted,
        recursive: true,
        signal: controller.signal,
      });
      if (gen !== tab.searchGeneration) {
        return;
      }
      tab.searchResults = listing.results;
      tab.searchTruncated = listing.truncated;
    } catch (err) {
      if (gen !== tab.searchGeneration || isCanceledError(err)) {
        return;
      }
      tab.searchActive = false;
      tab.searchResults = [];
      applyErrorTo(tab, err);
    } finally {
      if (gen === tab.searchGeneration) {
        tab.searchLoading = false;
      }
      if (searchAborts.get(tab.id) === controller) {
        searchAborts.delete(tab.id);
      }
    }
  }

  async function openSearchHit(hit: FilesSearchHit): Promise<void> {
    const target =
      hit.entry.kind === "directory" ? hit.path : parentPath(hit.path);
    clearSearch();
    await goToPath(target);
  }

  function isSelected(name: string): boolean {
    return selected.value.has(name);
  }

  function targetsForRow(entry: FilesEntry): FilesEntry[] {
    return selected.value.has(entry.name) ? selectedEntries.value : [entry];
  }

  function selectForRowContext(entry: FilesEntry): void {
    if (!isSelectable(entry) || selected.value.has(entry.name)) {
      return;
    }
    selected.value = new Set([entry.name]);
    activeTab.value.anchor = entry.name;
  }

  function selectOnly(name: string): void {
    selected.value = new Set([name]);
    activeTab.value.anchor = name;
  }

  function setAnchor(name: string | null): void {
    activeTab.value.anchor = name;
  }

  function toggleSelection(name: string): void {
    const next = new Set(selected.value);
    if (next.has(name)) {
      next.delete(name);
    } else {
      next.add(name);
    }
    selected.value = next;
    activeTab.value.anchor = name;
  }

  function selectRange(name: string, additive: boolean): void {
    const tab = activeTab.value;
    const entries = visibleEntries.value;
    const target = entries.find((entry) => entry.name === name);
    if (target === undefined || !isSelectable(target)) {
      return;
    }
    const anchor = tab.anchor;
    const anchored =
      anchor !== null && entries.some((entry) => entry.name === anchor);
    const range = anchored ? rangeNames(entries, anchor, name) : [name];
    const next = additive ? new Set(selected.value) : new Set<string>();
    for (const entryName of range) {
      next.add(entryName);
    }
    selected.value = next;
    if (!anchored) {
      tab.anchor = name;
    }
  }

  function toggleSelectAll(): void {
    const next = new Set(selected.value);
    if (allVisibleSelected.value) {
      for (const entry of selectableVisible.value) {
        next.delete(entry.name);
      }
    } else {
      for (const entry of selectableVisible.value) {
        next.add(entry.name);
      }
      if (activeTab.value.anchor === null) {
        activeTab.value.anchor = selectableVisible.value[0]?.name ?? null;
      }
    }
    selected.value = next;
  }

  function clearSelection(): void {
    selected.value = new Set();
    activeTab.value.anchor = null;
  }

  function resetBrowser(): void {
    for (const discarded of tabs.value) {
      discarded.generation += 1;
      searchAborts.get(discarded.id)?.abort();
      searchAborts.delete(discarded.id);
    }
    pendingRestore.clear();
    const tab = createTab();
    tabs.value = [tab];
    activeTabId.value = tab.id;
    roots.value = [];
    rootsServerId.value = "";
    acting.value = false;
    clipboard.value = null;
    conflictPrompt.value = null;
  }

  function reset(): void {
    resetBrowser();
    serverId.value = "";
  }

  return {
    serverId,
    roots,
    rootsServerId,
    tabs,
    activeTabId,
    activeTab,
    rootName,
    path,
    entries,
    cursor,
    truncated,
    loading,
    loadingMore,
    loaded,
    disabled,
    permissionDenied,
    acting,
    clipboard,
    clipboardMode,
    conflictPrompt,
    viewMode,
    sortKey,
    sortDir,
    query,
    recursive,
    searchActive,
    searchQuery,
    searchResults,
    searchTruncated,
    searchLoading,
    scrollTop,
    error,
    errorRaw,
    currentRoot,
    writable,
    freeBytes,
    breadcrumb,
    hasMore,
    counts,
    visibleCounts,
    visibleEntries,
    selectedNames,
    selectedEntries,
    selectedBytes,
    allVisibleSelected,
    someVisibleSelected,
    canReadContent,
    canWrite,
    hasClipboard,
    pasteProblem,
    pasteProblemAt,
    init,
    refresh,
    refreshDirectory,
    retry,
    reload,
    loadMore,
    openTab,
    closeTab,
    switchTab,
    nextTab,
    prevTab,
    collectTabsSnapshot,
    restorePersistedTabs,
    setScrollTop,
    setRoot,
    openAt,
    openDirectory,
    goToPath,
    openSymlinkTarget,
    goUp,
    setSort,
    setViewMode,
    setQuery,
    setRecursive,
    runSearch,
    clearSearch,
    openSearchHit,
    isSelected,
    targetsForRow,
    selectForRowContext,
    selectOnly,
    selectRange,
    setAnchor,
    toggleSelection,
    toggleSelectAll,
    clearSelection,
    createEntry,
    rename,
    chmod,
    remove,
    compress,
    extract,
    promptTransfer,
    resolveConflictPrompt,
    copySelection,
    cutSelection,
    clearClipboard,
    paste,
    pasteInto,
    reset,
  };
});
