import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  describeFilesError,
  isFilesDisabled,
  listEntries,
  listRoots,
} from "@/features/files/api/files";
import type {
  FilesEntry,
  FilesRoot,
} from "@/features/files/schemas/files";
import {
  breadcrumbSegments,
  countByKind,
  entryMatchesQuery,
  isSelectable,
  joinPath,
  parentPath,
  sortEntries,
} from "@/features/files/utils/files";
import type {
  BreadcrumbSegment,
  FilesSortDirection,
  FilesSortKey,
  FilesViewMode,
} from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

/**
 * Files browser store (P18 step 6, read-only). Holds one server's browsable
 * roots plus the current directory page, the selection, the sort/filter and
 * the view mode. Every read is team-scoped: the active team is read from the
 * teams store and sent as `X-Team-Id`.
 *
 * Concurrency: a single generation counter drops a response that lost to a
 * newer navigation or server switch, so a slow listing can never overwrite the
 * directory the user just opened. Content/download require the owner or admin
 * role — a read_only member still lists, but the privileged affordances are
 * hidden by `canReadContent`.
 */
export const useFilesStore = defineStore("files", () => {
  const serverId = ref("");
  const roots = ref<FilesRoot[]>([]);
  const rootName = ref("");
  const path = ref("");
  const entries = ref<FilesEntry[]>([]);
  const cursor = ref("");
  const truncated = ref(false);
  const loading = ref(false);
  const loadingMore = ref(false);
  const loaded = ref(false);
  const disabled = ref(false);
  const errorRaw = ref<unknown>(null);
  const viewMode = ref<FilesViewMode>("list");
  const sortKey = ref<FilesSortKey>("name");
  const sortDir = ref<FilesSortDirection>("asc");
  const query = ref("");
  const selected = ref<Set<string>>(new Set());

  // Bumped by every navigation/server switch. A response whose generation is
  // stale is discarded (see the plan's navigation race guard).
  let generation = 0;
  // Path of the last successfully loaded listing, so a failed refresh keeps
  // the visible entries while a failed navigation clears them.
  let lastLoadedPath = "";

  /** error renders the retained read failure in the active locale. */
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeFilesError(errorRaw.value),
  );

  /** currentRoot is the selected root object, if any. */
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

  /** teamId reads the active team the requests are scoped to. */
  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** applyError records a read failure; a 503 flips the disabled state. */
  function applyError(err: unknown): void {
    if (isFilesDisabled(err)) {
      disabled.value = true;
      errorRaw.value = null;
      return;
    }
    errorRaw.value = err;
  }

  /** applyListing stores one page and marks the listing loaded. */
  function applyListing(entriesPage: FilesEntry[], nextCursor: string, isTruncated: boolean): void {
    entries.value = entriesPage;
    cursor.value = nextCursor;
    truncated.value = isTruncated;
    loaded.value = true;
    disabled.value = false;
    lastLoadedPath = path.value;
  }

  /**
   * loadAll loads the roots, then the current directory. Used on first open
   * and by retry; navigation uses {@link loadEntries} alone.
   */
  async function loadAll(): Promise<void> {
    if (!serverId.value) {
      return;
    }
    const gen = ++generation;
    loading.value = true;
    errorRaw.value = null;
    disabled.value = false;
    try {
      const list = await listRoots(serverId.value, teamId());
      if (gen !== generation) {
        return;
      }
      roots.value = list;
      if (!list.some((root) => root.name === rootName.value)) {
        rootName.value = list[0]?.name ?? "";
      }
      if (rootName.value === "") {
        applyListing([], "", false);
        return;
      }
      const listing = await listEntries(serverId.value, teamId(), {
        root: rootName.value,
        path: path.value,
      });
      if (gen !== generation) {
        return;
      }
      applyListing(
        listing.entries,
        listing.cursor ?? "",
        listing.truncated,
      );
    } catch (err) {
      if (gen !== generation) {
        return;
      }
      applyError(err);
    } finally {
      if (gen === generation) {
        loading.value = false;
      }
    }
  }

  /** loadEntries loads the current directory only (navigation/refresh). */
  async function loadEntries(): Promise<void> {
    if (!serverId.value || rootName.value === "") {
      entries.value = [];
      cursor.value = "";
      truncated.value = false;
      return;
    }
    const gen = ++generation;
    loading.value = true;
    errorRaw.value = null;
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: rootName.value,
        path: path.value,
      });
      if (gen !== generation) {
        return;
      }
      applyListing(listing.entries, listing.cursor ?? "", listing.truncated);
    } catch (err) {
      if (gen !== generation) {
        return;
      }
      applyError(err);
      // A failed navigation has no data for the new path; a failed refresh
      // keeps the entries already on screen.
      if (path.value !== lastLoadedPath) {
        entries.value = [];
        cursor.value = "";
        truncated.value = false;
      }
    } finally {
      if (gen === generation) {
        loading.value = false;
      }
    }
  }

  /**
   * init binds the store to one server. Switching servers resets the browser;
   * a repeat call for the same server is a no-op once it has loaded.
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

  /** refresh reloads the current directory. */
  async function refresh(): Promise<void> {
    await loadEntries();
  }

  /** retry re-runs the first load after a failure. */
  async function retry(): Promise<void> {
    if (roots.value.length === 0) {
      await loadAll();
      return;
    }
    await loadEntries();
  }

  /** loadMore appends the next cursor page. */
  async function loadMore(): Promise<void> {
    if (
      !serverId.value ||
      rootName.value === "" ||
      cursor.value === "" ||
      loadingMore.value
    ) {
      return;
    }
    const gen = generation;
    loadingMore.value = true;
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: rootName.value,
        path: path.value,
        cursor: cursor.value,
      });
      if (gen !== generation) {
        return;
      }
      entries.value = [...entries.value, ...listing.entries];
      cursor.value = listing.cursor ?? "";
      truncated.value = listing.truncated;
    } catch (err) {
      if (gen !== generation) {
        return;
      }
      applyError(err);
    } finally {
      loadingMore.value = false;
    }
  }

  /** setRoot switches the browsable root and returns to its top level. */
  async function setRoot(name: string): Promise<void> {
    if (name === rootName.value) {
      return;
    }
    rootName.value = name;
    path.value = "";
    clearSelection();
    await loadEntries();
  }

  /** openDirectory descends into one directory. */
  async function openDirectory(name: string): Promise<void> {
    path.value = joinPath(path.value, name);
    clearSelection();
    await loadEntries();
  }

  /** goToPath navigates to an absolute (root-relative) path. */
  async function goToPath(next: string): Promise<void> {
    if (next === path.value) {
      return;
    }
    path.value = next;
    clearSelection();
    await loadEntries();
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

  /** setViewMode switches between the list and grid layouts. */
  function setViewMode(mode: FilesViewMode): void {
    viewMode.value = mode;
  }

  /** setQuery updates the client-side folder filter. */
  function setQuery(next: string): void {
    query.value = next;
  }

  /** isSelected reports whether one entry name is selected. */
  function isSelected(name: string): boolean {
    return selected.value.has(name);
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

  /** resetBrowser clears the listing but keeps the bound server id. */
  function resetBrowser(): void {
    generation += 1;
    roots.value = [];
    rootName.value = "";
    path.value = "";
    entries.value = [];
    cursor.value = "";
    truncated.value = false;
    loading.value = false;
    loadingMore.value = false;
    loaded.value = false;
    disabled.value = false;
    errorRaw.value = null;
    viewMode.value = "list";
    sortKey.value = "name";
    sortDir.value = "asc";
    query.value = "";
    selected.value = new Set();
    lastLoadedPath = "";
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
    rootName,
    path,
    entries,
    cursor,
    truncated,
    loading,
    loadingMore,
    loaded,
    disabled,
    viewMode,
    sortKey,
    sortDir,
    query,
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
    // actions
    init,
    refresh,
    retry,
    loadMore,
    setRoot,
    openDirectory,
    goToPath,
    goUp,
    setSort,
    setViewMode,
    setQuery,
    isSelected,
    toggleSelection,
    toggleSelectAll,
    clearSelection,
    reset,
  };
});
