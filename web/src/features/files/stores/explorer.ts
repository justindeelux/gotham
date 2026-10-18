import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createFile,
  createFolder,
  deleteEntry,
  describeFilesError,
  isCanceledError,
  listEntries,
  renameEntry,
  searchEntries,
} from "@/features/files/api/files";
import type { FilesSearchHit } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import type { ExplorerDirState } from "@/features/files/utils/explorer";
import {
  joinPath,
  normalizeEntryName,
  parentPath,
} from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

/**
 * Explorer tree store for the in-editor file explorer (JUS-223).
 *
 * One listing is cached per expanded directory and loaded on first expand, so
 * opening a deep tree never walks the whole filesystem up front. The store is
 * deliberately separate from the files browser: it owns its own root/path and
 * never moves the browser's active tab, while every write reuses the same
 * files API and the editor store's `applyRename`/`applyDelete`/`blockingNames`
 * rules so tabs and tree stay consistent.
 */

/** ExplorerDirtyError carries the names an unsaved buffer blocks. */
export class ExplorerDirtyError extends Error {
  /** names are the entries whose dirty tabs refuse the mutation. */
  readonly names: string[];

  constructor(names: string[]) {
    super("dirty editor tabs block this change");
    this.name = "ExplorerDirtyError";
    this.names = names;
  }
}

/** basename returns the last path segment of a root-relative path. */
function basename(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? path : path.slice(slash + 1);
}

