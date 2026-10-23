import { computed, ref } from "vue";

import {
  installUrl,
  startManifest,
} from "@/features/applications/api/githubApp";
import type { GitHubApp } from "@/features/applications/api/githubApp";
import { listApplications } from "@/features/applications/api/applications";
import type { Application } from "@/features/applications/api/applications";
import {
  authorizeProvider,
  autoProvisionGitLab,
  createProvider,
  deleteProvider,
  describeProviderError,
  gitlabSetupInfo,
  resolveGitlabCallbackUrl,
} from "@/features/applications/api/providers";
import type { SourceProvider } from "@/features/applications/api/providers";
import { describeGitHubAppError } from "@/features/applications/api/githubApp";
import { useGitHubAppStore } from "@/features/applications/stores/githubApp";
import { useProvidersStore } from "@/features/applications/stores/providers";
import { useTeamsStore } from "@/features/teams";
import { isApiError } from "@/features/servers";

export interface GitSourceRow {
  kind: "github-app" | "provider";
  id: string;
  title: string;
  subtitle: string;
  connected: boolean;
  account: string;
  instance: string;
  installations: number | null;
  repos: number | null;
  apps: string[];
  createdAt: string;
  legacy: boolean;
}

export function hostOf(value: string): string {
  try {
    return new URL(value).hostname.toLowerCase();
  } catch {
    return "";
  }
}

export function appsForGitHubApp(apps: Application[], appId: string): string[] {
  return apps.filter((app) => app.github_app_id === appId).map((app) => app.name);
}

export function appsForProvider(apps: Application[], provider: SourceProvider): string[] {
  const host = hostOf(provider.base_url);
  if (host === "") {
    return [];
  }
  return apps
    .filter(
      (app) =>
        app.provider === provider.provider &&
        hostOf(app.clone_url) === host &&
        app.github_app_id === "",
    )
    .map((app) => app.name);
}

export function isLegacyProvider(provider: SourceProvider): boolean {
  return provider.provider !== "gitlab";
}

export function rowForGitHubApp(app: GitHubApp, apps: Application[], repos: number | null): GitSourceRow {
  const accounts = app.installations.map((inst) => inst.account).filter(Boolean);
  return {
    kind: "github-app",
    id: app.id,
    title: "GitHub App",
    subtitle: app.slug || app.name,
    connected: app.connected,
    account: accounts.length > 0 ? accounts.join(", ") : "—",
    instance: hostOf(app.base_url) || app.base_url,
    installations: app.installations.length,
    repos,
    apps: appsForGitHubApp(apps, app.id),
    createdAt: app.created_at,
    legacy: false,
  };
}

export function rowForProvider(
  provider: SourceProvider,
  apps: Application[],
  repos: number | null,
): GitSourceRow {
  const legacy = isLegacyProvider(provider);
  return {
    kind: "provider",
    id: provider.id,
    title: provider.provider === "gitlab" ? "GitLab" : provider.provider,
    subtitle: legacy ? "Legacy OAuth · read-only" : "OAuth · PKCE",
    connected: provider.connected,
    account: hostOf(provider.base_url) || provider.base_url,
    instance: provider.scopes || "—",
    installations: null,
    repos,
    apps: appsForProvider(apps, provider),
    createdAt: provider.created_at,
    legacy,
  };
}

export interface DisconnectResult {
  applicationsUsing: number;
}

