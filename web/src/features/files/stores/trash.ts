import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  describeFilesError,
  emptyTrash,
  listTrash,
  purgeTrash,
  restoreTrash,
} from "@/features/files/api/files";
import type { TrashEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";

export const useTrashStore = defineStore("files-trash", () => {
  const entries = ref<TrashEntry[]>([]);
  const truncated = ref(false);
  const loading = ref(false);
  const acting = ref(false);
  const loaded = ref(false);
  const errorRaw = ref<unknown>(null);
  const loadedScope = ref("");

  let generation = 0;

  function browser() {
    return useFilesStore();
  }

  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  function currentScope(): string {
    const files = browser();
    if (files.serverId === "" || files.rootName === "") {
      return "";
    }
    return `${files.serverId}\u0000${files.rootName}`;
  }

  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeFilesError(errorRaw.value),
  );

  const scopeMatches = computed<boolean>(
    () => loadedScope.value !== "" && loadedScope.value === currentScope(),
  );

  async function runActing<T>(task: () => Promise<T>): Promise<T> {
    acting.value = true;
    try {
      return await task();
    } finally {
      acting.value = false;
    }
  }

  function clear(): void {
    entries.value = [];
    truncated.value = false;
    loaded.value = false;
    loadedScope.value = "";
    errorRaw.value = null;
  }

  async function load(): Promise<void> {
    const files = browser();
    const serverId = files.serverId;
    const root = files.rootName;
    if (serverId === "" || root === "") {
      clear();
      return;
    }
    const scope = `${serverId}\u0000${root}`;
    if (scope !== loadedScope.value) {
      entries.value = [];
      truncated.value = false;
      loaded.value = false;
    }
    const gen = ++generation;
    loading.value = true;
    errorRaw.value = null;
    try {
      const listing = await listTrash(serverId, teamId(), root);
      if (gen !== generation) {
        return;
      }
      entries.value = listing.entries;
      truncated.value = listing.truncated;
      loaded.value = true;
      loadedScope.value = scope;
    } catch (err) {
      if (gen !== generation) {
        return;
      }
      entries.value = [];
      truncated.value = false;
      loaded.value = false;
      loadedScope.value = "";
      errorRaw.value = err;
    } finally {
      if (gen === generation) {
        loading.value = false;
      }
    }
  }

  async function restore(entry: TrashEntry, overwrite = false): Promise<void> {
    if (!scopeMatches.value) {
      return;
    }
    const files = browser();
    await runActing(async () => {
      await restoreTrash(files.serverId, teamId(), {
        root: files.rootName,
        id: entry.id,
        overwrite,
      });
      await load();
      await files.refresh();
    });
  }

  async function purge(entry: TrashEntry): Promise<void> {
    if (!scopeMatches.value) {
      return;
    }
    const files = browser();
    await runActing(async () => {
      await purgeTrash(files.serverId, teamId(), files.rootName, entry.id);
      await load();
    });
  }

  async function empty(): Promise<number> {
    if (!scopeMatches.value) {
      return 0;
    }
    const files = browser();
    return runActing(async () => {
      const removed = await emptyTrash(files.serverId, teamId(), files.rootName);
      await load();
      return removed;
    });
  }

  function reset(): void {
    generation += 1;
    loading.value = false;
    acting.value = false;
    clear();
  }

  return {
    entries,
    truncated,
    loading,
    acting,
    loaded,
    error,
    scopeMatches,
    load,
    restore,
    purge,
    empty,
    reset,
  };
});
