import { defineStore } from "pinia";
import { ref } from "vue";

import {
  deleteApplication,
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
  const errorRaw = ref<unknown>(null);
  const acting = ref(false);
  const savingEnv = ref(false);
  const savingStorages = ref(false);

  const pollTimers = new Map<string, PollEntry>();
  let pollEpoch = 0;

  function applicationOf(appId: string): Application | null {
    return applicationsById.value[appId] ?? null;
  }

  function deploymentsOf(appId: string): Deployment[] {
    return deploymentsByApp.value[appId] ?? [];
  }

  function activeDeployment(appId: string): Deployment | null {
    return deploymentsOf(appId).find((item) => isActiveDeployment(item)) ?? null;
  }

  function latestDeployment(appId: string): Deployment | null {
    return deploymentsOf(appId)[0] ?? null;
  }

  function envOf(appId: string): EnvVar[] {
    return envByApp.value[appId] ?? [];
  }

  function storagesOf(appId: string): StorageMapping[] {
    return storagesByApp.value[appId] ?? [];
  }

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

  async function fetchDeployments(appId: string): Promise<void> {
    const epoch = pollEpoch;
    loading.value = true;
    error.value = null;
    errorRaw.value = null;
    try {
      const deployments = await listDeployments(appId);
      if (epoch !== pollEpoch) {
        return;
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

  async function fetchEnv(appId: string): Promise<EnvVar[]> {
    const env = await getEnv(appId);
    envByApp.value[appId] = env;
    return env;
  }

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

  async function fetchStorages(appId: string): Promise<StorageMapping[]> {
    const storages = await getStorages(appId);
    storagesByApp.value[appId] = storages;
    return storages;
  }

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

  function stopPolling(appId: string): void {
    const entry = pollTimers.get(appId) ?? null;
    if (entry !== null) {
      clearInterval(entry.handle);
      pollTimers.delete(appId);
    }
  }

  function stopAllPolling(): void {
    pollEpoch += 1;
    for (const entry of pollTimers.values()) {
      clearInterval(entry.handle);
    }
    pollTimers.clear();
  }

  onLocaleChange(() => {
    if (errorRaw.value !== null) {
      error.value = describeApplicationError(errorRaw.value);
    }
  });

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

  async function remove(appId: string): Promise<string[]> {
    acting.value = true;
    try {
      const warnings = await deleteApplication(appId);
      delete applicationsById.value[appId];
      delete deploymentsByApp.value[appId];
      delete envByApp.value[appId];
      delete storagesByApp.value[appId];
      stopPolling(appId);
      return warnings;
    } finally {
      acting.value = false;
    }
  }

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
    remove,
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
