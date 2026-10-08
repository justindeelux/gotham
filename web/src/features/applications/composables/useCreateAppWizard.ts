import { useMessage } from "naive-ui";
import { computed, inject, provide, reactive, ref, toValue, watch, type InjectionKey, type Ref } from "vue";

import {
  createApplication,
  describeApplicationError,
} from "@/features/applications/api/applications";
import type {
  Application,
  BuildPack,
  CreateApplicationInput,
  EnvVar,
  StorageMapping,
} from "@/features/applications/api/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { useGitHubAppStore } from "@/features/applications/stores/githubApp";
import { useProvidersStore } from "@/features/applications/stores/providers";
import { useServersStore } from "@/features/servers";
import { cloneUrlFor, suggestAppName } from "@/features/applications/utils/wizardSource";
import { activeLocale, i18n } from "@/shared/i18n";
import {
  countDroppedEnvRows,
  hasEnvKeyWarnings,
} from "@/features/applications/schemas/env";
import {
  appNameSchema,
  branchSchema,
  cloneUrlSchema,
  hostPortSchema,
  providerSchema,
  publicCloneUrlSchema,
  repoSchema,
  serverSchema,
  sourceTypeImplemented,
  sourceTypeSchema,
  wizardDomainSchema,
  type SourceType,
} from "@/features/applications/schemas/applications";
import { portSchema } from "@/shared/validation/primitives";

export interface WizardForm {
  /** GS-2 source model: which fetcher the orchestrator uses. */
  sourceType: SourceType;
  providerId: string;
  publicCloneUrl: string;
  repoFullName: string;
  cloneUrl: string;
  branch: string;
  name: string;
  buildPack: BuildPack;
  /** Project the application is created in (read-only summary, changeable). */
  projectId: string;
  /** Environment the application is created in (required by the API). */
  environmentId: string;
  serverId: string;
  port: number | null;
  hostPort: number | null;
  baseDomain: string;
  env: EnvVar[];
  storage: StorageMapping[];
}

export const STEP_NAMES = ["Source", "Build pack", "Runtime", "Env & storage", "Deploy"];

export interface BuildPackOption {
  value: BuildPack;
  label: string;
  hint: string;
}

export const BUILD_PACKS: BuildPackOption[] = [
  {
    value: "",
    label: "Auto-detect (recommended)",
    hint: "The control plane picks Dockerfile, Railpack, Buildpacks or static from the repo layout.",
  },
  {
    value: "railpack",
    label: "Railpack",
    hint: "Language auto-detection without a Dockerfile.",
  },
  {
    value: "dockerfile",
    label: "Dockerfile",
    hint: "Uses the Dockerfile in the repo. Full control over the build.",
  },
  {
    value: "buildpacks",
    label: "Buildpacks",
    hint: "Heroku-style buildpacks for legacy apps.",
  },
  {
    value: "static",
    label: "Static",
    hint: "Builds a static directory and serves it via Traefik. No process runs.",
  },
];

/** WizardEvents mirrors the shell's emits for the composable. */
export interface WizardEvents {
  (_event: "update:show", _value: boolean): void;
  (_event: "created", _application: Application): void;
}

/** WizardScope carries the project/environment the wizard creates in. */
export interface WizardScope {
  projectId: string | Ref<string>;
  environmentId: string | Ref<string>;
}

/**
 * Form state, validation and submission behind the create-application wizard.
 * The shell provides the returned state to the step components through
 * `createWizardKey`, so steps read one typed source instead of long prop lists.
 * `scope` is reactive (refs stay live): a route change while the wizard is
 * mounted re-seeds the form, so data can never be written to a stale
 * environment.
 */
