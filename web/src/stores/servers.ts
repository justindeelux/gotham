import { defineStore } from "pinia";
import { ref } from "vue";

import {
  createServer,
  deleteServer,
  describeServerError,
  listServers,
  updateServer,
  validateServer,
} from "../api/servers";
import type { CreateServerInput, Server, UpdateServerInput, ValidateOptions, ValidateOutcome } from "../api/servers";
import { createServerListSync } from "./serverListSync";

/** Polling cadence for the server list, in milliseconds. */
const pollIntervalMs = 5_000;

export const useServersStore = defineStore("servers", () => {
  const servers = ref<Server[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  // Interval handle kept outside reactive state; the store instance is a
  // singleton so a single handle is enough for the whole app.
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  // Ordering guard: a poll response older than a newer one, or predating a
  // local mutation, must not replace the list (A4-16/B4-5).
  const listSync = createServerListSync();

  /** applyServer merges one server into the in-memory list in place. */
  function applyServer(updated: Server): void {
    const index = servers.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      servers.value = [updated, ...servers.value];
      return;
    }
    servers.value[index] = updated;
  }

  /**
   * loadServerList fetches the list and applies it only when the ordering guard
   * admits the response. It returns the fetched list either way.
   */
  async function loadServerList(): Promise<Server[]> {
    const token = listSync.begin();
    const list = await listServers();
    if (listSync.admit(token)) {
      servers.value = list;
      error.value = null;
    }
    return list;
  }

  /** fetchServers loads the list, toggling the loading flag. */
  async function fetchServers(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      await loadServerList();
    } catch (err) {
      error.value = describeServerError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** refreshServers reloads the list without toggling the loading flag. */
  async function refreshServers(): Promise<void> {
    try {
      await loadServerList();
    } catch (err) {
      error.value = describeServerError(err);
    }
  }

  /**
   * pollServers starts a 5s interval that refreshes the list in place. Calling
   * it more than once is a no-op until {@link stopPolling} runs.
   */
  function pollServers(): void {
    if (pollTimer !== null) {
      return;
    }
    pollTimer = setInterval(() => {
      void refreshServers();
    }, pollIntervalMs);
  }

  /** stopPolling clears the interval; safe to call when not polling. */
  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  /** addServer creates a server and refreshes the list. */
  async function addServer(input: CreateServerInput): Promise<Server> {
    const created = await createServer(input);
    // Invalidate any poll that started before the create: its response would
    // not include the new row.
    listSync.markMutation();
    await refreshServers();
    return created;
  }

  /** removeServer deletes a server and drops it from the list. */
  async function removeServer(id: string): Promise<void> {
    await deleteServer(id);
    // Invalidate in-flight polls so a pre-delete response cannot resurrect the
    // deleted row.
    listSync.markMutation();
    servers.value = servers.value.filter((item) => item.id !== id);
  }

  /** validate runs the probes, merging the updated server into the list. */
  async function validate(id: string, passphrase?: string, options: ValidateOptions = {}): Promise<ValidateOutcome> {
    const outcome = await validateServer(id, passphrase, options);
    if (outcome.server) {
      // Invalidate in-flight polls so they cannot clobber the merge with older
      // state.
      listSync.markMutation();
      applyServer(outcome.server);
    }
    return outcome;
  }

  /** updateServer edits a server, merging the updated row into the list. */
  async function updateServerById(id: string, input: UpdateServerInput): Promise<Server> {
    const updated = await updateServer(id, input);
    listSync.markMutation();
    applyServer(updated);
    return updated;
  }

  /**
   * reset drops the cached list and stops polling, so the next sign-in never
   * sees the previous account's nodes. Called on sign-out (see the auth
   * store).
   */
  function reset(): void {
    stopPolling();
    servers.value = [];
    loading.value = false;
    error.value = null;
  }

  return {
    servers,
    loading,
    error,
    fetchServers,
    pollServers,
    stopPolling,
    reset,
    addServer,
    removeServer,
    validate,
    updateServer: updateServerById,
  };
});
