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
  isConflictError,
  isFilesDisabled,
  listEntries,
  listRoots,
  moveEntry,
  renameEntry,
} from "@/features/files/api/files";
import type { ArchiveFormat } from "@/features/files/api/files";
import type {
  FilesEntry,
  FilesRoot,
  FileKind,
} from "@/features/files/schemas/files";
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

/** FilesClipboard is a pending copy/cut of named entries from one directory. */
export interface FilesClipboard {
  mode: "copy" | "cut";
  root: string;
  path: string;
  names: string[];
  /** directories are the clipboard names that are folders, so pasting a folder
   *  into its own subtree is refused before the agent call (400). */
  directories: string[];
}

/** ConflictPrompt waits for the user to replace or skip 409'd names. */
export interface ConflictPrompt {
  op: "copy" | "move";
  /** root is the root the transfer ran in, captured when the prompt opened. */
  root: string;
  /** tabId binds the resolution to the tab that started the transfer, so a tab
   *  switch while the prompt is open cannot retarget the retry. */
  tabId: string;
  source: string;
  destination: string;
  names: string[];
  /** cutClipboard clears the clipboard for a cut once the prompt resolves. */
  cutClipboard: boolean;
}

/** TransferSummary reports one batch move/copy/delete outcome. */
export interface TransferSummary {
  done: string[];
  conflicts: string[];
  failed: Array<{ name: string; error: string }>;
}

/**
 * FilesTab is one browser tab's independent view state: its own root, path,
 * listing, selection, sort/filter, view mode and scroll offset. The write
 * clipboard and the replace/skip prompt stay store-level (shared) so a cut in
 * one tab can be pasted in another.
 */
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
  selected: Set<string>;
  /** scrollTop is the entry list's scroll offset, restored on tab switch. */
  scrollTop: number;
  /** lastLoadedPath is the path of the last successful listing in this tab. */
  lastLoadedPath: string;
  /** generation drops a listing whose tab navigated or reloaded since. */
  generation: number;
}

/**
 * EditorSync is the slice of the editor tab store the browser drives when a
 * mutation changes the filesystem under an open document. It is structural so
 * the store can be reached lazily (see {@link withEditor}) without a static
 * import.
 */
interface EditorSync {
  applyRename: (_root: string, _dir: string, _oldName: string, _newName: string) => void;
  applyMove: (_root: string, _fromDir: string, _toDir: string, _names: string[]) => void;
  applyDelete: (_root: string, _dir: string, _names: string[]) => void;
}

/**
 * withEditor reaches the editor tab store through a dynamic import. The files
 * store must not import it statically: the auth store (in the entry chunk)
 * resets the files store on sign-out, so a static import would drag the editor
 * store and its helpers into the entry bundle. The lazy import keeps them in
 * the files-page chunk while every mutation still routes its tab sync through
 * one place.
 */
async function withEditor(sync: (_editor: EditorSync) => void): Promise<void> {
  const { useEditorStore } = await import("@/features/files/stores/editor");
  sync(useEditorStore());
}

let tabSeq = 0;

/** nextTabId mints a stable per-session browser tab id. */
function nextTabId(): string {
  tabSeq += 1;
  return `ft-${Date.now().toString(36)}-${tabSeq}`;
}

/**
 * Files browser store. Holds one server's browsable roots plus a list of
 * independent browser tabs (JUS-123): the active tab owns the directory page,
 * the selection, the sort/filter, the view mode and the scroll offset, while the
 * clipboard, the acting flag and the replace/skip prompt stay shared across
 * tabs. Every read and every write is team-scoped: the active team is read from
 * the teams store and sent as `X-Team-Id`.
 *
 * Concurrency: each tab carries its own generation counter that drops a
 * response which lost to a newer navigation in that tab, so a slow listing can
 * never overwrite the directory the user just opened. Reads are open to any
 * member; content, download and every mutation require the owner or admin role,
 * enforced a second time by the control plane.
 */