export function useGitSourcesPage() {
  const githubAppStore = useGitHubAppStore();
  const providersStore = useProvidersStore();
  const teamsStore = useTeamsStore();

  const loading = ref(false);
  const error = ref<string | null>(null);
  const teamApps = ref<Application[]>([]);
  const usageState = ref<"loading" | "known" | "unknown">("loading");
  const disconnectError = ref<string | null>(null);
  const disconnectNames = ref<string[]>([]);

  const usageKnown = computed<boolean>(() => usageState.value === "known");
  const usageLoading = computed<boolean>(() => usageState.value === "loading");

  const rows = computed<GitSourceRow[]>(() => {
    const github = githubAppStore.apps.map((app) =>
      rowForGitHubApp(
        app,
        teamApps.value,
        githubAppStore.reposByApp[app.id] === undefined
          ? null
          : githubAppStore.reposByApp[app.id].length,
      ),
    );
    const providers = providersStore.providers.map((provider) =>
      rowForProvider(
        provider,
        teamApps.value,
        providersStore.reposByProvider[provider.id] === undefined
          ? null
          : providersStore.reposByProvider[provider.id].length,
      ),
    );
    return [...github, ...providers];
  });

  async function refresh(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      await Promise.all([githubAppStore.fetchApps(), providersStore.fetchProviders()]);
    } catch (err) {
      error.value = describeProviderError(err);
      return;
    } finally {
      loading.value = false;
    }
    usageState.value = "loading";
    try {
      teamApps.value = await listApplications(teamsStore.activeTeamId);
      usageState.value = "known";
    } catch {
      teamApps.value = [];
      usageState.value = "unknown";
    }
    const repos: Array<Promise<unknown>> = [];
    for (const app of githubAppStore.apps) {
      if (app.connected && githubAppStore.reposByApp[app.id] === undefined) {
        repos.push(githubAppStore.fetchRepos(app.id).catch(() => undefined));
      }
    }
    for (const provider of providersStore.providers) {
      if (provider.connected && providersStore.reposByProvider[provider.id] === undefined) {
        repos.push(providersStore.fetchRepos(provider.id).catch(() => undefined));
      }
    }
    await Promise.all(repos);
  }

  async function connectGitHub(name: string): Promise<void> {
    const manifest = await startManifest("", name);
    const form = document.createElement("form");
    form.method = "POST";
    form.action = manifest.action_url;
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = "manifest";
    input.value = JSON.stringify(manifest.manifest);
    form.appendChild(input);
    document.body.appendChild(form);
    form.submit();
  }

  async function installGitHubApp(appId: string): Promise<void> {
    const install = await installUrl(appId);
    window.location.href = install.install_url;
  }

  async function provisionGitLabAuto(baseUrl: string, adminToken: string): Promise<SourceProvider> {
    const created = await autoProvisionGitLab({
      base_url: baseUrl,
      admin_token: adminToken,
      redirect_url: await resolveGitlabCallbackUrl(),
    });
    await providersStore.fetchProviders().catch(() => undefined);
    return created;
  }

  async function gitLabManualInfo(baseUrl: string) {
    return gitlabSetupInfo(baseUrl, await resolveGitlabCallbackUrl());
  }

  async function provisionGitLabManual(input: {
    baseUrl: string;
    clientId: string;
    clientSecret: string;
    scopes?: string;
  }): Promise<SourceProvider> {
    const created = await createProvider({
      provider: "gitlab",
      base_url: input.baseUrl,
      client_id: input.clientId,
      client_secret: input.clientSecret,
      redirect_url: await resolveGitlabCallbackUrl(),
      scopes: input.scopes,
    });
    await providersStore.fetchProviders().catch(() => undefined);
    return created;
  }

  async function startAuthorize(providerId: string): Promise<void> {
    const auth = await authorizeProvider(providerId);
    window.location.href = auth.url;
  }

  async function reconnect(row: GitSourceRow): Promise<void> {
    if (row.kind === "github-app") {
      await installGitHubApp(row.id);
      return;
    }
    const auth = await authorizeProvider(row.id);
    window.location.href = auth.url;
  }

  async function disconnect(row: GitSourceRow): Promise<DisconnectResult> {
    disconnectError.value = null;
    disconnectNames.value = [];
    try {
      if (row.kind === "github-app") {
        const applicationsUsing = await githubAppStore.disconnect(row.id);
        await refresh().catch(() => undefined);
        return { applicationsUsing };
      }
      await deleteProvider(row.id);
      await providersStore.fetchProviders().catch(() => undefined);
      try {
        teamApps.value = await listApplications(teamsStore.activeTeamId);
        usageState.value = "known";
      } catch {
        teamApps.value = [];
        usageState.value = "unknown";
      }
      return { applicationsUsing: 0 };
    } catch (err) {
      if (isApiError(err) && err.status === 409) {
        disconnectNames.value = namesFromConflict(err.message);
      }
      disconnectError.value =
        row.kind === "github-app" ? describeGitHubAppError(err) : describeProviderError(err);
      throw err;
    }
  }

  return {
    rows,
    loading,
    error,
    usageKnown,
    usageLoading,
    disconnectError,
    disconnectNames,
    githubAppStore,
    providersStore,
    refresh,
    connectGitHub,
    installGitHubApp,
    provisionGitLabAuto,
    gitLabManualInfo,
    provisionGitLabManual,
    startAuthorize,
    reconnect,
    disconnect,
  };
}

export function namesFromConflict(message: string): string[] {
  const separator = message.indexOf(": ");
  const tail = separator === -1 ? message : message.slice(separator + 2);
  return tail
    .split(",")
    .map((part) => part.trim())
    .filter((part) => part !== "");
}
