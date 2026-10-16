import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  describeApplicationError,
  listRecentDeployments,
} from "@/features/applications";
import type { RecentDeployment } from "@/features/applications";

/** Bound the dashboard feed; the panel never shows more than this many rows. */
export const recentDeploymentsLimit = 10;

/**
 * Recent-deployments store backing the dashboard panel (JUS-193). It reads the
 * aggregate feed once on mount — the same read-once scope as the applications
 * tile — so a fresh install shows a friendly empty state instead of a broken
 * one, and a returning operator sees the newest deploys across their team.
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

  async function fetchRecentDeployments(): Promise<void> {
    loading.value = true;
    failure.value = null;
    try {
      deployments.value = await listRecentDeployments(recentDeploymentsLimit);
    } catch (err) {
      deployments.value = [];
      failure.value = err;
    } finally {
      loading.value = false;
    }
  }

  return { deployments, loading, error, fetchRecentDeployments };
});
