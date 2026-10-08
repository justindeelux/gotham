import { defineStore } from "pinia";
import { ref } from "vue";

import {
  describeGitHubAppError,
  disconnectGitHubApp,
  listGitHubApps,
  listGitHubBranches,
  listGitHubRepos,
} from "@/features/applications/api/githubApp";
import type {
  GitHubApp,
  GitHubBranch,
  GitHubRepo,
} from "@/features/applications/api/githubApp";
import { onLocaleChange } from "@/shared/i18n/locale";

/**
 * GitHub App connections (GS-5): apps created through the manifest flow,
 * their installations' repositories and branches. Mirrors the providers
 * store shape, so the wizard consumes both the same way.
 */
export const useGitHubAppStore = defineStore("github-app", () => {
  const apps = ref<GitHubApp[]>([]);
  const reposByApp = ref<Record<string, GitHubRepo[]>>({});
  const branchesByRepo = ref<Record<string, GitHubBranch[]>>({});
  const loading = ref(false);
  const reposLoading = ref(false);
  const branchesLoading = ref(false);
  const error = ref<string | null>(null);
  const reposError = ref<string | null>(null);
  const errorRaw = ref<unknown>(null);
  const reposErrorRaw = ref<unknown>(null);

  // A retained failure banner re-derives its curated summary when the
  // language changes; raw diagnostics inside re-resolve to passthrough text.
  onLocaleChange(() => {
    if (errorRaw.value !== null) {
      error.value = describeGitHubAppError(errorRaw.value);
    }
    if (reposErrorRaw.value !== null) {
      reposError.value = describeGitHubAppError(reposErrorRaw.value);
    }
  });

  /** fetchApps loads every GitHub App of the current user. */
  async function fetchApps(): Promise<void> {
    loading.value = true;
    error.value = null;
    errorRaw.value = null;
    try {
      apps.value = await listGitHubApps();
    } catch (err) {
      errorRaw.value = err;
      error.value = describeGitHubAppError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** fetchRepos loads the repositories visible through one app installation. */
  async function fetchRepos(appId: string): Promise<void> {
    reposLoading.value = true;
    reposError.value = null;
    reposErrorRaw.value = null;
    try {
      reposByApp.value[appId] = await listGitHubRepos(appId);
    } catch (err) {
      reposErrorRaw.value = err;
      reposError.value = describeGitHubAppError(err);
      throw err;
    } finally {
      reposLoading.value = false;
    }
  }

  /** fetchBranches loads the branches of one repository. */
  async function fetchBranches(appId: string, repo: string): Promise<void> {
    branchesLoading.value = true;
    try {
      branchesByRepo.value[`${appId}/${repo}`] = await listGitHubBranches(appId, repo);
    } finally {
      branchesLoading.value = false;
    }
  }

  /** reposOf returns the cached repos of one app, or an empty list. */
  function reposOf(appId: string): GitHubRepo[] {
    return reposByApp.value[appId] ?? [];
  }

  /** branchesOf returns the cached branches of one repository, or empty. */
  function branchesOf(appId: string, repo: string): GitHubBranch[] {
    return branchesByRepo.value[`${appId}/${repo}`] ?? [];
  }

  /** connectedApps lists only apps with at least one installation. */
  function connectedApps(): GitHubApp[] {
    return apps.value.filter((item) => item.connected);
  }

  /** disconnect deletes the stored credentials of one app. */
  async function disconnect(appId: string): Promise<number> {
    const result = await disconnectGitHubApp(appId);
    apps.value = apps.value.filter((item) => item.id !== appId);
    delete reposByApp.value[appId];
    return result.applications_using;
  }

  /**
   * reset drops the cached apps and repos, so the next sign-in never sees
   * the previous account's data. Called on sign-out (see the auth store).
   */
  function reset(): void {
    apps.value = [];
    reposByApp.value = {};
    branchesByRepo.value = {};
    loading.value = false;
    reposLoading.value = false;
    branchesLoading.value = false;
    error.value = null;
    reposError.value = null;
    errorRaw.value = null;
    reposErrorRaw.value = null;
  }

  return {
    apps,
    reposByApp,
    branchesByRepo,
    loading,
    reposLoading,
    branchesLoading,
    error,
    reposError,
    fetchApps,
    fetchRepos,
    fetchBranches,
    reposOf,
    branchesOf,
    connectedApps,
    disconnect,
    reset,
  };
});