/** isUnder reports whether `path` is `prefix` or a descendant of it. */
function isUnder(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`);
}

/** emptyDir builds a directory cache entry in its loading state. */
function loadingDir(path: string, entries: ExplorerDirState["entries"] = []): ExplorerDirState {
  return {
    path,
    entries,
    loaded: false,
    loading: true,
    truncated: false,
    cursor: "",
    error: null,
  };
}

export const useExplorerStore = defineStore("filesExplorer", () => {
  const files = useFilesStore();
  const editor = useEditorStore();

  const serverId = ref("");
  const rootName = ref("");
  const rootPath = ref("");
  /** dirs caches one listing per directory (keyed by root-relative path). */
  const dirs = ref<Record<string, ExplorerDirState>>({});
  /** expanded holds the nested folders the user opened; the root is implicit. */
  const expanded = ref<string[]>([]);
  const selectedPath = ref("");

  const searchQuery = ref("");
  const searchRecursive = ref(false);
  const searchHits = ref<FilesSearchHit[]>([]);
  const searchTruncated = ref(false);
  const searchLoading = ref(false);
  const searchRaw = ref<unknown>(null);

  // A load or search that finishes after a re-root must not write into the new
  // tree. `session` is bumped by every reset; in-flight work captures it.
  let session = 0;
  let searchController: AbortController | null = null;
  let searchGeneration = 0;

  const searchActive = computed<boolean>(() => searchQuery.value.trim() !== "");
  const searchError = computed<string | null>(() =>
    searchRaw.value === null ? null : describeFilesError(searchRaw.value),
  );
  const rootDir = computed<ExplorerDirState | undefined>(() => dirs.value[rootPath.value]);
  const rootLoading = computed<boolean>(() => rootDir.value?.loading ?? false);
  const rootTruncated = computed<boolean>(() => rootDir.value?.truncated ?? false);

  /**
   * writable reads the server root's flag, not the browser's current tab: the
   * explorer keeps its own root and may still be looking at a different one.
   */
  const writable = computed<boolean>(
    () => files.roots.find((root) => root.name === rootName.value)?.writable ?? false,
  );
  /** canWrite mirrors the browser gate: write role plus a writable root. */
  const canWrite = computed<boolean>(() => files.canReadContent && writable.value);

  /** teamId reads the active team every call is scoped to. */
  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** isExpanded reports whether a directory's children are on screen. */
  function isExpanded(path: string): boolean {
    return path === rootPath.value || expanded.value.includes(path);
  }

  /** abortSearch cancels the in-flight search and drops its result. */
  function abortSearch(): void {
    searchController?.abort();
    searchController = null;
    searchGeneration += 1;
  }

  /** clearSearchState empties the search results without touching the query. */
  function clearSearchState(): void {
    abortSearch();
    searchHits.value = [];
    searchTruncated.value = false;
    searchLoading.value = false;
    searchRaw.value = null;
  }

  /** clearCache drops every cached listing and expansion. */
  function clearCache(): void {
    session += 1;
    dirs.value = {};
    expanded.value = [];
    selectedPath.value = "";
    clearSearchState();
  }

  /**
   * bind points the explorer at one server/root/path. A mount on the same
   * address keeps the cached tree (the editor window closing and reopening must
   * not refetch); any other address starts from a clean slate.
   */
  function bind(nextServerId: string, nextRootName: string, nextRootPath: string): void {
    if (
      serverId.value === nextServerId &&
      rootName.value === nextRootName &&
      rootPath.value === nextRootPath &&
      dirs.value[nextRootPath] !== undefined
    ) {
      return;
    }
    serverId.value = nextServerId;
    rootName.value = nextRootName;
    rootPath.value = nextRootPath;
    clearCache();
  }

  /** load reads one directory, keeping any previous entries during a refresh. */
  async function load(path: string, force = false): Promise<void> {
    if (serverId.value === "") {
      return;
    }
    const current = dirs.value[path];
    if (current?.loading) {
      return;
    }
    if (current?.loaded && !force) {
      return;
    }
    const activeSession = session;
    dirs.value[path] = loadingDir(path, current?.entries ?? []);
    try {
      const listing = await listEntries(serverId.value, teamId(), {
        root: rootName.value,
        path,
      });
      if (activeSession !== session) {
        return;
      }
      dirs.value[path] = {
        path,
        entries: listing.entries,
        loaded: true,
        loading: false,
        truncated: listing.truncated,
        cursor: listing.cursor ?? "",
        error: null,
      };
    } catch (err) {
      if (activeSession !== session) {
        return;
      }
      dirs.value[path] = {
        ...loadingDir(path, current?.entries ?? []),
        loading: false,
        error: err,
      };
    }
  }

  /** init loads the current root on first mount. */
  async function init(): Promise<void> {
    await load(rootPath.value);
  }

  /** refresh reloads the root and every expanded folder, keeping the view. */
  async function refresh(): Promise<void> {
    const paths = [rootPath.value, ...expanded.value.filter((path) => path !== rootPath.value)];
    await Promise.all(paths.map((path) => load(path, true)));
  }

  /** setRoot re-roots the tree (up, or "set as root" on a folder). */
  async function setRoot(name: string, path: string): Promise<void> {
    rootName.value = name;
    rootPath.value = path;
    clearCache();
    await load(path);
  }

  /** up moves the tree root to its parent directory. */
  async function up(): Promise<void> {
    if (rootPath.value === "") {
      return;
    }
    await setRoot(rootName.value, parentPath(rootPath.value));
  }

  /** expand opens one directory and lazy-loads it on first use. */
  async function expand(path: string): Promise<void> {
    if (!isExpanded(path)) {
      expanded.value = [...expanded.value, path];
    }
    await load(path);
  }

  /** collapse folds one directory away; its cache survives for re-expansion. */
  function collapse(path: string): void {
    if (path === rootPath.value) {
      return;
    }
    expanded.value = expanded.value.filter((item) => item !== path);
  }

  /** toggle flips one directory's expansion. */
  async function toggle(path: string): Promise<void> {
    if (isExpanded(path)) {
      collapse(path);
      return;
    }
    await expand(path);
  }

  /**
   * dropUnder invalidates every cached listing at or below `prefix`. A renamed
   * or deleted folder's children can never be reused under the old paths.
   */
  function dropUnder(prefix: string, includeSelf: boolean): void {
    const next: Record<string, ExplorerDirState> = {};
    for (const [key, value] of Object.entries(dirs.value)) {
      const inside = includeSelf ? isUnder(key, prefix) : key.startsWith(`${prefix}/`);
      if (!inside) {
        next[key] = value;
      }
    }
    dirs.value = next;
    expanded.value = expanded.value.filter((path) =>
      includeSelf ? !isUnder(path, prefix) : !path.startsWith(`${prefix}/`),
    );
  }

  /** select records the user's focused row. */
  function select(path: string): void {
    selectedPath.value = path;
  }

  /** entryAt resolves one cached listing row by path. */
  function entryAt(path: string) {
    return dirs.value[parentPath(path)]?.entries.find((entry) => entry.name === basename(path)) ?? null;
  }

  /** assertNotDirty refuses a write that would drop unsaved tab edits. */
  function assertNotDirty(path: string): void {
    const blocked = editor.blockingNames(rootName.value, parentPath(path), [basename(path)]);
    if (blocked.length > 0) {
      throw new ExplorerDirtyError(blocked);
    }
  }

  /** blockingNames reports the unsaved entries among `paths`. */
  function blockingNames(paths: string[]): string[] {
    return paths.flatMap((path) =>
      editor.blockingNames(rootName.value, parentPath(path), [basename(path)]),
    );
  }

  /**
   * create makes an empty file or folder in `dir`, refreshes that listing and
   * selects the new row so it is visible and ready to rename.
   */
  async function create(kind: "file" | "directory", name: string, dir: string): Promise<string> {
    const clean = normalizeEntryName(name);
    const target = joinPath(dir, clean);
    if (kind === "directory") {
      await createFolder(serverId.value, teamId(), { root: rootName.value, path: target });
    } else {
      await createFile(serverId.value, teamId(), { root: rootName.value, path: target });
    }
    if (dir !== rootPath.value && !isExpanded(dir)) {
      expanded.value = [...expanded.value, dir];
    }
    await load(dir, true);
    selectedPath.value = target;
    return target;
  }

  /**
   * rename renames one entry and rebases every open tab under it through the
   * editor store. A dirty buffer anywhere inside the entry blocks the call
   * before the server changes anything.
   */
  async function rename(path: string, nextName: string): Promise<string> {
    const clean = normalizeEntryName(nextName);
    const dir = parentPath(path);
    const name = basename(path);
    const target = joinPath(dir, clean);
    assertNotDirty(path);
    await renameEntry(serverId.value, teamId(), {
      root: rootName.value,
      source: path,
      destination: target,
    });
    editor.applyRename(rootName.value, dir, name, clean);
    dropUnder(path, true);
    if (selectedPath.value !== "" && isUnder(selectedPath.value, path)) {
      selectedPath.value = target + selectedPath.value.slice(path.length);
    }
    await load(dir, true);
    return target;
  }

  /**
   * remove moves one entry to the recycle bin (recursively for a folder) and
   * closes every tab it owned. A dirty buffer blocks the call first. `kind`
   * lets a search hit in a folder that has never been expanded still delete
   * recursively.
   */
  async function remove(path: string, kind?: string): Promise<void> {
    const dir = parentPath(path);
    const name = basename(path);
    assertNotDirty(path);
    const isDirectory =
      kind === "directory" || entryAt(path)?.kind === "directory";
    await deleteEntry(serverId.value, teamId(), rootName.value, path, isDirectory);
    editor.applyDelete(rootName.value, dir, [name]);
    dropUnder(path, true);
    if (selectedPath.value !== "" && isUnder(selectedPath.value, path)) {
      selectedPath.value = "";
    }
    await load(dir, true);
  }

  /** setSearchQuery updates the search input. */
  function setSearchQuery(value: string): void {
    searchQuery.value = value;
    if (value.trim() === "") {
      clearSearchState();
    }
  }

  /** setSearchRecursive flips the "Include subdirectories" option. */
  function setSearchRecursive(value: boolean): void {
    searchRecursive.value = value;
  }

  /** runSearch searches names under the current root path. */
  async function runSearch(): Promise<void> {
    const query = searchQuery.value.trim();
    if (query === "" || serverId.value === "") {
      clearSearchState();
      return;
    }
    abortSearch();
    const generation = searchGeneration;
    const controller = new AbortController();
    searchController = controller;
    searchLoading.value = true;
    searchRaw.value = null;
    try {
      const listing = await searchEntries(serverId.value, teamId(), {
        root: rootName.value,
        path: rootPath.value,
        query,
        recursive: searchRecursive.value,
        signal: controller.signal,
      });
      if (generation !== searchGeneration) {
        return;
      }
      searchHits.value = listing.results;
      searchTruncated.value = listing.truncated;
    } catch (err) {
      if (generation === searchGeneration && !isCanceledError(err)) {
        searchHits.value = [];
        searchTruncated.value = false;
        searchRaw.value = err;
      }
    } finally {
      if (generation === searchGeneration) {
        searchLoading.value = false;
      }
    }
  }

  /** clearSearch empties the query and restores the tree. */
  function clearSearch(): void {
    searchQuery.value = "";
    clearSearchState();
  }

  /** reset unbinds the store (server switch / editor teardown). */
  function reset(): void {
    clearCache();
    serverId.value = "";
    rootName.value = "";
    rootPath.value = "";
    searchQuery.value = "";
    searchRecursive.value = false;
    session += 1;
  }

  return {
    // state
    serverId,
    rootName,
    rootPath,
    dirs,
    expanded,
    selectedPath,
    searchQuery,
    searchRecursive,
    searchHits,
    searchTruncated,
    searchLoading,
    searchRaw,
    // derived
    searchActive,
    searchError,
    rootDir,
    rootLoading,
    rootTruncated,
    canWrite,
    writable,
    // actions
    bind,
    init,
    load,
    refresh,
    setRoot,
    up,
    expand,
    collapse,
    toggle,
    isExpanded,
    select,
    entryAt,
    blockingNames,
    create,
    rename,
    remove,
    setSearchQuery,
    setSearchRecursive,
    runSearch,
    clearSearch,
    reset,
  };
});
