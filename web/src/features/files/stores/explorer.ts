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

export class ExplorerDirtyError extends Error {
  readonly names: string[];

  constructor(names: string[]) {
    super("dirty editor tabs block this change");
    this.name = "ExplorerDirtyError";
    this.names = names;
  }
}

function basename(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? path : path.slice(slash + 1);
}

function isUnder(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`);
}

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
  const dirs = ref<Record<string, ExplorerDirState>>({});
  const expanded = ref<string[]>([]);
  const selectedPath = ref("");

  const searchQuery = ref("");
  const searchRecursive = ref(false);
  const searchHits = ref<FilesSearchHit[]>([]);
  const searchTruncated = ref(false);
  const searchLoading = ref(false);
  const searchRaw = ref<unknown>(null);

  let session = 0;
  let loadSeq = 0;
  const loadTokens = new Map<string, number>();
  const inFlight = new Map<string, Promise<void>>();
  let searchController: AbortController | null = null;
  let searchGeneration = 0;

  const searchActive = computed<boolean>(() => searchQuery.value.trim() !== "");
  const searchError = computed<string | null>(() =>
    searchRaw.value === null ? null : describeFilesError(searchRaw.value),
  );
  const rootDir = computed<ExplorerDirState | undefined>(() => dirs.value[rootPath.value]);
  const rootLoading = computed<boolean>(() => rootDir.value?.loading ?? false);
  const rootTruncated = computed<boolean>(() => rootDir.value?.truncated ?? false);

  const writable = computed<boolean>(
    () => files.roots.find((root) => root.name === rootName.value)?.writable ?? false,
  );
  const canWrite = computed<boolean>(() => files.canReadContent && writable.value);

  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  function isExpanded(path: string): boolean {
    return path === rootPath.value || expanded.value.includes(path);
  }

  function abortSearch(): void {
    searchController?.abort();
    searchController = null;
    searchGeneration += 1;
  }

  function clearSearchState(): void {
    abortSearch();
    searchHits.value = [];
    searchTruncated.value = false;
    searchLoading.value = false;
    searchRaw.value = null;
  }

  function clearCache(): void {
    session += 1;
    inFlight.clear();
    loadTokens.clear();
    dirs.value = {};
    expanded.value = [];
    selectedPath.value = "";
    searchQuery.value = "";
    clearSearchState();
  }

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

  async function load(path: string, force = false): Promise<void> {
    if (serverId.value === "") {
      return;
    }
    const current = dirs.value[path];
    if (!force) {
      if (current?.loaded) {
        return;
      }
      const pending = inFlight.get(path);
      if (pending) {
        return pending;
      }
    }
    const activeSession = session;
    loadSeq += 1;
    const token = loadSeq;
    loadTokens.set(path, token);
    dirs.value[path] = loadingDir(path, current?.entries ?? []);
    const task = (async () => {
      try {
        const listing = await listEntries(serverId.value, teamId(), {
          root: rootName.value,
          path,
        });
        if (loadTokens.get(path) !== token || activeSession !== session) {
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
        if (loadTokens.get(path) !== token || activeSession !== session) {
          return;
        }
        dirs.value[path] = {
          ...loadingDir(path, current?.entries ?? []),
          loading: false,
          error: err,
        };
      }
    })();
    inFlight.set(path, task);
    try {
      await task;
    } finally {
      if (inFlight.get(path) === task) {
        inFlight.delete(path);
      }
    }
  }

  async function init(): Promise<void> {
    await load(rootPath.value);
  }

  async function refresh(): Promise<void> {
    const paths = [rootPath.value, ...expanded.value.filter((path) => path !== rootPath.value)];
    await Promise.all(paths.map((path) => load(path, true)));
  }

  async function setRoot(name: string, path: string): Promise<void> {
    rootName.value = name;
    rootPath.value = path;
    clearCache();
    await load(path);
  }

  async function up(): Promise<void> {
    if (rootPath.value === "") {
      return;
    }
    await setRoot(rootName.value, parentPath(rootPath.value));
  }

  async function expand(path: string): Promise<void> {
    if (!isExpanded(path)) {
      expanded.value = [...expanded.value, path];
    }
    await load(path);
  }

  function collapse(path: string): void {
    if (path === rootPath.value) {
      return;
    }
    expanded.value = expanded.value.filter((item) => item !== path);
  }

  async function toggle(path: string): Promise<void> {
    if (isExpanded(path)) {
      collapse(path);
      return;
    }
    await expand(path);
  }

  function dropUnder(prefix: string, includeSelf: boolean): void {
    const inside = (key: string): boolean =>
      includeSelf ? isUnder(key, prefix) : key.startsWith(`${prefix}/`);
    const next: Record<string, ExplorerDirState> = {};
    for (const [key, value] of Object.entries(dirs.value)) {
      if (!inside(key)) {
        next[key] = value;
      }
    }
    dirs.value = next;
    expanded.value = expanded.value.filter((path) => !inside(path));
    for (const key of loadTokens.keys()) {
      if (inside(key)) {
        loadSeq += 1;
        loadTokens.set(key, loadSeq);
        inFlight.delete(key);
      }
    }
  }

  function select(path: string): void {
    selectedPath.value = path;
  }

  function entryAt(path: string) {
    return dirs.value[parentPath(path)]?.entries.find((entry) => entry.name === basename(path)) ?? null;
  }

  function assertNotDirty(path: string): void {
    const blocked = editor.blockingNames(rootName.value, parentPath(path), [basename(path)]);
    if (blocked.length > 0) {
      throw new ExplorerDirtyError(blocked);
    }
  }

  function blockingNames(paths: string[]): string[] {
    return paths.flatMap((path) =>
      editor.blockingNames(rootName.value, parentPath(path), [basename(path)]),
    );
  }

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
    clearSearch();
    await load(dir, true);
    selectedPath.value = target;
    return target;
  }

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
    clearSearch();
    await load(dir, true);
    return target;
  }

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
    clearSearch();
    await load(dir, true);
  }

  function setSearchQuery(value: string): void {
    searchQuery.value = value;
    if (value.trim() === "") {
      clearSearchState();
    }
  }

  function setSearchRecursive(value: boolean): void {
    searchRecursive.value = value;
  }

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

  function clearSearch(): void {
    searchQuery.value = "";
    clearSearchState();
  }

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
    searchActive,
    searchError,
    rootDir,
    rootLoading,
    rootTruncated,
    canWrite,
    writable,
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
