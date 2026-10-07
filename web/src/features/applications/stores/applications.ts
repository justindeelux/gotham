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
  updateApplication,
} from "@/features/applications/api/applications";
import type {
  Application,
  Deployment,
  EnvVar,
  StorageMapping,
  UpdateApplicationInput,
} from "@/features/applications/api/applications";
import { onLocaleChange } from "@/shared/i18n/locale";
import { desiredPollIntervalMs } from "@/shared/utils/polling";

/** One deployment-history poll timer per application. */
interface PollEntry {
  handle: ReturnType<typeof setInterval>;
  active: boolean;
}

export const useApplicationsStore = defineStore("applications", () => {
  const applicationsById = ref<Record<string, Application>>({});
  const deploymentsByApp = ref<Record<string, Deployment[]>>({});
  const envByApp = ref<Record<string, EnvVar[]>>({});
  const storagesByApp = ref<Record<string, StorageMapping[]>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);
  /**
   * errorRaw keeps the failure `error` was derived from, so a language switch
   * re-derives the curated summary in the new locale without refetching. Raw
   * server diagnostics inside still pass through untouched.
   */
  const errorRaw = ref<unknown>(null);
  const acting = ref(false);
  const savingEnv = ref(false);
  const savingStorages = ref(false);

  // One poll timer per application; the store instance is a singleton so a
  // single map is enough for the whole app. `pollEpoch` invalidates in-flight
  // refreshes when polling is torn down, so a late response cannot restart a
  // timer after the component unmounted.
  const pollTimers = new Map<string, PollEntry>();
  let pollEpoch = 0;

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
    errorRaw.value = null;
    try {
      const application = await getApplication(appId);
      applicationsById.value[appId] = application;
      return application;
    } catch (err) {
      errorRaw.value = err;
      error.value = describeApplicationError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /**
   * update applies a partial update (rename, move environment, change node)
   * and merges the returned row into the cache.
   */
  async function update(
    appId: string,
    input: UpdateApplicationInput,
  ): Promise<Application> {
    acting.value = true;
    try {
      const application = await updateApplication(appId, input);
      applicationsById.value[appId] = application;
      return application;
    } finally {
      acting.value = false;
    }
  }

  /** fetchDeployments loads the deployment history of one application. */
  async function fetchDeployments(appId: string): Promise<void> {
    const epoch = pollEpoch;
    loading.value = true;
    error.value = null;
    errorRaw.value = null;
    try {
      const deployments = await listDeployments(appId);
      if (epoch !== pollEpoch) {
        return; // the page was left while this initial load was in flight
      }
      deploymentsByApp.value[appId] = deployments;
      settlePolling(appId);
    } catch (err) {
      if (epoch !== pollEpoch) {
        return;
      }
      errorRaw.value = err;
      error.value = describeApplicationError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /**
   * refreshDeployments reloads the history without toggling loading. A
   * response that resolves after polling was torn down (an application switch
   * or unmount) is discarded: applying it would restart a timer the owner
   * already cancelled.
   */
  async function refreshDeployments(appId: string): Promise<void> {
    const epoch = pollEpoch;
    try {
      const deployments = await listDeployments(appId);
      if (epoch !== pollEpoch) {
        return;
      }
      deploymentsByApp.value[appId] = deployments;
      error.value = null;
      errorRaw.value = null;
      settlePolling(appId);
    } catch (err) {
      if (epoch !== pollEpoch) {
        return;
      }
      errorRaw.value = err;
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
   * settlePolling ensures the application has a history poll at the cadence
   * its state calls for: fast while a deployment is in flight, slow when idle
   * so a deployment triggered elsewhere (a provider webhook, another session)
   * is still discovered. The cadence is re-evaluated on every fetch, so an
   * active deployment switches the timer to fast and a finished one back to
   * idle.
   */
  function settlePolling(appId: string): void {
    const active = activeDeployment(appId) !== null;
    const entry = pollTimers.get(appId) ?? null;
    if (entry !== null && entry.active === active) {
      return;
    }
    if (entry !== null) {
      clearInterval(entry.handle);
    }
    pollTimers.set(appId, {
      active,
      handle: setInterval(() => {
        void refreshDeployments(appId);
      }, desiredPollIntervalMs(active)),
    });
  }

  /** stopPolling clears the timer of one application; safe when idle. */
  function stopPolling(appId: string): void {
    const entry = pollTimers.get(appId) ?? null;
    if (entry !== null) {
      clearInterval(entry.handle);
      pollTimers.delete(appId);
    }
  }

  /**
   * stopAllPolling clears every deployment timer and invalidates in-flight
   * refreshes, so a late response cannot restart polling after teardown.
   */
  function stopAllPolling(): void {
    pollEpoch += 1;
    for (const entry of pollTimers.values()) {
      clearInterval(entry.handle);
    }
    pollTimers.clear();
  }

  // A retained failure banner re-derives its curated summary when the
  // language changes; the draft/route/polling state is untouched, and raw
  // diagnostics inside re-resolve to the same passthrough text.
  onLocaleChange(() => {
    if (errorRaw.value !== null) {
      error.value = describeApplicationError(errorRaw.value);
    }
  });

  /**
   * reset drops every cached collection and stops polling, so the next
   * sign-in never sees the previous account's applications. Called on
   * sign-out (see the auth store).
   */
  function reset(): void {
    stopAllPolling();
    applicationsById.value = {};
    deploymentsByApp.value = {};
    envByApp.value = {};
    storagesByApp.value = {};
    loading.value = false;
    error.value = null;
    errorRaw.value = null;
    acting.value = false;
    savingEnv.value = false;
    savingStorages.value = false;
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
    update,
    fetchDeployments,
    refreshDeployments,
    fetchEnv,
    saveEnv,
    fetchStorages,
    saveStorages,
    stopPolling,
    stopAllPolling,
    reset,
    deploy,
    rollback,
    stopApp,
    startApp,
  };
});
