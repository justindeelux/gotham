import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createServer,
  deleteServer,
  failureText,
  listServers,
  updateServer,
  validateServer,
} from "@/features/servers/api/servers";
import type { CreateServerInput, Server, UpdateServerInput, ValidateOptions, ValidateOutcome } from "@/features/servers/api/servers";
import { createServerListSync } from "./serverListSync";

const pollIntervalMs = 5_000;

export const useServersStore = defineStore("servers", () => {
  const servers = ref<Server[]>([]);
  const loading = ref(false);
  const failure = ref<unknown>(null);
  const error = computed<string | null>(() =>
    failure.value === null || failure.value === undefined
      ? null
      : failureText(failure.value),
  );

  let pollTimer: ReturnType<typeof setInterval> | null = null;

  const listSync = createServerListSync();

  function applyServer(updated: Server): void {
    const index = servers.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      servers.value = [updated, ...servers.value];
      return;
    }
    servers.value[index] = updated;
  }

  async function loadServerList(): Promise<Server[]> {
    const token = listSync.begin();
    const list = await listServers();
    if (listSync.admit(token)) {
      servers.value = list;
      failure.value = null;
    }
    return list;
  }

  async function fetchServers(): Promise<void> {
    loading.value = true;
    failure.value = null;
    try {
      await loadServerList();
    } catch (err) {
      failure.value = err;
      throw err;
    } finally {
      loading.value = false;
    }
  }

  async function refreshServers(): Promise<void> {
    try {
      await loadServerList();
    } catch (err) {
      failure.value = err;
    }
  }

  function pollServers(): void {
    if (pollTimer !== null) {
      return;
    }
    pollTimer = setInterval(() => {
      void refreshServers();
    }, pollIntervalMs);
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function addServer(input: CreateServerInput): Promise<Server> {
    const created = await createServer(input);
    listSync.markMutation();
    await refreshServers();
    return created;
  }

  async function removeServer(id: string): Promise<void> {
    await deleteServer(id);
    listSync.markMutation();
    servers.value = servers.value.filter((item) => item.id !== id);
  }

  async function validate(id: string, passphrase?: string, options: ValidateOptions = {}): Promise<ValidateOutcome> {
    const outcome = await validateServer(id, passphrase, options);
    if (outcome.server) {
      listSync.markMutation();
      applyServer(outcome.server);
    }
    return outcome;
  }

  async function updateServerById(id: string, input: UpdateServerInput): Promise<Server> {
    const updated = await updateServer(id, input);
    listSync.markMutation();
    applyServer(updated);
    return updated;
  }

  function reset(): void {
    stopPolling();
    listSync.markMutation();
    servers.value = [];
    loading.value = false;
    failure.value = null;
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
