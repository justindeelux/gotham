import { useMessage } from "naive-ui";
import { computed, inject, provide, reactive, ref, toValue, watch, type InjectionKey, type Ref } from "vue";

import {
  createApplication,
  createDeployKey,
  deleteApplication,
  describeApplicationError,
  setGitCredential,
  testConnection,
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
import { describeToolchainError, useToolchainStore } from "@/features/toolchain";
import type { ToolchainTool } from "@/features/toolchain";
import { cloneUrlFor, suggestAppName } from "@/features/applications/utils/wizardSource";
import { activeLocale, i18n } from "@/shared/i18n";
import {
  countDroppedEnvRows,
  hasEnvKeyWarnings,
} from "@/features/applications/schemas/env";
import {
  appNameSchema,
  branchSchema,
  buildArgKeySchema,
  buildArgValueSchema,
  cloneUrlSchema,
  dockerfileContentSchema,
  composeContentSchema,
  composeFileSchema,
  composeModeSchema,
  composeServiceSchema,
  imageRefSchema,
  isHttpsGitUrl,
  privateCloneUrlSchema,
  providerSchema,
  publicCloneUrlSchema,
  repoSchema,
  serverSchema,
  sourceTypeImplemented,
  sourceTypeSchema,
  type ComposeMode,
  type SourceType,
} from "@/features/applications/schemas/applications";
import type { BuildArgRow } from "@/features/applications/utils/buildArgs";
import { extractComposeServiceNames } from "@/features/applications/utils/compose";

export interface WizardForm {
  sourceType: SourceType;
  providerId: string;
  publicCloneUrl: string;
  privateCloneUrl: string;
  privateAuth: "ssh" | "https";
  httpsUsername: string;
  httpsToken: string;
  repoFullName: string;
  cloneUrl: string;
  dockerfileContent: string;
  buildArgs: BuildArgRow[];
  composeMode: ComposeMode;
  composeContent: string;
  composeFile: string;
  composeService: string;
  branch: string;
  imageRef: string;
  registryUsername: string;
  registryPassword: string;
  name: string;
  buildPack: BuildPack;
  projectId: string;
  environmentId: string;
  serverId: string;
  env: EnvVar[];
  storage: StorageMapping[];
}

export const STEP_NAMES = ["Source", "Runtime", "Build pack", "Env & storage", "Deploy"];

export interface BuildPackOption {
  value: BuildPack;
  label: string;
  hint: string;
}

export const BUILD_PACK_TOOLS: Record<string, string[]> = {
  railpack: ["docker", "buildkit", "railpack"],
  buildpacks: ["pack"],
};

export interface BuildPackChoice extends BuildPackOption {
  tools: string[];
  installed: boolean;
  selectable: boolean;
  missing: string | null;
  installing: boolean;
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

export function buildArgsValid(rows: BuildArgRow[]): boolean {
  if (rows.length > 64) {
    return false;
  }
  return rows
    .filter((row) => row.key.trim() !== "" || row.value !== "")
    .every(
      (row) =>
        buildArgKeySchema.safeParse(row.key).success &&
        buildArgValueSchema.safeParse(row.value).success,
    );
}

export function buildArgsPayload(
  rows: BuildArgRow[],
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key !== "") {
      out[key] = row.value;
    }
  }
  return out;
}

export interface WizardEvents {
  (_event: "update:show", _value: boolean): void;
  (_event: "created", _application: Application): void;
}

export interface WizardScope {
  projectId: string | Ref<string>;
  environmentId: string | Ref<string>;
  sourceType?: string | Ref<string>;
}