export const useFilesStore = defineStore("files", () => {
  const serverId = ref("");
  // roots are server-level (the same list for every tab); the selected root is
  // per tab, so two tabs can browse different roots of the same server.
  const roots = ref<FilesRoot[]>([]);

  /** createTab seeds one independent browser tab. */
  function createTab(rootName = "", path = ""): FilesTab {
    return {
      id: nextTabId(),
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
      selected: new Set(),
      scrollTop: 0,
      lastLoadedPath: "",
      generation: 0,
    };
  }

  const initialTab = createTab();
  const tabs = ref<FilesTab[]>([initialTab]);
  const activeTabId = ref(initialTab.id);

  /** findTab resolves one tab by id. */
  function findTab(id: string): FilesTab | null {
    return tabs.value.find((tab) => tab.id === id) ?? null;
  }

  /** activeTab is the focused tab; the list always keeps at least one. */
  const activeTab = computed<FilesTab>(
    () => findTab(activeTabId.value) ?? tabs.value[0],
  );

  // ── Per-tab state, read/written through the active tab ────────────────
  // Writable computed proxies keep the previous flat API (store.path,
  // store.entries, …) while every read and write lands on the active tab, so
  // the components, dialogs and the editor sync keep working unchanged.
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
  const selected = computed<Set<string>>({
    get: () => activeTab.value.selected,
    set: (value) => {
      activeTab.value.selected = value;
    },
  });
  /** scrollTop is the active tab's entry-list scroll offset. */
  const scrollTop = computed<number>({
    get: () => activeTab.value.scrollTop,
    set: (value) => {
      activeTab.value.scrollTop = value;
    },
  });

  // ── Shared (cross-tab) state ──────────────────────────────────────────
  // The write surface mirrors the read gates: canWrite needs both a
  // write-capable role and a writable root. A read_only member or a
  // read-only root hides every mutation affordance and every mutating call
  // is refused by the control plane regardless.
  const acting = ref(false);
  const clipboard = ref<FilesClipboard | null>(null);
  /** conflictPrompt is set when a transfer met 409s and awaits replace/skip. */
  const conflictPrompt = ref<ConflictPrompt | null>(null);

  /** error renders the retained read failure in the active locale. */
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeFilesError(errorRaw.value),
  );

  /** currentRoot is the active tab's selected root object, if any. */
  const currentRoot = computed<FilesRoot | null>(
    () => roots.value.find((root) => root.name === rootName.value) ?? null,
  );

  /** writable marks the selected root writable (compose roots are not). */
  const writable = computed<boolean>(() => currentRoot.value?.writable ?? false);

  /** freeBytes is the selected root's free space (0 when unknown). */
  const freeBytes = computed<number>(() => currentRoot.value?.free_bytes ?? 0);

  /** breadcrumb is root → … → current path. */
  const breadcrumb = computed<BreadcrumbSegment[]>(() =>
    breadcrumbSegments(rootName.value, path.value),
  );

  /** hasMore reports a server cursor for the next page. */
  const hasMore = computed<boolean>(() => cursor.value !== "");

  const counts = computed(() => countByKind(entries.value));

  /** visibleEntries applies the folder filter, then the active sort. */
  const visibleEntries = computed<FilesEntry[]>(() => {
    const filtered =
      query.value.trim() === ""
        ? entries.value
        : entries.value.filter((entry) => entryMatchesQuery(entry, query.value));
    return sortEntries(filtered, sortKey.value, sortDir.value);
  });

  /** selectedNames is the current selection (insertion order). */
  const selectedNames = computed<string[]>(() => [...selected.value]);

  /** selectedEntries are the selected rows still present in the listing. */
  const selectedEntries = computed<FilesEntry[]>(() =>
    entries.value.filter((entry) => selected.value.has(entry.name)),
  );

  /** selectedBytes totals the selected entries' sizes. */
  const selectedBytes = computed<number>(() =>
    selectedEntries.value.reduce((sum, entry) => sum + entry.size, 0),
  );

  const selectableVisible = computed<FilesEntry[]>(() =>
    visibleEntries.value.filter(isSelectable),
  );

  /** allVisibleSelected drives the header select-all checkbox. */
  const allVisibleSelected = computed<boolean>(
    () =>
      selectableVisible.value.length > 0 &&
      selectableVisible.value.every((entry) => selected.value.has(entry.name)),
  );

  /** someVisibleSelected drives the header checkbox's indeterminate state. */
  const someVisibleSelected = computed<boolean>(
    () =>
      !allVisibleSelected.value &&
      selectableVisible.value.some((entry) => selected.value.has(entry.name)),
  );

  /**
   * canReadContent gates the owner/admin-only affordances (content and the
   * download ticket). An unknown role (teams not loaded) stays hidden rather
   * than promising a read the control plane would refuse.
   */
  const canReadContent = computed<boolean>(() => {
    const role = useTeamsStore().activeTeam?.role;
    return role === "owner" || role === "admin";
  });

  /**
   * canWrite gates every mutation: the caller must be able to write (owner or
   * admin) and the selected root must be writable (a compose root is not). It
   * is the single source of truth for hiding create/rename/delete/move/upload
   * affordances; the control plane refuses the call regardless.
   */
  const canWrite = computed<boolean>(() => canReadContent.value && writable.value);

  /** hasClipboard reports a non-empty clipboard for the paste action. */
  const hasClipboard = computed<boolean>(
    () => (clipboard.value?.names.length ?? 0) > 0,
  );

  /** clipboardMode reads the pending clipboard operation ("copy"/"cut"). */
  const clipboardMode = computed<"copy" | "cut" | null>(
    () => clipboard.value?.mode ?? null,
  );

  /**
   * pasteProblemAt explains why the clipboard cannot be pasted into
   * `destination`: another root, the clipboard's own directory, or a folder
   * inside a moved folder (all of which the agent refuses with 400). null means
   * the paste is valid. It mirrors the old destination-picker pre-check so a
   * paste never fires a doomed call.
   *
   * A copy into the clipboard's own directory is refused as "same-folder" too.
   * A cut into itself is a no-op, and a same-folder copy needs a "name copy"
   * rename the agent does not perform, so duplicating in place is deliberately
   * left out rather than faked (a follow-up can add the auto-rename).
   */
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

  /** pasteProblem is the current directory's paste validity. */
  const pasteProblem = computed<"cross-root" | TransferProblem | null>(() =>
    pasteProblemAt(path.value),
  );

  /** teamId reads the active team the requests are scoped to. */
  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** applyErrorTo records a read failure on one tab; a 503 flips the disabled
   *  state and a 403 the distinct permission-denied state. */
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

  /** applyListingTo stores one page on one tab and marks it loaded. */
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
    // Drop selection names that no longer exist (a refresh after another
    // session deleted them), so the bulk bar never counts a phantom row.
    pruneSelectionToEntries(tab);
  }

  /** pruneSelectionToEntries keeps only selected names still in the listing. */
  function pruneSelectionToEntries(tab: FilesTab): void {
    if (tab.selected.size === 0) {
      return;
    }
    const present = new Set(tab.entries.map((entry) => entry.name));
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

  /** renameSelection follows a renamed selected entry to its new name. */
  function renameSelection(tab: FilesTab, oldName: string, nextName: string): void {
    if (!tab.selected.has(oldName)) {
      return;
    }
    const next = new Set(tab.selected);
    next.delete(oldName);
    next.add(nextName);
    tab.selected = next;
  }

  /**
   * pruneClipboard drops names that no longer exist from the clipboard. It only
   * touches a clipboard taken from the same directory (root+path), so a delete
   * or rename in another directory never clears it or retargets it. The scope is
   * explicit for the cut-paste flow, where the moved names left the source
   * directory rather than the directory on screen.
   */
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

  /**
   * loadAll loads the roots, then the active tab's directory. Used on first
   * open and by retry; navigation uses {@link loadEntriesFor} alone.
   */
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
    try {
      const list = await listRoots(server, teamId());
      // A node/team switch discards this tab and bumps its generation (see
      // resetBrowser) and changes serverId, so a stale roots response can never
      // overwrite the shared roots of the node now on screen.
      if (gen !== tab.generation || server !== serverId.value) {
        return;
      }
      roots.value = list;
      // Re-validate EVERY tab's root against the refreshed list, not only the
      // active one: a background tab bound to a removed root would otherwise
      // error on its next load.
      for (const item of tabs.value) {
        if (!list.some((root) => root.name === item.rootName)) {
          item.rootName = list[0]?.name ?? "";
          item.path = "";
          item.loaded = false;
        }
      }
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

  /** loadEntriesFor loads one tab's current directory (navigation/refresh). */
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
      // A failed navigation has no data for the new path; a failed refresh
      // keeps the entries already on screen.
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

  /** loadEntries loads the active tab's directory only. */
  async function loadEntries(): Promise<void> {
    await loadEntriesFor(activeTab.value);
  }

  /**
   * refreshTabsShowing reloads every other tab whose root+path shows one of the
   * directories a mutation changed, so a cross-tab cut/paste (or a delete from
   * another tab) never leaves a stale listing behind. `skipId` is the tab the
   * caller has already refreshed.
   */
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

  /**
   * init binds the store to one server. Switching servers (nodes) resets every
   * tab back to a single default tab so a node never inherits the previous
   * node's tabs; a repeat call for the same server is a no-op once it has
   * loaded.
   */
  async function init(nextServerId: string): Promise<void> {
    if (serverId.value !== nextServerId) {
      resetBrowser();
      serverId.value = nextServerId;
    }
    if (loading.value) {
      return;
    }
    await loadAll();
  }

  /** refresh reloads the active tab's directory. */
  async function refresh(): Promise<void> {
    await loadEntries();
  }

  /**
   * refreshDirectory reloads every tab showing root+dir (the active tab when
   * none matches). Used by a background batch — an upload into a directory the
   * user may have tabbed away from — so the directory it landed in is not left
   * stale.
   */
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

  /** retry re-runs the first load after a failure. */
  async function retry(): Promise<void> {
    if (roots.value.length === 0) {
      await loadAll();
      return;
    }
    await loadEntries();
  }

  /** loadMore appends the next cursor page to the active tab. */
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

  // ── Tab actions (JUS-123) ─────────────────────────────────────────────

  /**
   * openTab opens a new independent tab at `path` under `root` (the active
   * tab's root and its top level by default) and focuses it. It is the "+"
   * button and the folder context menu's "Open in new tab".
   */
  async function openTab(
    options: { root?: string; path?: string } = {},
  ): Promise<void> {
    const root = options.root ?? rootName.value;
    if (!serverId.value || root === "") {
      return;
    }
    const created = createTab(root, options.path ?? "");
    tabs.value = [...tabs.value, created];
    activeTabId.value = created.id;
    // Read the reactive proxy back: the raw object pushed above is not reactive
    // on its own, so a mutation on it would never re-render the browser.
    await loadEntriesFor(findTab(created.id) ?? created);
  }

  /**
   * closeTab closes one tab and focuses a neighbour (the one to its left, else
   * the one to its right). Closing the last tab leaves a single default tab, so
   * the browser is never empty.
   */
  async function closeTab(id: string): Promise<void> {
    const index = tabs.value.findIndex((tab) => tab.id === id);
    if (index === -1) {
      return;
    }
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
      // Route through switchTab so a neighbour that was never loaded (or was
      // invalidated by loadAll's root re-validation) is fetched, not shown blank.
      await switchTab(remaining[Math.min(index, remaining.length - 1)].id);
    }
  }

  /** switchTab focuses one tab, loading it if it has never loaded. */
  async function switchTab(id: string): Promise<void> {
    const tab = findTab(id);
    if (!tab) {
      return;
    }
    activeTabId.value = tab.id;
    if (!tab.loaded && !tab.loading) {
      await loadEntriesFor(tab);
    }
  }

  /** activateRelative focuses the tab `delta` places away, wrapping around. */
  async function activateRelative(delta: number): Promise<void> {
    if (tabs.value.length < 2) {
      return;
    }
    const index = tabs.value.findIndex((tab) => tab.id === activeTabId.value);
    const next = (index + delta + tabs.value.length) % tabs.value.length;
    await switchTab(tabs.value[next].id);
  }

  /** nextTab focuses the tab to the right (wrapping). */
  async function nextTab(): Promise<void> {
    await activateRelative(1);
  }

  /** prevTab focuses the tab to the left (wrapping). */
  async function prevTab(): Promise<void> {
    await activateRelative(-1);
  }

  /** setScrollTop records the active tab's entry-list scroll offset. */
  function setScrollTop(top: number): void {
    activeTab.value.scrollTop = top;
  }

  // ── Write actions (P18 step 7) ────────────────────────────────────────
  // Every mutation runs under `acting` (so the dialogs can disable their
  // confirm button and spin), refreshes the acting tab's directory on success,
  // and rethrows so the caller owns the localized error toast. A batch
  // operation reports a per-name outcome instead of aborting on the first
  // failure. Each mutation captures its tab up front, so a tab switch while the
  // call is in flight cannot refresh or retarget another tab.

  /** runMutation wraps one write in the acting flag. */
  async function runMutation<T>(task: () => Promise<T>): Promise<T> {
    acting.value = true;
    try {
      return await task();
    } finally {
      acting.value = false;
    }
  }

  /** createEntry creates an empty file or a folder in the active directory. */
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

  /** rename changes one entry's name inside the active directory. */
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
      // Follow the rename in the selection and any pending clipboard that was
      // taken from this same directory (root+path): a rename elsewhere must not
      // retarget the clipboard to a file in another folder.
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

  /** isDirectory reports whether one entry needs a recursive delete. */
  function isDirectory(entry: FilesEntry): boolean {
    return entry.kind === "directory";
  }

  /**
   * chmod changes one entry's permission bits and refreshes the listing. Only
   * the low 0o777 bits are sent; the agent refuses setuid/setgid/sticky. A
   * successful call re-loads the directory so the new mode shows immediately.
   */
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

  /**
   * compress builds one archive from entries in the active directory and
   * returns the created archive's entry. It refreshes the listing on success
   * and rethrows so the dialog owns the localized error.
   */
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

  /**
   * extract unpacks one archive into the active directory. It refreshes the
   * listing on success and rethrows so the dialog owns the localized error.
   */
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

  /**
   * remove deletes entries, recursing into folders. A delete always moves the
   * entries to the root's recycle bin (permanent deletion happens only from the
   * bin). It never aborts on one failure: the summary reports the names that
   * were deleted and the ones that failed, so a partial batch still shows
   * feedback for every entry.
   */
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

  /**
   * runTransfer copies or moves names from source into destination (same root),
   * one entry at a time so a single 409 or failure does not hide the rest. It
   * does not prompt: callers that want the replace/skip flow call
   * {@link promptTransfer}. The acting tab is passed in, so a prompt resolved
   * after a tab switch still retries the tab that started the transfer.
   */
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
      // A move changes both the source and the destination directory. Refresh
      // every other tab still showing either, so a cross-tab cut/paste leaves
      // no stale listing (and no selection pointing at a moved path) behind.
      await refreshTabsShowing(root, [source, destination], tab.id);
    }
    return summary;
  }

  /**
   * promptTransfer runs a transfer with overwrite=false and, when names clash,
   * opens the replace/skip prompt instead of silently failing them. cutClipboard
   * marks a clipboard cut so the moved names are dropped from it; failed or
   * skipped names stay for a later retry. The prompt records the acting tab, so
   * a resolution after a tab switch still targets it.
   */
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
    // A cut clipboard is consumed only for the names that actually moved: a
    // failed or skipped entry stays so the user can retry it. The scope is the
    // source directory the names left, not the destination now on screen.
    if (cutClipboard && op === "move") {
      pruneClipboard(summary.done, tab.rootName, source);
    }
    return summary;
  }

  /** resolveConflictPrompt retries the 409'd names, or drops them (skip). */
  async function resolveConflictPrompt(overwrite: boolean): Promise<TransferSummary> {
    const prompt = conflictPrompt.value;
    conflictPrompt.value = null;
    if (!prompt) {
      return { done: [], conflicts: [], failed: [] };
    }
    // Resolve against the tab that started the transfer, even if the user has
    // since switched (or closed) tabs.
    const tab = findTab(prompt.tabId) ?? activeTab.value;
    if (!overwrite) {
      // Skipped names stay in a cut clipboard so a later paste can retry them.
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

  /**
   * copySelection copy-marks names (the active selection by default). A row
   * menu passes the single row it acted on, so it never has to mutate the
   * selection to stage a copy.
   */
  function copySelection(names: string[] = selectedNames.value): void {
    markClipboard("copy", names);
  }

  /** cutSelection cut-marks names (the active selection by default). */
  function cutSelection(names: string[] = selectedNames.value): void {
    markClipboard("cut", names);
  }

  /** markClipboard stores a non-empty clipboard for the active directory. */
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

  /** clearClipboard drops any pending copy/cut. */
  function clearClipboard(): void {
    clipboard.value = null;
  }

  /**
   * pasteInto applies the clipboard into `destination` (a folder in the active
   * root). It refuses an invalid destination up front (another root, the
   * clipboard's own folder, or a folder inside a moved folder), so the agent's
   * 400 is never reached; the caller reads {@link pasteProblemAt} to explain the
   * refusal. A name clash opens the replace/skip prompt; a cut clipboard is
   * consumed only for the names that actually moved.
   */
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

  /** paste applies the clipboard into the active directory. */
  async function paste(): Promise<TransferSummary> {
    return pasteInto(path.value);
  }

  /** reload re-reads roots and the listing (a team switch changes both). */
  async function reload(): Promise<void> {
    resetBrowser();
    await loadAll();
  }

  /** setRoot switches the active tab's root and returns to its top level. */
  async function setRoot(name: string): Promise<void> {
    const tab = activeTab.value;
    if (name === tab.rootName) {
      return;
    }
    tab.rootName = name;
    tab.path = "";
    tab.scrollTop = 0;
    clearSelection();
    await loadEntriesFor(tab);
  }

  /** openDirectory descends into one directory in the active tab. */
  async function openDirectory(name: string): Promise<void> {
    const tab = activeTab.value;
    tab.path = joinPath(tab.path, name);
    tab.scrollTop = 0;
    clearSelection();
    await loadEntriesFor(tab);
  }

  /** goToPath navigates the active tab to an absolute (root-relative) path. */
  async function goToPath(next: string): Promise<void> {
    const tab = activeTab.value;
    if (next === tab.path) {
      return;
    }
    tab.path = next;
    tab.scrollTop = 0;
    clearSelection();
    await loadEntriesFor(tab);
  }

  /** goUp navigates to the parent directory (no-op at the root). */
  async function goUp(): Promise<void> {
    if (path.value === "") {
      return;
    }
    await goToPath(parentPath(path.value));
  }

  /** setSort cycles a column: same key flips the direction, a new key resets. */
  function setSort(key: FilesSortKey): void {
    if (sortKey.value === key) {
      sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
      return;
    }
    sortKey.value = key;
    sortDir.value = "asc";
  }

  /** setViewMode switches the active tab between the list and grid layouts. */
  function setViewMode(mode: FilesViewMode): void {
    viewMode.value = mode;
  }

  /** setQuery updates the active tab's client-side folder filter. */
  function setQuery(next: string): void {
    query.value = next;
  }

  /** isSelected reports whether one entry name is selected. */
  function isSelected(name: string): boolean {
    return selected.value.has(name);
  }

  /**
   * targetsForRow resolves a row-menu action's targets: the whole current
   * selection when the row is part of it, otherwise just that row. It never
   * toggles the selection, so cutting an already-selected row cannot deselect
   * it and cut an empty set.
   */
  function targetsForRow(entry: FilesEntry): FilesEntry[] {
    return selected.value.has(entry.name) ? selectedEntries.value : [entry];
  }

  /**
   * selectForRowContext applies the right-click selection rule: an unselected
   * row becomes the sole selection, while a row already inside the selection
   * keeps the whole multi-selection for the menu. A non-selectable entry is
   * left untouched so the menu still resolves it through {@link targetsForRow}.
   */
  function selectForRowContext(entry: FilesEntry): void {
    if (!isSelectable(entry) || selected.value.has(entry.name)) {
      return;
    }
    selected.value = new Set([entry.name]);
  }

  /** toggleSelection adds or removes one entry from the selection. */
  function toggleSelection(name: string): void {
    const next = new Set(selected.value);
    if (next.has(name)) {
      next.delete(name);
    } else {
      next.add(name);
    }
    selected.value = next;
  }

  /** toggleSelectAll adds every visible selectable entry, or clears them. */
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
    }
    selected.value = next;
  }

  /** clearSelection drops the whole selection. */
  function clearSelection(): void {
    selected.value = new Set();
  }

  /**
   * resetBrowser clears every tab and leaves one default tab, keeping the bound
   * server id. It is the node-switch reset and the team-switch/ sign-out base.
   */
  function resetBrowser(): void {
    // Invalidate every tab being discarded: a listing (listRoots/listEntries)
    // still in flight for one of them must not write after the switch. The
    // shared `roots` is the dangerous one — a stale listRoots would replace the
    // new node's roots with the previous node's. Pre-PR this was a single
    // module-level counter bumped on reset; per-tab counters need the same bump.
    for (const discarded of tabs.value) {
      discarded.generation += 1;
    }
    const tab = createTab();
    tabs.value = [tab];
    activeTabId.value = tab.id;
    roots.value = [];
    acting.value = false;
    clipboard.value = null;
    conflictPrompt.value = null;
  }

  /**
   * reset clears the browser and unbinds the server, so the next sign-in never
   * sees the previous account's listings. Called on sign-out (see the auth
   * store).
   */
  function reset(): void {
    resetBrowser();
    serverId.value = "";
  }

  return {
    // state
    serverId,
    roots,
    // tabs
    tabs,
    activeTabId,
    activeTab,
    // active-tab state
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
    scrollTop,
    // derived
    error,
    currentRoot,
    writable,
    freeBytes,
    breadcrumb,
    hasMore,
    counts,
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
    // actions
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
    setScrollTop,
    setRoot,
    openDirectory,
    goToPath,
    goUp,
    setSort,
    setViewMode,
    setQuery,
    isSelected,
    targetsForRow,
    selectForRowContext,
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
