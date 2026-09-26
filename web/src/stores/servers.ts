import { defineStore } from "pinia";
import { ref } from "vue";

import {
  createServer,
  deleteServer,
  describeServerError,
  listServers,
  validateServer,
} from "../api/servers";
import type { CreateServerInput, Server, ValidateOutcome } from "../api/servers";

/** Polling cadence for the server list, in milliseconds. */
const pollIntervalMs = 5_000;

export const useServersStore = defineStore("servers", () => {
  const servers = ref<Server[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  // Interval handle kept outside reactive state; the store instance is a
  // singleton so a single handle is enough for the whole app.
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  /** applyServer merges one server into the in-memory list in place. */
  function applyServer(updated: Server): void {
    const index = servers.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      servers.value = [updated, ...servers.value];
      return;
    }
    servers.value[index] = updated;
  }

  /** fetchServers loads the list, toggling the loading flag. */
  async function fetchServers(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      servers.value = await listServers();
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
      servers.value = await listServers();
      error.value = null;
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
    await refreshServers();
    return created;
  }

  /** removeServer deletes a server and drops it from the list. */
  async function removeServer(id: string): Promise<void> {
    await deleteServer(id);
    servers.value = servers.value.filter((item) => item.id !== id);
  }

  /** validate runs the probes, merging the updated server into the list. */
  async function validate(id: string): Promise<ValidateOutcome> {
    const outcome = await validateServer(id);
    if (outcome.server) {
      applyServer(outcome.server);
    }
    return outcome;
  }

  return {
    servers,
    loading,
    error,
    fetchServers,
    pollServers,
    stopPolling,
    addServer,
    removeServer,
    validate,
  };
});