export function useCreateAppWizard(
  show: Ref<boolean>,
  emit: WizardEvents,
  scope: WizardScope = { projectId: "", environmentId: "" },
) {
  const providersStore = useProvidersStore();
  const githubAppStore = useGitHubAppStore();
  const serversStore = useServersStore();
  const appsStore = useApplicationsStore();
  const toolchainStore = useToolchainStore();
  const message = useMessage();

  const step = ref(0);
  const submitting = ref(false);
  const submitFailure = ref<{ kind: "scope" } | { kind: "error"; error: unknown } | null>(null);
  const noSshUrl = ref(false);

  const createdKey = ref<{ application: Application; publicKey: string } | null>(null);
  const keyRecovery = ref<{ application: Application } | null>(null);
  const keyTesting = ref(false);
  const keyTestPassed = ref(false);
  const keyTestMessage = ref("");
  const keyConfirmed = ref(false);
  const keyDeploying = ref(false);

  const canDeployCreated = computed<boolean>(
    () => createdKey.value !== null && (keyTestPassed.value || keyConfirmed.value),
  );

  function tr(key: string, params?: Record<string, string | number>): string {
    void activeLocale.value;
    return String(i18n.global.t(key, params ?? {}));
  }

  const errorMessage = computed<string>(() => {
    if (submitFailure.value === null) {
      return "";
    }
    if (submitFailure.value.kind === "scope") {
      return tr("applications.wizard.scopeError");
    }
    return describeApplicationError(submitFailure.value.error);
  });

  const sourceError = computed<string>(() =>
    noSshUrl.value ? tr("applications.wizard.noSshUrl") : "",
  );

  const stepNames = computed<string[]>(() => [
    tr("applications.wizard.steps.source"),
    tr("applications.wizard.steps.runtime"),
    tr("applications.wizard.steps.buildPack"),
    tr("applications.wizard.steps.envStorage"),
    tr("applications.wizard.steps.deploy"),
  ]);

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

  const nodeTools = computed<Map<string, ToolchainTool>>(() => {
    const map = new Map<string, ToolchainTool>();
    if (toolchainStore.serverId !== form.serverId || form.serverId === "") {
      return map;
    }
    for (const tool of toolchainStore.tools) {
      map.set(tool.id, tool);
    }
    return map;
  });

  function toolInstalled(toolId: string): boolean | undefined {
    const tool = nodeTools.value.get(toolId);
    return tool ? tool.installed : undefined;
  }

  function engineInstalled(tools: string[]): boolean | undefined {
    let unknown = false;
    for (const toolId of tools) {
      const state = toolInstalled(toolId);
      if (state === undefined) {
        unknown = true;
      } else if (!state) {
        return false;
      }
    }
    return unknown ? undefined : true;
  }

  const cliEngineInstalled = computed<boolean | undefined>(() => {
    const states = [
      engineInstalled(BUILD_PACK_TOOLS.railpack),
      engineInstalled(BUILD_PACK_TOOLS.buildpacks),
    ];
    if (states.some((state) => state === true)) {
      return true;
    }
    return states.some((state) => state === undefined) ? undefined : false;
  });

  const noBuildEngine = computed<boolean>(() => cliEngineInstalled.value === false);

  const nodeToolsLoading = computed<boolean>(
    () =>
      form.serverId !== "" &&
      toolchainStore.serverId === form.serverId &&
      toolchainStore.loading,
  );

  const nodeToolsError = computed<boolean>(
    () =>
      form.serverId !== "" &&
      toolchainStore.serverId === form.serverId &&
      toolchainStore.failure !== null,
  );

  const buildPackChoices = computed<BuildPackChoice[]>(() =>
    buildPacks.value.map((pack) => {
      const tools = BUILD_PACK_TOOLS[pack.value] ?? [];
      if (tools.length === 0) {
        const selectable =
          pack.value === "" ? cliEngineInstalled.value !== false : true;
        return { ...pack, tools, installed: selectable, selectable, missing: null, installing: false };
      }
      const installed = engineInstalled(tools);
      const installing = tools.some((toolId) =>
        toolchainStore.activeJobs.some(
          (job) => job.tool_id === toolId && job.action === "install",
        ),
      );
      return {
        ...pack,
        tools,
        installed: installed === true,
        selectable: installed !== false,
        missing: tools.find((toolId) => toolInstalled(toolId) === false) ?? null,
        installing,
      };
    }),
  );

  const buildPackValid = computed<boolean>(() => {
    if (buildPackSkipped.value || isCompose.value) {
      return true;
    }
    const choice = buildPackChoices.value.find((item) => item.value === form.buildPack);
    return choice ? choice.selectable : true;
  });

  const preselectedSourceType = computed<string>(() => {
    const raw = toValue(scope.sourceType ?? "");
    return sourceTypeSchema.safeParse(raw).success ? raw : "";
  });

  const sourceTypeLocked = computed<boolean>(() => preselectedSourceType.value !== "");

  function seedSource(): void {
    form.sourceType =
      preselectedSourceType.value === ""
        ? "git_public"
        : (preselectedSourceType.value as SourceType);
  }

  const form = reactive<WizardForm>({
    sourceType: "git_public",
    providerId: "",
    publicCloneUrl: "",
    privateCloneUrl: "",
    privateAuth: "ssh",
    httpsUsername: "",
    httpsToken: "",
    repoFullName: "",
    cloneUrl: "",
    dockerfileContent: "",
    buildArgs: [],
    composeMode: "paste",
    composeContent: "",
    composeFile: "docker-compose.yml",
    composeService: "",
    branch: "main",
    imageRef: "",
    registryUsername: "",
    registryPassword: "",
    name: "",
    buildPack: "",
    projectId: toValue(scope.projectId),
    environmentId: toValue(scope.environmentId),
    serverId: "",
    env: [{ key: "NODE_ENV", value: "production" }],
    storage: [],
  });
  seedSource();

  const providerOptions = computed<Array<{ label: string; value: string }>>(() => {
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

  const isPrivateRepo = computed<boolean>(() => form.sourceType === "git_private");

  const isProviderFlow = computed<boolean>(
    () => form.sourceType === "github_app" || form.sourceType === "gitlab_app",
  );

  const isGitHubAppFlow = computed<boolean>(() => form.sourceType === "github_app");

  const isDockerfile = computed<boolean>(() => form.sourceType === "dockerfile");

  const isImage = computed<boolean>(() => form.sourceType === "image");

  const isCompose = computed<boolean>(() => form.sourceType === "compose");

  const isComposePaste = computed<boolean>(
    () => isCompose.value && form.composeMode === "paste",
  );

  const sourceTypeOptions = computed<Array<{ label: string; value: string; disabled?: boolean }>>(() => [
    { label: tr("applications.wizard.sourceGitPublic"), value: "git_public" },
    { label: tr("applications.wizard.sourceGitPrivate"), value: "git_private" },
    { label: tr("applications.wizard.sourceGithubApp"), value: "github_app" },
    { label: tr("applications.wizard.sourceGitlabApp"), value: "gitlab_app" },
    { label: tr("applications.wizard.sourceDockerfile"), value: "dockerfile" },
    { label: tr("applications.wizard.sourceImage"), value: "image" },
    { label: tr("applications.wizard.sourceCompose"), value: "compose" },
  ]);

  const lockedSourceLabel = computed<string>(
    () =>
      sourceTypeOptions.value.find((item) => item.value === form.sourceType)?.label ??
      form.sourceType,
  );

  const composeModeOptions = computed<Array<{ label: string; value: string }>>(() => [
    { label: tr("applications.wizard.composeModePaste"), value: "paste" },
    { label: tr("applications.wizard.composeModeRepo"), value: "repo" },
  ]);

  const composeServiceOptions = computed<Array<{ label: string; value: string }>>(() =>
    extractComposeServiceNames(form.composeContent).map((name) => ({
      label: name,
      value: name,
    })),
  );

  const repoOptions = computed<Array<{ label: string; value: string }>>(() => {
    const repos = isGitHubAppFlow.value
      ? githubAppStore.reposOf(form.providerId)
      : providersStore.reposOf(form.providerId);
    return repos.map((repo) => ({
      label: `${repo.full_name}${repo.private ? tr("applications.wizard.privateSuffix") : ""}`,
      value: repo.full_name,
    }));
  });

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

  const reposTruncated = computed<boolean>(
    () => isGitHubAppFlow.value && githubAppStore.reposTruncated(form.providerId),
  );

  const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
    serversStore.servers.map((server) => ({
      label: `${server.name} · ${server.ip}`,
      value: server.id,
    })),
  );

  const selectedProviderName = computed<string>(() => {
    if (!isProviderFlow.value) {
      return "";
    }
    if (isGitHubAppFlow.value) {
      return "github";
    }
    return (
      providersStore.providers.find((item) => item.id === form.providerId)
        ?.provider ?? ""
    );
  });

  function providerSlugOf(providerId: string): string {
    return (
      providersStore.providers.find((item) => item.id === providerId)
        ?.provider ?? ""
    );
  }

  const sourceValid = computed<boolean>(() => {
    if (!sourceTypeSchema.safeParse(form.sourceType).success) {
      return false;
    }
    if (!sourceTypeImplemented(form.sourceType)) {
      return false;
    }
    switch (form.sourceType) {
      case "git_public":
        if (!publicCloneUrlSchema.safeParse(form.publicCloneUrl).success) {
          return false;
        }
        break;
      case "git_private": {
        if (!privateCloneUrlSchema.safeParse(form.privateCloneUrl).success) {
          return false;
        }
        if (form.privateAuth === "https") {
          if (!isHttpsGitUrl(form.privateCloneUrl)) {
            return false;
          }
          if (form.httpsToken.trim() === "") {
            return false;
          }
        }
        break;
      }
      case "dockerfile":
        if (!dockerfileContentSchema.safeParse(form.dockerfileContent).success) {
          return false;
        }
        if (!buildArgsValid(form.buildArgs)) {
          return false;
        }
        break;
      case "image":
        if (!imageRefSchema.safeParse(form.imageRef).success) {
          return false;
        }
        if (
          (form.registryUsername.trim() === "") !==
          (form.registryPassword.trim() === "")
        ) {
          return false;
        }
        break;
      case "compose":
        if (!composeModeSchema.safeParse(form.composeMode).success) {
          return false;
        }
        if (isComposePaste.value) {
          if (!composeContentSchema.safeParse(form.composeContent).success) {
            return false;
          }
        } else {
          if (form.providerId !== "") {
            if (!providerSchema.safeParse(form.providerId).success) {
              return false;
            }
            if (!repoSchema.safeParse(form.repoFullName).success) {
              return false;
            }
            if (!cloneUrlSchema.safeParse(form.cloneUrl).success) {
              return false;
            }
          } else if (!publicCloneUrlSchema.safeParse(form.publicCloneUrl).success) {
            return false;
          }
          if (!composeFileSchema.safeParse(form.composeFile).success) {
            return false;
          }
        }
        if (!composeServiceSchema.safeParse(form.composeService).success) {
          return false;
        }
        break;
      default: {
        if (!providerSchema.safeParse(form.providerId).success) {
          return false;
        }
        if (!repoSchema.safeParse(form.repoFullName).success) {
          return false;
        }
        if (!cloneUrlSchema.safeParse(form.cloneUrl).success) {
          return false;
        }
        break;
      }
    }
    if (form.sourceType === "image") {
      return appNameSchema.safeParse(form.name).success;
    }
    const branchOk =
      form.sourceType === "dockerfile" ||
      ((form.sourceType === "git_public" || form.sourceType === "git_private") &&
        form.branch.trim() === "")
        ? true
        : isComposePaste.value
          ? true
          : branchSchema.safeParse(form.branch).success;
    return branchOk && appNameSchema.safeParse(form.name).success;
  });

  const runtimeValid = computed<boolean>(() =>
    serverSchema.safeParse(form.serverId).success,
  );

  const scopeValid = computed<boolean>(() => form.environmentId !== "");

  const envKeyWarnings = computed<boolean>(() => hasEnvKeyWarnings(form.env));

  const droppedEnvRows = computed<number>(() => countDroppedEnvRows(form.env));

  const canContinue = computed<boolean>(() => {
    switch (step.value) {
      case 0:
        return sourceValid.value;
      case 1:
        return runtimeValid.value;
      case 2:
        return buildPackValid.value;
      default:
        return true;
    }
  });

  const buildPackLabel = computed<string>(() => {
    if (form.sourceType === "dockerfile") {
      return tr("applications.wizard.sourceDockerfile");
    }
    if (form.sourceType === "image") {
      return tr("applications.wizard.packImageNone");
    }
    return buildPacks.value.find((item) => item.value === form.buildPack)?.label ?? tr("applications.wizard.packAuto");
  });

  const buildPackSkipped = computed<boolean>(
    () => form.sourceType === "dockerfile" || form.sourceType === "image",
  );

  function nextStep(): void {
    step.value += 1;
    if (step.value === 2 && buildPackSkipped.value) {
      step.value += 1;
    }
  }

  function prevStep(): void {
    step.value -= 1;
    if (step.value === 2 && buildPackSkipped.value) {
      step.value -= 1;
    }
  }

  const stepPosition = computed<number>(() =>
    buildPackSkipped.value && step.value > 1 ? step.value : step.value + 1,
  );

  const visibleStepNames = computed<string[]>(() =>
    buildPackSkipped.value
      ? [stepNames.value[0], stepNames.value[1], stepNames.value[3], stepNames.value[4]]
      : stepNames.value,
  );

  const visibleStepIndex = computed<number>(() =>
    buildPackSkipped.value && step.value > 1 ? step.value - 1 : step.value,
  );

  const stepTotal = computed<number>(() => (buildPackSkipped.value ? 4 : 5));

  const reviewSource = computed<string>(() => {
    if (form.sourceType === "dockerfile") {
      const count = Object.keys(buildArgsPayload(form.buildArgs)).length;
      return String(
        i18n.global.t("applications.wizard.reviewDockerfileArgs", { count }, count),
      );
    }
    if (form.sourceType === "image") {
      return form.imageRef.trim();
    }
    if (form.sourceType === "compose") {
      const detail =
        form.composeMode === "paste"
          ? form.composeService.trim()
          : `${form.composeService.trim()} · ${form.composeFile.trim()}`;
      return `${tr("applications.wizard.sourceCompose")} · ${detail}`;
    }
    const repo =
      form.sourceType === "git_public"
        ? form.publicCloneUrl.trim()
        : form.sourceType === "git_private"
          ? form.privateCloneUrl.trim()
          : form.repoFullName;
    const branch = form.branch.trim();
    return `${repo} · ${branch === "" ? tr("applications.wizard.branchDefault") : branch}`;
  });

  watch(
    show,
    (visible) => {
      if (visible) {
        seedScope();
        seedSource();
        void providersStore.fetchProviders().catch(() => undefined);
        if (form.sourceType === "github_app") {
          void githubAppStore.fetchApps().catch(() => undefined);
        }
        void serversStore.fetchServers().catch(() => undefined);
      } else {
        resetWizard();
      }
    },
  );

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

  watch(
    () => form.sourceType,
    () => {
      form.providerId = "";
      form.publicCloneUrl = "";
      form.privateCloneUrl = "";
      form.privateAuth = "ssh";
      form.httpsUsername = "";
      form.httpsToken = "";
      form.repoFullName = "";
      form.cloneUrl = "";
      form.dockerfileContent = "";
      form.buildArgs = [];
      form.imageRef = "";
      form.registryUsername = "";
      form.registryPassword = "";
      form.composeMode = "paste";
      form.composeContent = "";
      form.composeFile = "docker-compose.yml";
      form.composeService = "";
      noSshUrl.value = false;
      if (form.sourceType === "github_app") {
        void githubAppStore.fetchApps().catch(() => undefined);
      }
    },
  );

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

  async function loadBranches(): Promise<void> {
    if (form.providerId === "" || form.repoFullName === "") {
      return;
    }
    await providersStore
      .fetchBranches(form.providerId, form.repoFullName)
      .catch(() => undefined);
  }

  function subscribeNodeTools(): void {
    const id = form.serverId;
    if (id === "") {
      return;
    }
    const status = serversStore.servers.find((item) => item.id === id)?.status;
    if (status !== "ready") {
      return;
    }
    if (toolchainStore.serverId === id) {
      void toolchainStore.refresh().catch(() => undefined);
      return;
    }
    void toolchainStore.subscribe(id).catch(() => undefined);
  }

  async function installBuildPack(value: BuildPack): Promise<void> {
    const tools = BUILD_PACK_TOOLS[value] ?? [];
    if (tools.length === 0 || form.serverId === "") {
      return;
    }
    try {
      await toolchainStore.enqueue(tools, "install");
      toolchainStore.openQueueModal();
    } catch (error) {
      message.warning(describeToolchainError(error), { duration: 8000 });
    }
  }

  async function retryNodeTools(): Promise<void> {
    if (form.serverId === "") {
      return;
    }
    try {
      await toolchainStore.refresh();
    } catch {
    }
  }

  function disconnectNodeTools(): void {
    toolchainStore.disconnect();
    toolchainStore.serverId = "";
  }

  function handleRepoSelect(fullName: string): void {
    if (isGitHubAppFlow.value) {
      const repo = githubAppStore.reposOf(form.providerId).find((item) => item.full_name === fullName);
      if (!repo) {
        return;
      }
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

  function buildPayload(): CreateApplicationInput {
    if (form.sourceType === "image") {
      return {
        name: form.name.trim(),
        environment_id: form.environmentId,
        provider: "",
        repo: "",
        clone_url: "",
        source_type: form.sourceType,
        branch: "",
        build_pack: "",
        image_ref: form.imageRef.trim(),
        registry_username: form.registryUsername.trim(),
        registry_password: form.registryPassword,
        server_id: form.serverId,
        env: form.env.filter((row) => row.key.trim() !== ""),
        storage: form.storage.filter((row) => row.name.trim() !== ""),
      };
    }
    if (form.sourceType === "compose") {
      const repoSource =
        form.providerId !== ""
          ? { provider: providerSlugOf(form.providerId), repo: form.repoFullName, cloneUrl: form.cloneUrl }
          : { provider: "", repo: form.publicCloneUrl.trim(), cloneUrl: form.publicCloneUrl.trim() };
      const pasted = form.composeMode === "paste";
      return {
        name: form.name.trim(),
        environment_id: form.environmentId,
        provider: pasted ? "" : repoSource.provider,
        repo: pasted ? "" : repoSource.repo,
        clone_url: pasted ? "" : repoSource.cloneUrl,
        source_type: form.sourceType,
        ...(pasted
          ? { compose_content: form.composeContent }
          : { compose_file: form.composeFile.trim() }),
        compose_service: form.composeService.trim(),
        branch: pasted ? "" : form.branch.trim(),
        build_pack: "",
        image_ref: "",
        server_id: form.serverId,
        env: form.env.filter((row) => row.key.trim() !== ""),
        storage: form.storage.filter((row) => row.name.trim() !== ""),
      };
    }
    const source =
      form.sourceType === "git_public"
        ? { repo: form.publicCloneUrl.trim(), cloneUrl: form.publicCloneUrl.trim() }
        : form.sourceType === "git_private"
          ? { repo: form.privateCloneUrl.trim(), cloneUrl: form.privateCloneUrl.trim() }
          : form.sourceType === "dockerfile"
            ? { repo: "", cloneUrl: "" }
            : { repo: form.repoFullName, cloneUrl: form.cloneUrl };
    return {
      name: form.name.trim(),
      environment_id: form.environmentId,
      provider: selectedProviderName.value,
      repo: source.repo,
      clone_url: source.cloneUrl,
      source_type: form.sourceType,
      github_app_id: isGitHubAppFlow.value ? form.providerId : undefined,
      dockerfile_content: form.sourceType === "dockerfile" ? form.dockerfileContent : undefined,
      build_args: form.sourceType === "dockerfile" ? buildArgsPayload(form.buildArgs) : undefined,
      branch: form.branch.trim(),
      build_pack: form.sourceType === "dockerfile" ? "" : form.buildPack,
      image_ref: "",
      server_id: form.serverId,
      env: form.env.filter((row) => row.key.trim() !== ""),
      storage: form.storage.filter((row) => row.name.trim() !== ""),
    };
  }

  async function handleSubmit(): Promise<void> {
    submitFailure.value = null;
    if (!scopeValid.value) {
      submitFailure.value = { kind: "scope" };
      return;
    }
    submitting.value = true;
    try {
      const { application, webhook } = await createApplication(buildPayload());
      if (form.sourceType === "git_private" && form.privateAuth === "ssh") {
        try {
          const key = await createDeployKey(application.id);
          createdKey.value = { application, publicKey: key.public_key };
        } catch (credError) {
          submitFailure.value = { kind: "error", error: credError };
          keyRecovery.value = { application };
        } finally {
          submitting.value = false;
        }
        return;
      }
      if (form.sourceType === "git_private") {
        try {
          await setGitCredential(application.id, form.httpsUsername.trim(), form.httpsToken);
        } catch (credError) {
          message.warning(
            tr("applications.privateGit.credentialFailed", {
              error: describeApplicationError(credError),
            }),
            { duration: 8000 },
          );
        }
      }
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

  function closeWizard(): void {
    emit("update:show", false);
    resetWizard();
  }

  async function runCreatedKeyTest(): Promise<void> {    if (!createdKey.value) {
      return;
    }
    keyTesting.value = true;
    keyTestMessage.value = "";
    try {
      const result = await testConnection(createdKey.value.application.id);
      keyTestPassed.value = result.ok;
      keyTestMessage.value = result.ok
        ? tr("applications.privateGit.connected")
        : result.message;
    } catch (error) {
      keyTestPassed.value = false;
      keyTestMessage.value = describeApplicationError(error);
    } finally {
      keyTesting.value = false;
    }
  }

  async function deployCreatedKey(): Promise<void> {
    if (!createdKey.value) {
      return;
    }
    keyDeploying.value = true;
    try {
      await appsStore.deploy(createdKey.value.application.id);
      message.success(
        tr("applications.wizard.createdQueued", { name: createdKey.value.application.name }),
      );
    } catch (deployError) {
      message.warning(
        tr("applications.wizard.createdDeployFailed", {
          name: createdKey.value.application.name,
          error: describeApplicationError(deployError),
        }),
        { duration: 8000 },
      );
    } finally {
      keyDeploying.value = false;
    }
    const created = createdKey.value.application;
    resetWizard();
    emit("created", created);
    emit("update:show", false);
  }

  function closeCreatedKey(): void {
    if (!createdKey.value) {
      return;
    }
    const created = createdKey.value.application;
    resetWizard();
    emit("created", created);
    emit("update:show", false);
  }

  async function retryKeyCreation(): Promise<void> {
    if (!keyRecovery.value) {
      return;
    }
    submitting.value = true;
    try {
      const key = await createDeployKey(keyRecovery.value.application.id);
      createdKey.value = {
        application: keyRecovery.value.application,
        publicKey: key.public_key,
      };
      keyRecovery.value = null;
      submitFailure.value = null;
    } catch (credError) {
      submitFailure.value = { kind: "error", error: credError };
    } finally {
      submitting.value = false;
    }
  }

  async function deleteRecoveryApp(): Promise<void> {
    if (!keyRecovery.value) {
      return;
    }
    submitting.value = true;
    try {
      await deleteApplication(keyRecovery.value.application.id);
    } catch (error) {
      submitFailure.value = { kind: "error", error };
      submitting.value = false;
      return;
    }
    resetWizard();
  }

  function seedScope(): void {
    form.projectId = toValue(scope.projectId);
    form.environmentId = toValue(scope.environmentId);
  }

  function handleShowChange(value: boolean): void {
    emit("update:show", value);
    if (!value) {
      if (createdKey.value) {
        const created = createdKey.value.application;
        resetWizard();
        emit("created", created);
        return;
      }
      resetWizard();
    }
  }

  function resetWizard(): void {
    step.value = 0;
    seedSource();
    form.providerId = "";
    form.publicCloneUrl = "";
    form.privateCloneUrl = "";
    form.privateAuth = "ssh";
    form.httpsUsername = "";
    form.httpsToken = "";
    form.repoFullName = "";
    form.cloneUrl = "";
    form.dockerfileContent = "";
    form.buildArgs = [];
    form.composeMode = "paste";
    form.composeContent = "";
    form.composeFile = "docker-compose.yml";
    form.composeService = "";
    form.branch = "main";
    form.imageRef = "";
    form.registryUsername = "";
    form.registryPassword = "";
    form.name = "";
    form.buildPack = "";
    seedScope();
    form.serverId = "";
    form.env = [{ key: "NODE_ENV", value: "production" }];
    form.storage = [];
    submitFailure.value = null;
    noSshUrl.value = false;
    submitting.value = false;
    createdKey.value = null;
    keyRecovery.value = null;
    keyTesting.value = false;
    keyTestPassed.value = false;
    keyTestMessage.value = "";
    keyConfirmed.value = false;
    keyDeploying.value = false;
    providersStore.reposError = null;
    githubAppStore.clearErrors();
    providersStore.reposErrorRaw = null;
    toolchainStore.closeQueueModal();
    disconnectNodeTools();
  }

  return {
    form,
    step,
    stepNames,
    buildPacks,
    buildPackChoices,
    buildPackValid,
    noBuildEngine,
    nodeToolsLoading,
    nodeToolsError,
    installBuildPack,
    retryNodeTools,
    subscribeNodeTools,
    submitting,
    errorMessage,
    sourceError,
    providersStore,
    githubAppStore,
    providerOptions,
    sourceTypeOptions,
    sourceTypeLocked,
    lockedSourceLabel,
    composeModeOptions,
    composeServiceOptions,
    isPublicRepo,
    isPrivateRepo,
    isProviderFlow,
    isGitHubAppFlow,
    isDockerfile,
    isImage,
    isCompose,
    isComposePaste,
    repoOptions,
    branchOptions,
    reposTruncated,
    serverOptions,
    sourceValid,
    runtimeValid,
    scopeValid,
    envKeyWarnings,
    droppedEnvRows,
    canContinue,
    buildPackLabel,
    buildPackSkipped,
    stepPosition,
    stepTotal,
    visibleStepNames,
    visibleStepIndex,
    nextStep,
    prevStep,
    reviewSource,
    buildPayload,
    loadRepos,
    loadBranches,
    handleRepoSelect,
    handleSubmit,
    closeWizard,
    handleShowChange,
    createdKey,
    keyRecovery,
    keyTesting,
    keyTestPassed,
    keyTestMessage,
    keyConfirmed,
    keyDeploying,
    canDeployCreated,
    runCreatedKeyTest,
    deployCreatedKey,
    closeCreatedKey,
    retryKeyCreation,
    deleteRecoveryApp,
  };
}

export type CreateWizardState = ReturnType<typeof useCreateAppWizard>;

export const createWizardKey: InjectionKey<CreateWizardState> = Symbol("create-wizard");

export function provideCreateWizard(state: CreateWizardState): void {
  provide(createWizardKey, state);
}

export function useCreateWizardState(): CreateWizardState {
  const state = inject(createWizardKey);
  if (!state) {
    throw new Error("useCreateWizardState must be used inside CreateAppWizard");
  }
  return state;
}

