import { defineStore } from "pinia";
import { ref } from "vue";

import {
  autoProvisionGitLab,
  deleteProvider,
  describeProviderError,
  gitlabSetupInfo,
  listBranches,
  listProviders,
  listRepos,
} from "@/features/applications/api/providers";
import type {
  AutoProvisionGitLabInput,
  GitLabSetupInfo,
  ProviderBranch,
  ProviderRepo,
  SourceProvider,
} from "@/features/applications/api/providers";
import { onLocaleChange } from "@/shared/i18n/locale";

export const useProvidersStore = defineStore("providers", () => {
  const providers = ref<SourceProvider[]>([]);
  const reposByProvider = ref<Record<string, ProviderRepo[]>>({});
  const branchesByRepo = ref<Record<string, ProviderBranch[]>>({});
  const loading = ref(false);
  const reposLoading = ref(false);
  const branchesLoading = ref(false);
  const error = ref<string | null>(null);
  const reposError = ref<string | null>(null);
  const branchesError = ref<string | null>(null);
  /**
   * errorRaw/reposErrorRaw keep the failures the banners were derived from,
   * so a language switch re-derives the curated summaries without refetching.
   */
  const errorRaw = ref<unknown>(null);
  const reposErrorRaw = ref<unknown>(null);

  // A retained failure banner re-derives its curated summary when the
  // language changes; raw diagnostics inside re-resolve to passthrough text.
  onLocaleChange(() => {
    if (errorRaw.value !== null) {
      error.value = describeProviderError(errorRaw.value);
    }
    if (reposErrorRaw.value !== null) {
      reposError.value = describeProviderError(reposErrorRaw.value);
    }
  });

  /** fetchProviders loads every source provider of the current user. */
  async function fetchProviders(): Promise<void> {
    loading.value = true;
    error.value = null;
    errorRaw.value = null;
    try {
      providers.value = await listProviders();
    } catch (err) {
      errorRaw.value = err;
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
    reposErrorRaw.value = null;
    try {
      reposByProvider.value[providerId] = await listRepos(providerId);
    } catch (err) {
      reposErrorRaw.value = err;
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

  /** branchKey scopes a branch list to one provider repository. */
  function branchKey(providerId: string, repo: string): string {
    return `${providerId}/${repo}`;
  }

  /** fetchBranches loads the branches of one provider repository. */
  async function fetchBranches(providerId: string, repo: string): Promise<void> {
    branchesLoading.value = true;
    branchesError.value = null;
    try {
      branchesByRepo.value[branchKey(providerId, repo)] = await listBranches(
        providerId,
        repo,
      );
    } catch (err) {
      branchesError.value = describeProviderError(err);
      throw err;
    } finally {
      branchesLoading.value = false;
    }
  }

  /** branchesOf returns the cached branches of one provider repository. */
  function branchesOf(providerId: string, repo: string): ProviderBranch[] {
    return branchesByRepo.value[branchKey(providerId, repo)] ?? [];
  }

  /** disconnectProvider forgets a stored connection and drops its caches. */
  async function disconnectProvider(providerId: string): Promise<void> {
    await deleteProvider(providerId);
    providers.value = providers.value.filter((item) => item.id !== providerId);
    delete reposByProvider.value[providerId];
    for (const key of Object.keys(branchesByRepo.value)) {
      if (key.startsWith(`${providerId}/`)) {
        delete branchesByRepo.value[key];
      }
    }
  }

  /** provisionGitLab creates the GitLab OAuth application automatically and
   * refreshes the provider list. */
  async function provisionGitLab(
    input: AutoProvisionGitLabInput,
  ): Promise<SourceProvider> {
    const created = await autoProvisionGitLab(input);
    await fetchProviders().catch(() => undefined);
    return created;
  }

  /** fetchSetupInfo returns the manual-application details for a GitLab instance. */
  function fetchSetupInfo(
    baseUrl: string,
    redirectUrl: string,
  ): Promise<GitLabSetupInfo> {
    return gitlabSetupInfo(baseUrl, redirectUrl);
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
    branchesByRepo.value = {};
    loading.value = false;
    reposLoading.value = false;
    branchesLoading.value = false;
    error.value = null;
    reposError.value = null;
    branchesError.value = null;
    errorRaw.value = null;
    reposErrorRaw.value = null;
  }

  return {
    providers,
    reposByProvider,
    branchesByRepo,
    loading,
    reposLoading,
    branchesLoading,
    error,
    reposError,
    branchesError,
    errorRaw,
    reposErrorRaw,
    reset,
    fetchProviders,
    fetchRepos,
    reposOf,
    fetchBranches,
    branchesOf,
    disconnectProvider,
    provisionGitLab,
    fetchSetupInfo,
    connectedProviders,
  };
});
