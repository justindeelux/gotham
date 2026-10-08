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
  /** SSH clone URL for a private repository without a provider (GS-4). */
  privateCloneUrl: string;
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

export const PUBLIC_PROVIDER = "public";

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
    privateCloneUrl: "",
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

  const providerOptions = computed<Array<{ label: string; value: string }>>(() => [
    ...providersStore.providers.map((item) => ({
      label: `${item.provider} · ${item.connected ? tr("applications.wizard.connected") : tr("applications.wizard.notConnected")}`,
      value: item.id,
    })),
    { label: tr("applications.wizard.publicRepo"), value: PUBLIC_PROVIDER },
  ]);

  const isPublicRepo = computed<boolean>(() => form.sourceType === "git_public");

  const isPrivateRepo = computed<boolean>(() => form.sourceType === "git_private");

  /** isProviderFlow covers the connected-provider types (existing flow). */
  const isProviderFlow = computed<boolean>(
    () => form.sourceType === "github_app" || form.sourceType === "gitlab_app",
  );

  /** sourceTypeOptions renders the GS-2 type selector in the current locale. */
  const sourceTypeOptions = computed<Array<{ label: string; value: string }>>(() => [
    { label: tr("applications.wizard.sourceGitPublic"), value: "git_public" },
    { label: tr("applications.wizard.sourceGitPrivate"), value: "git_private" },
    { label: tr("applications.wizard.sourceGithubApp"), value: "github_app" },
    { label: tr("applications.wizard.sourceGitlabApp"), value: "gitlab_app" },
    { label: tr("applications.wizard.sourceDockerfile"), value: "dockerfile" },
    { label: tr("applications.wizard.sourceCompose"), value: "compose" },
    { label: tr("applications.wizard.sourceImage"), value: "image" },
  ]);

  const repoOptions = computed<Array<{ label: string; value: string }>>(() =>
    providersStore.reposOf(form.providerId).map((repo) => ({
      label: `${repo.full_name}${repo.private ? tr("applications.wizard.privateSuffix") : ""}`,
      value: repo.full_name,
    })),
  );

  const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
    serversStore.servers.map((server) => ({
      label: `${server.name} · ${server.ip}`,
      value: server.id,
    })),
  );

  /** selectedProviderName resolves the provider slug for the create payload. */
  const selectedProviderName = computed<string>(() => {
    if (form.sourceType === "git_public") {
      return "public";
    }
    if (form.sourceType === "git_private") {
      return "";
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
    // Dockerfile, Compose and image sources render a not-yet-available
    // placeholder until GS-7..GS-9, so the step cannot continue on them.
    if (!sourceTypeImplemented(form.sourceType)) {
      return false;
    }
    switch (form.sourceType) {
      case "git_public":
        if (!cloneUrlSchema.safeParse(form.publicCloneUrl).success) {
          return false;
        }
        break;
      case "git_private":
        if (!cloneUrlSchema.safeParse(form.privateCloneUrl).success) {
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
    return (
      branchSchema.safeParse(form.branch).success &&
      appNameSchema.safeParse(form.name).success
    );
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
      form.sourceType === "git_public"
        ? form.publicCloneUrl.trim()
        : form.sourceType === "git_private"
          ? form.privateCloneUrl.trim()
          : form.repoFullName;
    return `${repo} · ${form.branch.trim()}`;
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
      if (providerId !== "" && providerId !== PUBLIC_PROVIDER) {
        void loadRepos();
      }
    },
  );

  /** loadRepos fetches the selected provider's repositories; the store exposes any error. */
  async function loadRepos(): Promise<void> {
    if (form.providerId === "" || form.providerId === PUBLIC_PROVIDER) {
      return;
    }
    await providersStore.fetchRepos(form.providerId).catch(() => undefined);
  }

  /** handleRepoSelect prefills branch, clone URL and a name from the repo. */
  function handleRepoSelect(fullName: string): void {
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
  }

  /** buildPayload assembles the create-application body from the wizard state. */
  function buildPayload(): CreateApplicationInput {
    let repo = "";
    let cloneUrl = "";
    switch (form.sourceType) {
      case "git_public":
        repo = form.publicCloneUrl.trim();
        cloneUrl = form.publicCloneUrl.trim();
        break;
      case "git_private":
        repo = form.privateCloneUrl.trim();
        cloneUrl = form.privateCloneUrl.trim();
        break;
      default:
        repo = form.repoFullName;
        cloneUrl = form.cloneUrl;
        break;
    }
    return {
      name: form.name.trim(),
      environment_id: form.environmentId,
      provider: selectedProviderName.value,
      repo,
      clone_url: cloneUrl,
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
    form.privateCloneUrl = "";
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
    providerOptions,
    sourceTypeOptions,
    isPublicRepo,
    isPrivateRepo,
    isProviderFlow,
    repoOptions,
    serverOptions,
    sourceValid,
    runtimeValid,
    scopeValid,
    envKeyWarnings,
    droppedEnvRows,
    canContinue,
    buildPackLabel,
    reviewSource,
    loadRepos,
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