export function useCreateAppWizard(
  show: Ref<boolean>,
  emit: WizardEvents,
  scope: WizardScope = { projectId: "", environmentId: "" },
) {
  const providersStore = useProvidersStore();
  const githubAppStore = useGitHubAppStore();
  const serversStore = useServersStore();
  const appsStore = useApplicationsStore();
  const message = useMessage();

  const step = ref(0);
  const submitting = ref(false);
  /**
   * submitFailure keeps the submit failure for the retained banner: a missing
   * scope (curated key) or the raw thrown error. errorMessage derives the
   * display text in the current locale, so a language switch refreshes the
   * banner without clearing the draft.
   */
  const submitFailure = ref<{ kind: "scope" } | { kind: "error"; error: unknown } | null>(null);
  /**
   * noSshUrl flags the private-repo-without-SSH-URL gap; sourceError derives
   * its display text in the current locale.
   */
  const noSshUrl = ref(false);

  /**
   * tr resolves one applications message in the current locale. Reading
   * activeLocale pins the calling computed to the language switch.
   */
  function tr(key: string, params?: Record<string, string | number>): string {
    void activeLocale.value;
    return String(i18n.global.t(key, params ?? {}));
  }

  /** errorMessage derives the retained submit banner in the current locale. */
  const errorMessage = computed<string>(() => {
    if (submitFailure.value === null) {
      return "";
    }
    if (submitFailure.value.kind === "scope") {
      return tr("applications.wizard.scopeError");
    }
    return describeApplicationError(submitFailure.value.error);
  });

  /** sourceError derives the SSH-URL gap warning in the current locale. */
  const sourceError = computed<string>(() =>
    noSshUrl.value ? tr("applications.wizard.noSshUrl") : "",
  );

  /** stepNames renders the rail labels in the current locale. */
  const stepNames = computed<string[]>(() => [
    tr("applications.wizard.steps.source"),
    tr("applications.wizard.steps.buildPack"),
    tr("applications.wizard.steps.runtime"),
    tr("applications.wizard.steps.envStorage"),
    tr("applications.wizard.steps.deploy"),
  ]);

  /** buildPacks renders the pack choices in the current locale. */
  const buildPacks = computed<BuildPackOption[]>(() => [
    {
      value: "",
      label: tr("applications.wizard.packAuto"),
      hint: tr("applications.wizard.packAutoHint"),
    },
    {
      value: "railpack",
      label: tr("applications.wizard.packRailpack"),
      hint: tr("applications.wizard.packRailpackHint"),
    },
    {
      value: "dockerfile",
      label: tr("applications.wizard.packDockerfile"),
      hint: tr("applications.wizard.packDockerfileHint"),
    },
    {
      value: "buildpacks",
      label: tr("applications.wizard.packBuildpacks"),
      hint: tr("applications.wizard.packBuildpacksHint"),
    },
    {
      value: "static",
      label: tr("applications.wizard.packStatic"),
      hint: tr("applications.wizard.packStaticHint"),
    },
  ]);

  const form = reactive<WizardForm>({
    sourceType: "git_public",
    providerId: "",
    publicCloneUrl: "",
    repoFullName: "",
    cloneUrl: "",
    branch: "main",
    name: "",
    buildPack: "",
    projectId: toValue(scope.projectId),
    environmentId: toValue(scope.environmentId),
    serverId: "",
    port: 3000,
    hostPort: null,
    baseDomain: "",
    env: [{ key: "NODE_ENV", value: "production" }],
    storage: [],
  });

  const providerOptions = computed<Array<{ label: string; value: string }>>(() => {
    // The provider list follows the source type, so a gitlab connection can
    // never be submitted under a github_app source (and the public sentinel
    // stays out of the provider flow entirely). github_app lists GitHub App
    // connections (GS-5); gitlab_app keeps the OAuth provider list.
    if (form.sourceType === "github_app") {
      return githubAppStore.apps.map((item) => ({
        label: `${item.name || item.slug} · ${item.connected ? tr("applications.wizard.connected") : tr("applications.wizard.notConnected")}`,
        value: item.id,
      }));
    }
    const providers =
      form.sourceType === "gitlab_app"
        ? providersStore.providers.filter((item) => item.provider === "gitlab")
        : providersStore.providers;
    return providers.map((item) => ({
      label: `${item.provider} · ${item.connected ? tr("applications.wizard.connected") : tr("applications.wizard.notConnected")}`,
      value: item.id,
    }));
  });

  const isPublicRepo = computed<boolean>(() => form.sourceType === "git_public");

  /** isProviderFlow covers the connected-provider types (existing flow). */
  const isProviderFlow = computed<boolean>(
    () => form.sourceType === "github_app" || form.sourceType === "gitlab_app",
  );

  /** isGitHubAppFlow covers the GitHub App type (GS-5): repos and branches
   * come from the installation, not the OAuth provider list. */
  const isGitHubAppFlow = computed<boolean>(() => form.sourceType === "github_app");

  /** sourceTypeOptions renders the GS-2 type selector in the current locale. */
  const sourceTypeOptions = computed<Array<{ label: string; value: string; disabled?: boolean }>>(() => [
    { label: tr("applications.wizard.sourceGitPublic"), value: "git_public" },
    { label: tr("applications.wizard.sourceGitPrivate"), value: "git_private", disabled: true },
    { label: tr("applications.wizard.sourceGithubApp"), value: "github_app" },
    { label: tr("applications.wizard.sourceGitlabApp"), value: "gitlab_app" },
    { label: tr("applications.wizard.sourceDockerfile"), value: "dockerfile", disabled: true },
    { label: tr("applications.wizard.sourceCompose"), value: "compose", disabled: true },
    { label: tr("applications.wizard.sourceImage"), value: "image", disabled: true },
  ]);

  const repoOptions = computed<Array<{ label: string; value: string }>>(() => {
    const repos = isGitHubAppFlow.value
      ? githubAppStore.reposOf(form.providerId)
      : providersStore.reposOf(form.providerId);
    return repos.map((repo) => ({
      label: `${repo.full_name}${repo.private ? tr("applications.wizard.privateSuffix") : ""}`,
      value: repo.full_name,
    }));
  });

  /** branchOptions lists the branches of the selected repository: the
   * installation branches for the GitHub App flow, the provider branches
   * otherwise. */
  const branchOptions = computed<Array<{ label: string; value: string }>>(() => {
    const suffix = tr("applications.wizard.protectedSuffix");
    const branches = isGitHubAppFlow.value
      ? githubAppStore.branchesOf(form.providerId, form.repoFullName)
      : providersStore.branchesOf(form.providerId, form.repoFullName);
    return branches.map((branch) => ({
      label: branch.protected ? `${branch.name}${suffix}` : branch.name,
      value: branch.name,
    }));
  });

  const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
    serversStore.servers.map((server) => ({
      label: `${server.name} · ${server.ip}`,
      value: server.id,
    })),
  );

  /** selectedProviderName resolves the provider slug for the create payload. */
  const selectedProviderName = computed<string>(() => {
    if (!isProviderFlow.value) {
      return "";
    }
    // The GitHub App flow authenticates through the installation, but the
    // application row still keys hooks and deploy keys off provider "github".
    if (isGitHubAppFlow.value) {
      return "github";
    }
    return (
      providersStore.providers.find((item) => item.id === form.providerId)
        ?.provider ?? ""
    );
  });

  /** sourceValid gates the Source step: the type, its fields, branch, name. */
  const sourceValid = computed<boolean>(() => {
    if (!sourceTypeSchema.safeParse(form.sourceType).success) {
      return false;
    }
    // Unimplemented types (git_private until GS-4, containers until GS-7..9)
    // render a not-yet-available placeholder, so the step cannot continue.
    if (!sourceTypeImplemented(form.sourceType)) {
      return false;
    }
    switch (form.sourceType) {
      case "git_public":
        // Keyless by definition: only the anonymous http(s)/git schemes pass,
        // so an SSH URL can never reach a clone on ambient credentials.
        if (!publicCloneUrlSchema.safeParse(form.publicCloneUrl).success) {
          return false;
        }
        break;
      default: {
        // github_app/gitlab_app keep the existing provider-backed flow.
        if (!providerSchema.safeParse(form.providerId).success) {
          return false;
        }
        if (!repoSchema.safeParse(form.repoFullName).success) {
          return false;
        }
        // A private repository without a provider ssh_url is rejected rather than
        // silently degraded to https: the keyed cloner would rewrite that URL and
        // drop a self-hosted SSH port. sourceError names the gap.
        if (!cloneUrlSchema.safeParse(form.cloneUrl).success) {
          return false;
        }
        break;
      }
    }
    // A public repo with no branch pins the remote default at clone time
    // (ls-remote); provider flows keep the required prefilled branch.
    const branchOk =
      form.sourceType === "git_public" && form.branch.trim() === ""
        ? true
        : branchSchema.safeParse(form.branch).success;
    return branchOk && appNameSchema.safeParse(form.name).success;
  });

  /** runtimeValid gates the Runtime step: a node and a valid port. */
  const runtimeValid = computed<boolean>(() => {
    if (!serverSchema.safeParse(form.serverId).success) {
      return false;
    }
    if (!portSchema.safeParse(form.port).success) {
      return false;
    }
    if (!hostPortSchema.safeParse(form.hostPort).success) {
      return false;
    }
    return wizardDomainSchema.safeParse(form.baseDomain).success;
  });

  /** scopeValid gates the submit: the API requires an environment. */
  const scopeValid = computed<boolean>(() => form.environmentId !== "");

  /**
   * Environment names are warn-only: the API accepts any structurally valid
   * name, so the wizard must not block one it accepts. The alert names rows that
   * deviate from the convention; nameless rows are reported because the payload
   * drops them.
   */
  const envKeyWarnings = computed<boolean>(() => hasEnvKeyWarnings(form.env));

  const droppedEnvRows = computed<number>(() => countDroppedEnvRows(form.env));

  const canContinue = computed<boolean>(() => {
    switch (step.value) {
      case 0:
        return sourceValid.value;
      case 2:
        return runtimeValid.value;
      default:
        return true;
    }
  });

  const buildPackLabel = computed<string>(
    () => buildPacks.value.find((item) => item.value === form.buildPack)?.label ?? tr("applications.wizard.packAuto"),
  );

  /** reviewSource renders the repo headline on the review step. */
  const reviewSource = computed<string>(() => {
    const repo =
      form.sourceType === "git_public" ? form.publicCloneUrl.trim() : form.repoFullName;
    const branch = form.branch.trim();
    return `${repo} · ${branch === "" ? tr("applications.wizard.branchDefault") : branch}`;
  });

  // Entering the wizard loads providers and nodes; changing provider loads repos.
  watch(
    show,
    (visible) => {
      if (visible) {
        // Re-seed from the live scope: the route may have moved while the
        // wizard was closed (or mounted), and reset-on-close alone would keep
        // the stale environment for the next open.
        seedScope();
        void providersStore.fetchProviders().catch(() => undefined);
        void serversStore.fetchServers().catch(() => undefined);
      } else {
        resetWizard();
      }
    },
  );

  // A route change while the wizard is mounted re-seeds the scope, so the
  // summary and the payload always name the current environment.
  watch(
    [() => toValue(scope.projectId), () => toValue(scope.environmentId)],
    () => seedScope(),
  );

  watch(
    () => form.providerId,
    (providerId) => {
      form.repoFullName = "";
      form.cloneUrl = "";
      noSshUrl.value = false;
      if (providerId !== "") {
        void loadRepos();
      }
    },
  );

  // Switching the source type drops the previous type's fields, so a github
  // provider picked under github_app can never leak into a gitlab_app
  // payload (and the backend agreement check would reject it anyway).
  // Entering the GitHub App flow loads the connections for the selector.
  watch(
    () => form.sourceType,
    () => {
      form.providerId = "";
      form.publicCloneUrl = "";
      form.repoFullName = "";
      form.cloneUrl = "";
      noSshUrl.value = false;
      if (form.sourceType === "github_app") {
        void githubAppStore.fetchApps().catch(() => undefined);
      }
    },
  );

  /** loadRepos fetches the selected provider's repositories; the store exposes any error. */
  async function loadRepos(): Promise<void> {
    if (form.providerId === "") {
      return;
    }
    if (isGitHubAppFlow.value) {
      await githubAppStore.fetchRepos(form.providerId).catch(() => undefined);
      return;
    }
    await providersStore.fetchRepos(form.providerId).catch(() => undefined);
  }

  /** loadBranches fetches the selected repository's branches; failures keep
   * the free-text branch input (the store exposes the error). */
  async function loadBranches(): Promise<void> {
    if (form.providerId === "" || form.repoFullName === "") {
      return;
    }
    await providersStore
      .fetchBranches(form.providerId, form.repoFullName)
      .catch(() => undefined);
  }

  /** handleRepoSelect prefills branch, clone URL and a name from the repo. */
  function handleRepoSelect(fullName: string): void {
    if (isGitHubAppFlow.value) {
      const repo = githubAppStore.reposOf(form.providerId).find((item) => item.full_name === fullName);
      if (!repo) {
        return;
      }
      // The installation token is injected at clone time, so the stored URL
      // is always the https clone_url: an ssh_url would fail the token
      // cloner, which only accepts http(s).
      form.cloneUrl = repo.clone_url;
      noSshUrl.value = false;
      if (repo.default_branch) {
        form.branch = repo.default_branch;
      }
      if (form.name.trim() === "") {
        form.name = suggestAppName(repo.name);
      }
      void githubAppStore.fetchBranches(form.providerId, fullName).catch(() => undefined);
      return;
    }
    const repo = providersStore.reposOf(form.providerId).find((item) => item.full_name === fullName);
    if (!repo) {
      return;
    }
    form.cloneUrl = cloneUrlFor(repo);
    noSshUrl.value = false;
    if (repo.private && form.cloneUrl === "") {
      noSshUrl.value = true;
    }
    if (repo.default_branch) {
      form.branch = repo.default_branch;
    }
    if (form.name.trim() === "") {
      form.name = suggestAppName(repo.name);
    }
    void loadBranches();
  }

  /** buildPayload assembles the create-application body from the wizard state. */
  function buildPayload(): CreateApplicationInput {
    const source =
      form.sourceType === "git_public"
        ? { repo: form.publicCloneUrl.trim(), cloneUrl: form.publicCloneUrl.trim() }
        : { repo: form.repoFullName, cloneUrl: form.cloneUrl };
    return {
      name: form.name.trim(),
      environment_id: form.environmentId,
      provider: selectedProviderName.value,
      repo: source.repo,
      clone_url: source.cloneUrl,
      source_type: form.sourceType,
      branch: form.branch.trim(),
      build_pack: form.buildPack,
      base_domain: form.baseDomain.trim(),
      port: form.port ?? 3000,
      host_port: form.hostPort ?? 0,
      server_id: form.serverId,
      env: form.env.filter((row) => row.key.trim() !== ""),
      storage: form.storage.filter((row) => row.name.trim() !== ""),
    };
  }

  /** handleSubmit posts the wizard payload, queues the first deploy and reports. */
  async function handleSubmit(): Promise<void> {
    submitFailure.value = null;
    if (!scopeValid.value) {
      submitFailure.value = { kind: "scope" };
      return;
    }
    submitting.value = true;
    try {
      const { application, webhook } = await createApplication(buildPayload());
      // The create route only stores the row; "Create & deploy" must queue the
      // first deployment explicitly and surface whether it was queued.
      try {
        await appsStore.deploy(application.id);
        message.success(
          tr("applications.wizard.createdQueued", { name: application.name }),
        );
      } catch (deployError) {
        message.warning(
          tr("applications.wizard.createdDeployFailed", {
            name: application.name,
            error: describeApplicationError(deployError),
          }),
          { duration: 8000 },
        );
      }
      if (webhook && !webhook.installed) {
        message.warning(
          tr("applications.wizard.webhookOff", {
            detail:
              webhook.error ?? tr("applications.wizard.webhookOffDefault"),
          }),
          { duration: 8000 },
        );
      }
      emit("created", application);
      emit("update:show", false);
      resetWizard();
    } catch (error) {
      submitFailure.value = { kind: "error", error };
    } finally {
      submitting.value = false;
    }
  }

  /** closeWizard closes the modal and resets the wizard state. */
  function closeWizard(): void {
    emit("update:show", false);
    resetWizard();
  }

  /** seedScope copies the live route scope into the form. */
  function seedScope(): void {
    form.projectId = toValue(scope.projectId);
    form.environmentId = toValue(scope.environmentId);
  }

  /** handleShowChange mirrors the modal visibility and resets when closing. */
  function handleShowChange(value: boolean): void {
    emit("update:show", value);
    if (!value) {
      resetWizard();
    }
  }

  /** resetWizard returns every field to its initial value. */
  function resetWizard(): void {
    step.value = 0;
    form.sourceType = "git_public";
    form.providerId = "";
    form.publicCloneUrl = "";
    form.repoFullName = "";
    form.cloneUrl = "";
    form.branch = "main";
    form.name = "";
    form.buildPack = "";
    seedScope();
    form.serverId = "";
    form.port = 3000;
    form.hostPort = null;
    form.baseDomain = "";
    form.env = [{ key: "NODE_ENV", value: "production" }];
    form.storage = [];
    submitFailure.value = null;
    noSshUrl.value = false;
    submitting.value = false;
    // The provider store is a singleton: a stale repo error must not survive
    // into the next wizard with a Retry that no longer applies.
    providersStore.reposError = null;
    githubAppStore.clearErrors();
    providersStore.reposErrorRaw = null;
  }

  return {
    form,
    step,
    stepNames,
    buildPacks,
    submitting,
    errorMessage,
    sourceError,
    providersStore,
    githubAppStore,
    providerOptions,
    sourceTypeOptions,
    isPublicRepo,
    isProviderFlow,
    isGitHubAppFlow,
    repoOptions,
    branchOptions,
    serverOptions,
    sourceValid,
    runtimeValid,
    scopeValid,
    envKeyWarnings,
    droppedEnvRows,
    canContinue,
    buildPackLabel,
    reviewSource,
    buildPayload,
    loadRepos,
    loadBranches,
    handleRepoSelect,
    handleSubmit,
    closeWizard,
    handleShowChange,
  };
}

/** CreateWizardState is the single typed source the step components inject. */
export type CreateWizardState = ReturnType<typeof useCreateAppWizard>;

export const createWizardKey: InjectionKey<CreateWizardState> = Symbol("create-wizard");

/** provideCreateWizard shares the shell's wizard state with the step components. */
export function provideCreateWizard(state: CreateWizardState): void {
  provide(createWizardKey, state);
}

/** useCreateWizardState reads the shell-provided wizard state in a step. */
export function useCreateWizardState(): CreateWizardState {
  const state = inject(createWizardKey);
  if (!state) {
    throw new Error("useCreateWizardState must be used inside CreateAppWizard");
  }
  return state;
}

