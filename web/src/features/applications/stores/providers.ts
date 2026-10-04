import { defineStore } from "pinia";
import { ref } from "vue";

import {
  describeProviderError,
  listProviders,
  listRepos,
} from "@/features/applications/api/providers";
import type { ProviderRepo, SourceProvider } from "@/features/applications/api/providers";

export const useProvidersStore = defineStore("providers", () => {
  const providers = ref<SourceProvider[]>([]);
  const reposByProvider = ref<Record<string, ProviderRepo[]>>({});
  const loading = ref(false);
  const reposLoading = ref(false);
  const error = ref<string | null>(null);
  const reposError = ref<string | null>(null);

  /** fetchProviders loads every source provider of the current user. */
  async function fetchProviders(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      providers.value = await listProviders();
    } catch (err) {
      error.value = describeProviderError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** fetchRepos loads the repositories visible through one provider. */
  async function fetchRepos(providerId: string): Promise<void> {
    reposLoading.value = true;
    reposError.value = null;
    try {
      reposByProvider.value[providerId] = await listRepos(providerId);
    } catch (err) {
      reposError.value = describeProviderError(err);
      throw err;
    } finally {
      reposLoading.value = false;
    }
  }

  /** reposOf returns the cached repos of one provider, or an empty list. */
  function reposOf(providerId: string): ProviderRepo[] {
    return reposByProvider.value[providerId] ?? [];
  }

  /** connectedProviders lists only providers with stored credentials. */
  function connectedProviders(): SourceProvider[] {
    return providers.value.filter((item) => item.connected);
  }

  /**
   * reset drops the cached providers and repos, so the next sign-in never
   * sees the previous account's data. Called on sign-out (see the auth
   * store).
   */
  function reset(): void {
    providers.value = [];
    reposByProvider.value = {};
    loading.value = false;
    reposLoading.value = false;
    error.value = null;
    reposError.value = null;
  }

  return {
    providers,
    reposByProvider,
    loading,
    reposLoading,
    error,
    reposError,
    reset,
    fetchProviders,
    fetchRepos,
    reposOf,
    connectedProviders,
  };
});
