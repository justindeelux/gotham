import { defineStore } from "pinia";
import { ref } from "vue";

import {
  createDatabase,
  deleteDatabase,
  describeDatabaseError,
  getDatabase,
  getDatabaseCredentials,
  listDatabases,
  renameDatabase,
  restartDatabase,
  startDatabase,
  stopDatabase,
} from "../api/databases";
import type {
  CreateDatabaseInput,
  Database,
  DatabaseCredentials,
} from "../api/databases";

/** Polling cadence for the database list, in milliseconds. */
const pollIntervalMs = 5_000;

export const useDatabasesStore = defineStore("databases", () => {
  const databases = ref<Database[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);
  const acting = ref(false);
  const credentialsById = ref<Record<string, DatabaseCredentials>>({});
  const credentialsLoading = ref(false);
  const credentialsError = ref<string | null>(null);

  // Interval handle kept outside reactive state; the store instance is a
  // singleton so a single handle is enough for the whole app.
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  // Bumped by every local list mutation (create/delete). A list response that
  // started before the mutation must merge instead of replacing the array, or
  // it would clobber the row this tab just created.
  let mutationGeneration = 0;

  /**
   * mergeById folds a server list into the local one: a server row wins for a
   * known id, and a local-only row (a just-created database the list has not
   * caught up with) is preserved.
   */
  function mergeById(server: Database[], local: Database[]): Database[] {
    const byId = new Map(server.map((item) => [item.id, item]));
    for (const item of local) {
      if (!byId.has(item.id)) {
        byId.set(item.id, item);
      }
    }
    return [...byId.values()];
  }

  /** applyDatabase merges one database into the in-memory list in place. */
  function applyDatabase(updated: Database): void {
    const index = databases.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      databases.value = [updated, ...databases.value];
      return;
    }
    databases.value[index] = updated;
  }

  /** fetchDatabases loads the list, toggling the loading flag. */
  async function fetchDatabases(): Promise<void> {
    loading.value = true;
    error.value = null;
    const generation = mutationGeneration;
    try {
      const server = await listDatabases();
      databases.value =
        generation === mutationGeneration
          ? server
          : mergeById(server, databases.value);
    } catch (err) {
      error.value = describeDatabaseError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** refreshDatabases reloads the list without toggling the loading flag. */
  async function refreshDatabases(): Promise<void> {
    const generation = mutationGeneration;
    try {
      const server = await listDatabases();
      databases.value =
        generation === mutationGeneration
          ? server
          : mergeById(server, databases.value);
      error.value = null;
    } catch (err) {
      error.value = describeDatabaseError(err);
    }
  }

  /**
   * pollDatabases starts a 5s interval that refreshes the list in place.
   * Calling it more than once is a no-op until {@link stopPolling} runs.
   */
  function pollDatabases(): void {
    if (pollTimer !== null) {
      return;
    }
    pollTimer = setInterval(() => {
      void refreshDatabases();
    }, pollIntervalMs);
  }

  /** stopPolling clears the interval; safe to call when not polling. */
  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  /**
   * fetchDatabase loads one database and merges it into the list, so the
   * detail page stays consistent with the list after lifecycle actions.
   */
  async function fetchDatabase(id: string): Promise<Database> {
    const database = await getDatabase(id);
    applyDatabase(database);
    return database;
  }

  /**
   * provision creates a database and merges the row into the list, keeping
   * the generated credentials for the success panel. The caller owns the
   * created row, never the whole list reload.
   */
  async function provision(
    input: CreateDatabaseInput,
  ): Promise<{ database: Database; credentials: DatabaseCredentials }> {
    acting.value = true;
    try {
      const created = await createDatabase(input);
      applyDatabase(created.database);
      credentialsById.value[created.database.id] = created.credentials;
      mutationGeneration += 1;
      return created;
    } finally {
      acting.value = false;
    }
  }

  /** rename changes the display name of one database. */
  async function rename(id: string, name: string): Promise<Database> {
    acting.value = true;
    try {
      const updated = await renameDatabase(id, { name });
      applyDatabase(updated);
      return updated;
    } finally {
      acting.value = false;
    }
  }

  /** remove deletes a database and drops it from the list. */
  async function remove(id: string): Promise<void> {
    acting.value = true;
    try {
      await deleteDatabase(id);
      databases.value = databases.value.filter((item) => item.id !== id);
      delete credentialsById.value[id];
      mutationGeneration += 1;
    } finally {
      acting.value = false;
    }
  }

  /** fetchCredentials loads the owner-only credentials of one database. */
  async function fetchCredentials(id: string): Promise<DatabaseCredentials> {
    credentialsLoading.value = true;
    credentialsError.value = null;
    try {
      const credentials = await getDatabaseCredentials(id);
      credentialsById.value[id] = credentials;
      return credentials;
    } catch (err) {
      credentialsError.value = describeDatabaseError(err);
      throw err;
    } finally {
      credentialsLoading.value = false;
    }
  }

  /** credentialsOf returns the cached credentials of one database, if any. */
  function credentialsOf(id: string): DatabaseCredentials | null {
    return credentialsById.value[id] ?? null;
  }

  /** start powers the container back on and merges the updated row. */
  async function start(id: string): Promise<Database> {
    acting.value = true;
    try {
      const updated = await startDatabase(id);
      applyDatabase(updated);
      return updated;
    } finally {
      acting.value = false;
    }
  }

  /** stop shuts the container down and merges the updated row. */
  async function stop(id: string): Promise<Database> {
    acting.value = true;
    try {
      const updated = await stopDatabase(id);
      applyDatabase(updated);
      return updated;
    } finally {
      acting.value = false;
    }
  }

  /** restart re-runs the container in place and merges the updated row. */
  async function restart(id: string): Promise<Database> {
    acting.value = true;
    try {
      const updated = await restartDatabase(id);
      applyDatabase(updated);
      return updated;
    } finally {
      acting.value = false;
    }
  }

  return {
    databases,
    loading,
    error,
    acting,
    credentialsById,
    credentialsLoading,
    credentialsError,
    fetchDatabases,
    refreshDatabases,
    pollDatabases,
    stopPolling,
    fetchDatabase,
    provision,
    rename,
    remove,
    fetchCredentials,
    credentialsOf,
    start,
    stop,
    restart,
  };
});
