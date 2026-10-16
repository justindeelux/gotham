import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  describeApplicationError,
  listRecentDeployments,
} from "@/features/applications";
import type { RecentDeployment } from "@/features/applications";

/** Bound the dashboard feed; the panel never shows more than this many rows. */
export const recentDeploymentsLimit = 10;

/** Cadence of the visible-only background refresh, in milliseconds. */
const refreshIntervalMs = 30_000;

/**
 * Recent-deployments store backing the dashboard panel (JUS-193). It loads the
 * aggregate feed on mount and keeps it light-refreshed while the panel is
 * visible (a 30s interval plus a refetch on window focus); every read is
 * single-flight, so an interval tick and a focus event never overlap. A fresh
 * install shows a friendly empty state instead of a broken one.
 */
export const useRecentDeploymentsStore = defineStore("dashboard-deployments", () => {
  const deployments = ref<RecentDeployment[]>([]);
  const loading = ref(false);
  /**
   * failure keeps the raw read failure. `error` derives its display text in
   * the current locale, so a language switch re-renders without refetching.
   */
  const failure = ref<unknown>(null);
  const error = computed<string | null>(() =>
    failure.value === null || failure.value === undefined
      ? null
      : describeApplicationError(failure.value),
  );

  // Interval handle and the in-flight read are kept outside reactive state;
  // the store instance is a singleton, so one of each is enough. The in-flight
  // promise makes every read single-flight: a tick landing while another read
  // runs joins it instead of firing a second request.
  let refreshTimer: ReturnType<typeof setInterval> | null = null;
  let inflight: Promise<void> | null = null;

  /** loadFeed performs one single-flight read, clearing rows on failure. */
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

  /** fetchRecentDeployments loads the feed behind the initial skeleton. */
  async function fetchRecentDeployments(): Promise<void> {
    loading.value = true;
    try {
      await loadFeed();
    } finally {
      loading.value = false;
    }
  }

  /** refreshRecentDeployments re-reads the feed without a skeleton. */
  function refreshRecentDeployments(): void {
    void loadFeed();
  }

  /** onFocus refreshes when the window regains focus (tab switch back). */
  function onFocus(): void {
    refreshRecentDeployments();
  }

  /**
   * startRefresh polls every 30s while the tab is visible and refreshes on
   * window focus. Calling it more than once is a no-op until stopRefresh runs.
   */
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

  /** stopRefresh clears the interval and the focus listener. */
  function stopRefresh(): void {
    if (refreshTimer !== null) {
      clearInterval(refreshTimer);
      refreshTimer = null;
    }
    window.removeEventListener("focus", onFocus);
  }

  /**
   * reset drops this user's cached rows and the refresh timer. Sign-out calls
   * it so the next account cannot render the previous user's deployments
   * before its own first read resolves.
   */
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
