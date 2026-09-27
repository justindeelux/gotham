import { defineStore } from "pinia";
import { ref } from "vue";

import {
  describeApplicationError,
  isActiveDeployment,
  listDeployments,
  rollbackDeployment,
  triggerDeploy,
} from "../api/applications";
import type { Deployment } from "../api/applications";

/** Polling cadence for an in-flight deployment, in milliseconds. */
const activePollIntervalMs = 3_000;

export const useApplicationsStore = defineStore("applications", () => {
  const deploymentsByApp = ref<Record<string, Deployment[]>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);
  const acting = ref(false);

  // One poll timer per application with an in-flight deployment; the store
  // instance is a singleton so a single map is enough for the whole app.
  const pollTimers = new Map<string, ReturnType<typeof setInterval>>();

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

  return {
    deploymentsByApp,
    loading,
    error,
    acting,
    deploymentsOf,
    activeDeployment,
    latestDeployment,
    fetchDeployments,
    refreshDeployments,
    stopPolling,
    stopAllPolling,
    deploy,
    rollback,
  };
});
