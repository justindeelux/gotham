import { defineStore } from "pinia";
import { ref } from "vue";

import {
  describeApplicationError,
  getApplication,
  getEnv,
  getStorages,
  isActiveDeployment,
  listDeployments,
  replaceEnv,
  replaceStorages,
  rollbackDeployment,
  startApplication,
  stopApplication,
  triggerDeploy,
} from "../api/applications";
import type {
  Application,
  Deployment,
  EnvVar,
  StorageMapping,
} from "../api/applications";

/** Polling cadence for an in-flight deployment, in milliseconds. */
const activePollIntervalMs = 3_000;

export const useApplicationsStore = defineStore("applications", () => {
  const applicationsById = ref<Record<string, Application>>({});
  const deploymentsByApp = ref<Record<string, Deployment[]>>({});
  const envByApp = ref<Record<string, EnvVar[]>>({});
  const storagesByApp = ref<Record<string, StorageMapping[]>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);
  const acting = ref(false);
  const savingEnv = ref(false);
  const savingStorages = ref(false);

  // One poll timer per application with an in-flight deployment; the store
  // instance is a singleton so a single map is enough for the whole app.
  const pollTimers = new Map<string, ReturnType<typeof setInterval>>();

  /** applicationOf returns the cached application, if one was fetched. */
  function applicationOf(appId: string): Application | null {
    return applicationsById.value[appId] ?? null;
  }

  /** deploymentsOf returns the cached deployments of one application. */
  function deploymentsOf(appId: string): Deployment[] {
    return deploymentsByApp.value[appId] ?? [];
  }

  /** activeDeployment returns the newest non-terminal deployment, if any. */
  function activeDeployment(appId: string): Deployment | null {
    return deploymentsOf(appId).find((item) => isActiveDeployment(item)) ?? null;
  }

  /** latestDeployment returns the newest deployment, if any. */
  function latestDeployment(appId: string): Deployment | null {
    return deploymentsOf(appId)[0] ?? null;
  }

  /** envOf returns the cached environment rows of one application. */
  function envOf(appId: string): EnvVar[] {
    return envByApp.value[appId] ?? [];
  }

  /** storagesOf returns the cached storage mappings of one application. */
  function storagesOf(appId: string): StorageMapping[] {
    return storagesByApp.value[appId] ?? [];
  }

  /** fetchApplication loads one application by id. */
  async function fetchApplication(appId: string): Promise<Application> {
    loading.value = true;
    error.value = null;
    try {
      const application = await getApplication(appId);
      applicationsById.value[appId] = application;
      return application;
    } catch (err) {
      error.value = describeApplicationError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** fetchDeployments loads the deployment history of one application. */
  async function fetchDeployments(appId: string): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      deploymentsByApp.value[appId] = await listDeployments(appId);
      settlePolling(appId);
    } catch (err) {
      error.value = describeApplicationError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** refreshDeployments reloads the history without toggling loading. */
  async function refreshDeployments(appId: string): Promise<void> {
    try {
      deploymentsByApp.value[appId] = await listDeployments(appId);
      error.value = null;
      settlePolling(appId);
    } catch (err) {
      error.value = describeApplicationError(err);
    }
  }

  /** fetchEnv loads the environment collection of one application. */
  async function fetchEnv(appId: string): Promise<EnvVar[]> {
    const env = await getEnv(appId);
    envByApp.value[appId] = env;
    return env;
  }

  /** saveEnv replaces the environment collection and refreshes the cache. */
  async function saveEnv(appId: string, env: EnvVar[]): Promise<EnvVar[]> {
    savingEnv.value = true;
    try {
      const saved = await replaceEnv(appId, env);
      envByApp.value[appId] = saved;
      return saved;
    } finally {
      savingEnv.value = false;
    }
  }

  /** fetchStorages loads the storage collection of one application. */
  async function fetchStorages(appId: string): Promise<StorageMapping[]> {
    const storages = await getStorages(appId);
    storagesByApp.value[appId] = storages;
    return storages;
  }

  /** saveStorages replaces the storage collection and refreshes the cache. */
  async function saveStorages(
    appId: string,
    storages: StorageMapping[],
  ): Promise<StorageMapping[]> {
    savingStorages.value = true;
    try {
      const saved = await replaceStorages(appId, storages);
      storagesByApp.value[appId] = saved;
      return saved;
    } finally {
      savingStorages.value = false;
    }
  }

  /**
   * settlePolling starts the 3s poll while a deployment is in flight and
   * stops it once every deployment reached a terminal state.
   */
  function settlePolling(appId: string): void {
    const active = activeDeployment(appId) !== null;
    const timer = pollTimers.get(appId) ?? null;
    if (active && timer === null) {
      pollTimers.set(
        appId,
        setInterval(() => {
          void refreshDeployments(appId);
        }, activePollIntervalMs),
      );
    } else if (!active && timer !== null) {
      clearInterval(timer);
      pollTimers.delete(appId);
    }
  }

  /** stopPolling clears the timer of one application; safe when idle. */
  function stopPolling(appId: string): void {
    const timer = pollTimers.get(appId) ?? null;
    if (timer !== null) {
      clearInterval(timer);
      pollTimers.delete(appId);
    }
  }

  /** stopAllPolling clears every deployment timer; safe when idle. */
  function stopAllPolling(): void {
    for (const timer of pollTimers.values()) {
      clearInterval(timer);
    }
    pollTimers.clear();
  }

  /** deploy queues a deployment of the application's current revision. */
  async function deploy(appId: string): Promise<Deployment> {
    acting.value = true;
    try {
      const created = await triggerDeploy(appId);
      await refreshDeployments(appId);
      return created;
    } finally {
      acting.value = false;
    }
  }

  /** rollback queues a rollback, optionally to one named deployment. */
  async function rollback(
    appId: string,
    deploymentId?: string,
  ): Promise<Deployment> {
    acting.value = true;
    try {
      const created = await rollbackDeployment(
        appId,
        deploymentId ? { deployment_id: deploymentId } : {},
      );
      await refreshDeployments(appId);
      return created;
    } finally {
      acting.value = false;
    }
  }

  /**
   * stopApp stops the container of the newest deployment, then refreshes the
   * deployment history. The row is untouched: the deployment still describes
   * its release.
   */
  async function stopApp(appId: string): Promise<Deployment> {
    acting.value = true;
    try {
      const deployment = await stopApplication(appId);
      await refreshDeployments(appId);
      return deployment;
    } finally {
      acting.value = false;
    }
  }

  /** startApp restarts the container of the newest deployment. */
  async function startApp(appId: string): Promise<Deployment> {
    acting.value = true;
    try {
      const deployment = await startApplication(appId);
      await refreshDeployments(appId);
      return deployment;
    } finally {
      acting.value = false;
    }
  }

  return {
    applicationsById,
    deploymentsByApp,
    envByApp,
    storagesByApp,
    loading,
    error,
    acting,
    savingEnv,
    savingStorages,
    applicationOf,
    deploymentsOf,
    activeDeployment,
    latestDeployment,
    envOf,
    storagesOf,
    fetchApplication,
    fetchDeployments,
    refreshDeployments,
    fetchEnv,
    saveEnv,
    fetchStorages,
    saveStorages,
    stopPolling,
    stopAllPolling,
    deploy,
    rollback,
    stopApp,
    startApp,
  };
});
