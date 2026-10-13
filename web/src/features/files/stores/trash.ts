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

/**
 * Recycle-bin store (JUS-122). Kept in its own module so the file-browser tabs
 * work (package C) can evolve {@link useFilesStore} without a merge collision:
 * this store only reads the browser's bound server/root and asks it to refresh
 * after a restore. The bin is per-root, so every call is scoped to the browser's
 * current root; listing is open to any member, while restore/purge/empty are
 * owner/admin (the control plane refuses otherwise).
 *
 * Scope safety: the loaded listing is tagged with the (server, root) it came
 * from. Switching node, root or team invalidates it immediately (the store is
 * reset on those switches), a failed load never keeps the previous bin, and
 * {@link scopeMatches} gates every mutation so an id from another node/root can
 * never act on the current one.
 */
export const useTrashStore = defineStore("files-trash", () => {
  const entries = ref<TrashEntry[]>([]);
  const truncated = ref(false);
  const loading = ref(false);
  const acting = ref(false);
  const loaded = ref(false);
  const errorRaw = ref<unknown>(null);
  /** loadedScope is the "serverId\0root" the current entries belong to. */
  const loadedScope = ref("");

  // Bumped by every load so a slow listing can never overwrite a newer one.
  let generation = 0;

  /** browser reads the files store's bound server and current root. */
  function browser() {
    return useFilesStore();
  }

  /** teamId reads the active team the requests are scoped to. */
  function teamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** currentScope names the browser's bound server and root ("" when unbound). */
  function currentScope(): string {
    const files = browser();
    if (files.serverId === "" || files.rootName === "") {
      return "";
    }
    return `${files.serverId}\u0000${files.rootName}`;
  }

  /** error renders the retained load failure in the active locale. */
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeFilesError(errorRaw.value),
  );

  /**
   * scopeMatches is true only when the loaded entries belong to the browser's
   * current server+root. The dialog gates restore/purge/empty on it, so a
   * listing left over from a previous node/root is never actionable.
   */
  const scopeMatches = computed<boolean>(
    () => loadedScope.value !== "" && loadedScope.value === currentScope(),
  );

  /** runActing wraps one bin mutation in the acting flag. */
  async function runActing<T>(task: () => Promise<T>): Promise<T> {
    acting.value = true;
    try {
      return await task();
    } finally {
      acting.value = false;
    }
  }

  /** clear drops the listing and its scope tag. */
  function clear(): void {
    entries.value = [];
    truncated.value = false;
    loaded.value = false;
    loadedScope.value = "";
    errorRaw.value = null;
  }

  /**
   * load lists the current root's bin. A different server/root invalidates the
   * previous listing before the request starts, and a failure clears it too, so
   * the dialog never shows another node's bin — in flight or after an error.
   */
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
      // Never leave a stale bin on screen: a failed load shows the error state.
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

  /**
   * restore returns one bin entry to its recorded path and refreshes both the
   * bin and the browser. It rethrows a 409 (an occupied destination) so the
   * caller can explain the conflict; the store never guesses an overwrite. A
   * listing that does not belong to the current node/root is refused.
   */
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
      // The restored entry may land in the directory on screen.
      await files.refresh();
    });
  }

  /** purge permanently deletes one bin entry and reloads the bin. */
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

  /** empty permanently deletes every bin entry and returns how many went. */
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

  /** reset clears the bin (node/root/team switch, and sign-out). */
  function reset(): void {
    generation += 1;
    loading.value = false;
    acting.value = false;
    clear();
  }

  return {
    // state
    entries,
    truncated,
    loading,
    acting,
    loaded,
    // derived
    error,
    scopeMatches,
    // actions
    load,
    restore,
    purge,
    empty,
    reset,
  };
});
