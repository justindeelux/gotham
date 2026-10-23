import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  describeApplicationError,
  listRecentDeployments,
} from "@/features/applications";
import type { RecentDeployment } from "@/features/applications";

export const recentDeploymentsLimit = 10;

const refreshIntervalMs = 30_000;

export const useRecentDeploymentsStore = defineStore("dashboard-deployments", () => {
  const deployments = ref<RecentDeployment[]>([]);
  const loading = ref(false);
  const failure = ref<unknown>(null);
  const error = computed<string | null>(() =>
    failure.value === null || failure.value === undefined
      ? null
      : describeApplicationError(failure.value),
  );

  let refreshTimer: ReturnType<typeof setInterval> | null = null;
  let inflight: Promise<void> | null = null;

  function loadFeed(): Promise<void> {
    if (inflight !== null) {
      return inflight;
    }
    inflight = (async () => {
      try {
        deployments.value = await listRecentDeployments(recentDeploymentsLimit);
        failure.value = null;
      } catch (err) {
        deployments.value = [];
        failure.value = err;
      } finally {
        inflight = null;
      }
    })();
    return inflight;
  }

  async function fetchRecentDeployments(): Promise<void> {
    loading.value = true;
    try {
      await loadFeed();
    } finally {
      loading.value = false;
    }
  }

  function refreshRecentDeployments(): void {
    void loadFeed();
  }

  function onFocus(): void {
    refreshRecentDeployments();
  }

  function startRefresh(): void {
    if (refreshTimer !== null) {
      return;
    }
    refreshTimer = setInterval(() => {
      if (document.visibilityState !== "hidden") {
        refreshRecentDeployments();
      }
    }, refreshIntervalMs);
    window.addEventListener("focus", onFocus);
  }

  function stopRefresh(): void {
    if (refreshTimer !== null) {
      clearInterval(refreshTimer);
      refreshTimer = null;
    }
    window.removeEventListener("focus", onFocus);
  }

  function reset(): void {
    stopRefresh();
    deployments.value = [];
    loading.value = false;
    failure.value = null;
  }

  return {
    deployments,
    loading,
    error,
    fetchRecentDeployments,
    refreshRecentDeployments,
    startRefresh,
    stopRefresh,
    reset,
  };
});
