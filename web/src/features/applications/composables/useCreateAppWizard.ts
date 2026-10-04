import { useMessage } from "naive-ui";
import { computed, inject, provide, reactive, ref, watch, type InjectionKey, type Ref } from "vue";

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
import {
  countDroppedEnvRows,
  hasEnvKeyWarnings,
} from "@/features/applications/utils/wizardValidation";

export interface WizardForm {
  providerId: string;
  publicCloneUrl: string;
  repoFullName: string;
  cloneUrl: string;
  branch: string;
  name: string;
  buildPack: BuildPack;
  serverId: string;
  port: number | null;
  hostPort: number | null;
  baseDomain: string;
  env: EnvVar[];
  storage: StorageMapping[];
}

export const PUBLIC_PROVIDER = "public";

export const STEP_NAMES = ["Source", "Build pack", "Runtime", "Env & storage", "Deploy"];

const NAME_PATTERN = /^[a-z][a-z0-9-]{2,30}$/;
const DOMAIN_PATTERN = /^[a-z0-9.-]+\.[a-z]{2,}$/;

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

/**
 * Form state, validation and submission behind the create-application wizard.
 * The shell provides the returned state to the step components through
 * `createWizardKey`, so steps read one typed source instead of long prop lists.
 */
export function useCreateAppWizard(show: Ref<boolean>, emit: WizardEvents) {
  const providersStore = useProvidersStore();
  const serversStore = useServersStore();
  const appsStore = useApplicationsStore();
  const message = useMessage();

  const step = ref(0);
  const submitting = ref(false);
  const errorMessage = ref("");
  const sourceError = ref("");

  const form = reactive<WizardForm>({
    providerId: "",
    publicCloneUrl: "",
    repoFullName: "",
    cloneUrl: "",
    branch: "main",
    name: "",
    buildPack: "",
    serverId: "",
    port: 3000,
    hostPort: null,
    baseDomain: "",
    env: [{ key: "NODE_ENV", value: "production" }],
    storage: [],
  });

  const providerOptions = computed<Array<{ label: string; value: string }>>(() => [
    ...providersStore.providers.map((item) => ({
      label: `${item.provider} · ${item.connected ? "connected" : "not connected"}`,
      value: item.id,
    })),
    { label: "Public repository · paste URL", value: PUBLIC_PROVIDER },
  ]);

  const isPublicRepo = computed<boolean>(() => form.providerId === PUBLIC_PROVIDER);

  const repoOptions = computed<Array<{ label: string; value: string }>>(() =>
    providersStore.reposOf(form.providerId).map((repo) => ({
      label: `${repo.full_name}${repo.private ? " (private)" : ""}`,
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
    if (isPublicRepo.value) {
      return "public";
    }
    return (
      providersStore.providers.find((item) => item.id === form.providerId)
        ?.provider ?? ""
    );
  });

  /** sourceValid gates the Source step: provider, repo (or URL), branch, name. */
  const sourceValid = computed<boolean>(() => {
    if (form.providerId === "") {
      return false;
    }
    if (isPublicRepo.value) {
      if (form.publicCloneUrl.trim() === "") {
        return false;
      }
    } else {
      if (form.repoFullName === "") {
        return false;
      }
      // A private repository without a provider ssh_url is rejected rather than
      // silently degraded to https: the keyed cloner would rewrite that URL and
      // drop a self-hosted SSH port. sourceError names the gap.
      if (form.cloneUrl.trim() === "") {
        return false;
      }
    }
    return form.branch.trim() !== "" && NAME_PATTERN.test(form.name.trim());
  });

  /** runtimeValid gates the Runtime step: a node and a valid port. */
  const runtimeValid = computed<boolean>(() => {
    if (form.serverId === "") {
      return false;
    }
    if (form.port === null || !Number.isInteger(form.port) || form.port < 1 || form.port > 65535) {
      return false;
    }
    if (
      form.hostPort !== null &&
      (!Number.isInteger(form.hostPort) || form.hostPort < 0 || form.hostPort > 65535)
    ) {
      return false;
    }
    const domain = form.baseDomain.trim();
    return domain === "" || DOMAIN_PATTERN.test(domain);
  });

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
    () => BUILD_PACKS.find((item) => item.value === form.buildPack)?.label ?? "Auto-detect",
  );

  /** reviewSource renders the repo headline on the review step. */
  const reviewSource = computed<string>(() => {
    const repo = isPublicRepo.value ? form.publicCloneUrl.trim() : form.repoFullName;
    return `${repo} · ${form.branch.trim()}`;
  });

  // Entering the wizard loads providers and nodes; changing provider loads repos.
  watch(
    show,
    (visible) => {
      if (visible) {
        void providersStore.fetchProviders().catch(() => undefined);
        void serversStore.fetchServers().catch(() => undefined);
      } else {
        resetWizard();
      }
    },
  );

  watch(
    () => form.providerId,
    (providerId) => {
      form.repoFullName = "";
      form.cloneUrl = "";
      sourceError.value = "";
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
    sourceError.value = "";
    if (repo.private && form.cloneUrl === "") {
      sourceError.value =
        "The provider reported no SSH URL for this private repository, so a deploy key cannot be used. Pick another repository or make the SSH URL available on the provider.";
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
    return {
      name: form.name.trim(),
      provider: selectedProviderName.value,
      repo: isPublicRepo.value ? form.publicCloneUrl.trim() : form.repoFullName,
      clone_url: isPublicRepo.value ? form.publicCloneUrl.trim() : form.cloneUrl,
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
    errorMessage.value = "";
    submitting.value = true;
    try {
      const { application, webhook } = await createApplication(buildPayload());
      // The create route only stores the row; "Create & deploy" must queue the
      // first deployment explicitly and surface whether it was queued.
      try {
        await appsStore.deploy(application.id);
        message.success(`Application "${application.name}" created and first deploy queued`);
      } catch (deployError) {
        message.warning(
          `Application "${application.name}" was created, but the first deploy could not be queued: ${describeApplicationError(
            deployError,
          )}`,
          { duration: 8000 },
        );
      }
      if (webhook && !webhook.installed) {
        message.warning(
          `Automatic deploys are off: ${
            webhook.error ?? "the provider hook could not be installed"
          }`,
          { duration: 8000 },
        );
      }
      emit("created", application);
      emit("update:show", false);
      resetWizard();
    } catch (error) {
      errorMessage.value = describeApplicationError(error);
    } finally {
      submitting.value = false;
    }
  }

  /** closeWizard closes the modal and resets the wizard state. */
  function closeWizard(): void {
    emit("update:show", false);
    resetWizard();
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
    form.providerId = "";
    form.publicCloneUrl = "";
    form.repoFullName = "";
    form.cloneUrl = "";
    form.branch = "main";
    form.name = "";
    form.buildPack = "";
    form.serverId = "";
    form.port = 3000;
    form.hostPort = null;
    form.baseDomain = "";
    form.env = [{ key: "NODE_ENV", value: "production" }];
    form.storage = [];
    errorMessage.value = "";
    sourceError.value = "";
    submitting.value = false;
    // The provider store is a singleton: a stale repo error must not survive
    // into the next wizard with a Retry that no longer applies.
    providersStore.reposError = null;
  }

  return {
    form,
    step,
    stepNames: STEP_NAMES,
    buildPacks: BUILD_PACKS,
    submitting,
    errorMessage,
    sourceError,
    providersStore,
    providerOptions,
    isPublicRepo,
    repoOptions,
    serverOptions,
    sourceValid,
    runtimeValid,
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

