import { useMessage } from "naive-ui";
import { onMounted, ref } from "vue";

import { describeApplicationError, listApplications } from "@/features/applications/api/applications";
import type { Application } from "@/features/applications/api/applications";
import { useProvidersStore } from "@/features/applications/stores/providers";

/**
 * Application list state behind the Applications page. The page itself only
 * handles navigation and layout.
 */
export function useApplicationsList() {
  const message = useMessage();
  const providersStore = useProvidersStore();

  const wizardOpen = ref(false);
  const listError = ref<string | null>(null);
  // Start loading so the first paint shows the spinner, never the empty state
  // before the initial list response lands (C4-16).
  const listLoading = ref(true);
  const knownApps = ref<Application[]>([]);
  const openById = ref("");

  /**
   * fetchKnownApplications reads the mounted list route. A rejection renders
   * explicitly — the page never fabricates rows.
   */
  async function fetchKnownApplications(): Promise<void> {
    listLoading.value = true;
    listError.value = null;
    try {
      knownApps.value = await listApplications();
    } catch (error) {
      knownApps.value = [];
      listError.value = describeApplicationError(error);
    } finally {
      listLoading.value = false;
    }
  }

  /** refreshProviders reloads the provider connections. */
  async function refreshProviders(): Promise<void> {
    try {
      await providersStore.fetchProviders();
      message.success("Providers refreshed");
    } catch {
      // The store already exposes the error; no extra toast needed.
    }
  }

  onMounted(() => {
    void fetchKnownApplications();
    void providersStore.fetchProviders().catch(() => undefined);
  });

  return {
    wizardOpen,
    listError,
    listLoading,
    knownApps,
    openById,
    fetchKnownApplications,
    refreshProviders,
  };
}
