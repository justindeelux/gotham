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
  composeHostPortSchema,
  composeModeSchema,
  composeServiceSchema,
  hostPortSchema,
  imageRefSchema,
  isHttpsGitUrl,
  privateCloneUrlSchema,
  providerSchema,
  publicCloneUrlSchema,
  repoSchema,
  serverSchema,
  sourceTypeImplemented,
  sourceTypeSchema,
  wizardDomainSchema,
  type ComposeMode,
  type SourceType,
} from "@/features/applications/schemas/applications";
import { portSchema } from "@/shared/validation/primitives";
import type { BuildArgRow } from "@/features/applications/utils/buildArgs";
import { extractComposeServiceNames } from "@/features/applications/utils/compose";

export interface WizardForm {
  /** GS-2 source model: which fetcher the orchestrator uses. */
  sourceType: SourceType;
  providerId: string;
  publicCloneUrl: string;
  /** GS-4 provider-less private source: the ssh/scp-like/https clone URL. */
  privateCloneUrl: string;
  /** GS-4 credential for the private URL: a generated deploy key or a token. */
  privateAuth: "ssh" | "https";
  httpsUsername: string;
  httpsToken: string;
  repoFullName: string;
  cloneUrl: string;
  /** Pasted Dockerfile text for the dockerfile source type (GS-7). */
  dockerfileContent: string;
  /** Optional --build-arg pairs for the dockerfile source type. */
  buildArgs: BuildArgRow[];
  /** Compose input mode: pasted document text or a file in a repository (GS-8). */
  composeMode: ComposeMode;
  /** Pasted compose document text for compose/paste sources. */
  composeContent: string;
  /** In-repo compose file path for compose/repo sources. */
  composeFile: string;
  /** Compose service the domain/port routing targets. */
  composeService: string;
  branch: string;
  /** GS-9 prebuilt reference (registry/repo:tag, optionally digest-pinned). */
  imageRef: string;
  /** GS-9 optional private-registry credential (sealed at rest). */
  registryUsername: string;
  registryPassword: string;
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

/** buildArgsValid gates --build-arg drafts: named rows need a valid key and
 * a bounded value; nameless rows are dropped on submit like env rows. */
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

/** buildArgsPayload drops nameless draft rows for the create payload. */
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
   * createdKey holds the SSH post-create state (GS-4): the application row
   * already exists and its deploy key is generated, but the operator has
   * not registered the public half yet, so the first deploy must wait for
   * an explicit Test + Deploy instead of queueing blindly.
   */
  const createdKey = ref<{ application: Application; publicKey: string } | null>(null);
  /**
   * keyRecovery parks the created application when its deploy key could not
   * be generated: retrying the submit would hit a name conflict on the
   * existing row, so the wizard offers key retry or rollback instead.
   */
  const keyRecovery = ref<{ application: Application } | null>(null);
  const keyTesting = ref(false);
  const keyTestPassed = ref(false);
  const keyTestMessage = ref("");
  const keyConfirmed = ref(false);
  const keyDeploying = ref(false);

  /** canDeployCreated gates Deploy on the key step: a passed test or an explicit confirm. */
  const canDeployCreated = computed<boolean>(
    () => createdKey.value !== null && (keyTestPassed.value || keyConfirmed.value),
  );

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

  /** isPrivateRepo covers the provider-less private-git source (GS-4). */
  const isPrivateRepo = computed<boolean>(() => form.sourceType === "git_private");

  /** isProviderFlow covers the connected-provider types (existing flow). */
  const isProviderFlow = computed<boolean>(
    () => form.sourceType === "github_app" || form.sourceType === "gitlab_app",
  );

  /** isGitHubAppFlow covers the GitHub App type (GS-5): repos and branches
   * come from the installation, not the OAuth provider list. */
  const isGitHubAppFlow = computed<boolean>(() => form.sourceType === "github_app");

  /** isDockerfile covers the pasted-Dockerfile type (no repository, GS-7). */
  const isDockerfile = computed<boolean>(() => form.sourceType === "dockerfile");

  /** isImage covers the prebuilt container image source (GS-9). */
  const isImage = computed<boolean>(() => form.sourceType === "image");

  /** isCompose covers the compose source (GS-8). */
  const isCompose = computed<boolean>(() => form.sourceType === "compose");

  /**
   * isComposePaste is true for pasted compose documents: no repository, no
   * branch, no build pack. Repo-backed compose sources keep the repository
   * fields, branch and file path.
   */
  const isComposePaste = computed<boolean>(
    () => isCompose.value && form.composeMode === "paste",
  );

  /** sourceTypeOptions renders the GS-2 type selector in the current locale.
   * Compose is the last enabled source type (GS-8). */
  const sourceTypeOptions = computed<Array<{ label: string; value: string; disabled?: boolean }>>(() => [
    { label: tr("applications.wizard.sourceGitPublic"), value: "git_public" },
    { label: tr("applications.wizard.sourceGitPrivate"), value: "git_private" },
    { label: tr("applications.wizard.sourceGithubApp"), value: "github_app" },
    { label: tr("applications.wizard.sourceGitlabApp"), value: "gitlab_app" },
    { label: tr("applications.wizard.sourceDockerfile"), value: "dockerfile" },
    { label: tr("applications.wizard.sourceImage"), value: "image" },
    { label: tr("applications.wizard.sourceCompose"), value: "compose" },
  ]);

  /** composeModeOptions renders the paste/repo choice in the current locale. */
  const composeModeOptions = computed<Array<{ label: string; value: string }>>(() => [
    { label: tr("applications.wizard.composeModePaste"), value: "paste" },
    { label: tr("applications.wizard.composeModeRepo"), value: "repo" },
  ]);

  /** composeServiceOptions suggests the web service from the pasted text. */
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

  /** branchOptions lists the branches of the selected repository: the   * installation branches for the GitHub App flow, the provider branches
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

  /** reposTruncated flags a partial repository list, so the wizard says so. */
  const reposTruncated = computed<boolean>(
    () => isGitHubAppFlow.value && githubAppStore.reposTruncated(form.providerId),
  );

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

  /** providerSlugOf resolves the slug of any picked provider, including the
   * compose repo mode that reuses the connected-provider flow. */
  function providerSlugOf(providerId: string): string {
    return (
      providersStore.providers.find((item) => item.id === providerId)
        ?.provider ?? ""
    );
  }

  /** sourceValid gates the Source step: the type, its fields, branch, name. */
  const sourceValid = computed<boolean>(() => {
    if (!sourceTypeSchema.safeParse(form.sourceType).success) {
      return false;
    }
    // Not-yet-implemented types render a not-yet-available placeholder, so
    // the step cannot continue. All known types are implemented; this is
    // the fail-closed fallback.
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
      case "git_private": {
        // Provider-less by definition: an ssh/scp-like/https URL with no
        // embedded credentials. The token transport needs an https URL and
        // a token; the deploy key works with any of the shapes.
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
        // No repository: pasted text (FROM required, 64 KiB cap) plus
        // optional build args with non-empty keys.
        if (!dockerfileContentSchema.safeParse(form.dockerfileContent).success) {
          return false;
        }
        if (!buildArgsValid(form.buildArgs)) {
          return false;
        }
        break;
      case "image":
        // Prebuilt reference with optional digest pinning and credential; no
        // branch or build pack (the API rejects them for this type). The
        // credential is both-or-neither, like the server enforces.
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
        // Pasted documents carry no repository; repo-backed ones reuse the
        // connected-provider flow when a provider is picked, else a public
        // URL. Both name the routed web service.
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
    // A git source with no branch pins the remote default at clone time
    // (ls-remote); provider flows keep the required prefilled branch.
    // Dockerfile, image and pasted-compose sources carry no branch at all
    // (the field is hidden).
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

  /** runtimeValid gates the Runtime step: a node and a valid port. */
  const runtimeValid = computed<boolean>(() => {
    if (!serverSchema.safeParse(form.serverId).success) {
      return false;
    }
    if (!portSchema.safeParse(form.port).success) {
      return false;
    }
    // Compose applications publish their web service on the node: a pinned
    // privileged host port is refused (auto-assign or above 1023).
    if (form.sourceType === "compose") {
      if (!composeHostPortSchema.safeParse(form.hostPort).success) {
        return false;
      }
    } else if (!hostPortSchema.safeParse(form.hostPort).success) {
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

  const buildPackLabel = computed<string>(() => {
    // A Dockerfile source always builds with the Dockerfile engine; the
    // review names the source instead of the hidden auto-detect choice.
    if (form.sourceType === "dockerfile") {
      return tr("applications.wizard.sourceDockerfile");
    }
    // Image sources are prebuilt: the review shows that no build runs.
    if (form.sourceType === "image") {
      return tr("applications.wizard.packImageNone");
    }
    return buildPacks.value.find((item) => item.value === form.buildPack)?.label ?? tr("applications.wizard.packAuto");
  });

  /** buildPackSkipped hides the build-pack step for sources without a build. */
  const buildPackSkipped = computed<boolean>(
    () => form.sourceType === "dockerfile" || form.sourceType === "image",
  );

  /** nextStep advances, jumping over the hidden build-pack step. */
  function nextStep(): void {
    step.value += 1;
    if (step.value === 1 && buildPackSkipped.value) {
      step.value += 1;
    }
  }

  /** prevStep goes back, jumping over the hidden build-pack step. */
  function prevStep(): void {
    step.value -= 1;
    if (step.value === 1 && buildPackSkipped.value) {
      step.value -= 1;
    }
  }

  /** stepPosition renders the 1-based position among visited steps. */
  const stepPosition = computed<number>(() =>
    buildPackSkipped.value && step.value > 1 ? step.value : step.value + 1,
  );

  /** stepTotal renders the visited step count (4 without build pack). */
  const stepTotal = computed<number>(() => (buildPackSkipped.value ? 4 : 5));

  /** reviewSource renders the repo headline on the review step. */
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
    if (form.sourceType === "image") {
      // Image sources deploy a prebuilt reference: no provider, repository,
      // branch or build pack. The credential is optional (public images need
      // none) and is sealed at rest.
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
        base_domain: form.baseDomain.trim(),
        port: form.port ?? 3000,
        host_port: form.hostPort ?? 0,
        server_id: form.serverId,
        env: form.env.filter((row) => row.key.trim() !== ""),
        storage: form.storage.filter((row) => row.name.trim() !== ""),
      };
    }
    if (form.sourceType === "compose") {
      // Pasted documents carry no repository, branch or build pack; repo
      // mode reuses the connected-provider flow when a provider is picked,
      // else the public URL.
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
        base_domain: form.baseDomain.trim(),
        port: form.port ?? 3000,
        host_port: form.hostPort ?? 0,
        server_id: form.serverId,
        env: form.env.filter((row) => row.key.trim() !== ""),
        storage: form.storage.filter((row) => row.name.trim() !== ""),
      };
    }
    // git_private is provider-less: the clone URL doubles as the repo label,
    // like the git_public shape (the backend agreement check refuses any
    // provider on this type).
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
      // The GitHub App flow links the application to its connection, so the
      // clone takes the installation-token path; every other source stays
      // unlinked on the legacy path.
      github_app_id: isGitHubAppFlow.value ? form.providerId : undefined,
      dockerfile_content: form.sourceType === "dockerfile" ? form.dockerfileContent : undefined,
      build_args: form.sourceType === "dockerfile" ? buildArgsPayload(form.buildArgs) : undefined,
      branch: form.branch.trim(),
      // A Dockerfile source always builds with the Dockerfile engine; the
      // build-pack choice is hidden and never sent.
      build_pack: form.sourceType === "dockerfile" ? "" : form.buildPack,
      image_ref: "",
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
      // A provider-less private SSH source stops here: the key is generated
      // and shown on the key step, and the first deploy waits for the
      // operator to register it (Test + Deploy). Queueing immediately would
      // always fail with permission denied. The HTTPS token needs no
      // operator action, so it seals and falls through to the auto-deploy.
      if (form.sourceType === "git_private" && form.privateAuth === "ssh") {
        try {
          const key = await createDeployKey(application.id);
          createdKey.value = { application, publicKey: key.public_key };
        } catch (credError) {
          // The application row already exists, so a plain retry of the
          // submit would hit a name conflict: park the application for the
          // recovery actions (retry the key, or delete and start over).
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

  /** runCreatedKeyTest probes the new SSH application with its stored key. */
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

  /** deployCreatedKey queues the first deploy of the SSH application. */
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

  /** closeCreatedKey leaves the key step for the detail page (same landing). */
  function closeCreatedKey(): void {
    if (!createdKey.value) {
      return;
    }
    const created = createdKey.value.application;
    resetWizard();
    emit("created", created);
    emit("update:show", false);
  }

  /** retryKeyCreation retries the deploy key for the parked application. */
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

  /** deleteRecoveryApp rolls the parked application back and restarts. */
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

  /** seedScope copies the live route scope into the form. */
  function seedScope(): void {
    form.projectId = toValue(scope.projectId);
    form.environmentId = toValue(scope.environmentId);
  }

  /** handleShowChange mirrors the modal visibility and resets when closing.
   * Closing from the key step still emits created: the application exists,
   * and without the event the list behind the modal goes stale. */
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

  /** resetWizard returns every field to its initial value. */
  function resetWizard(): void {
    step.value = 0;
    form.sourceType = "git_public";
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
    form.port = 3000;
    form.hostPort = null;
    form.baseDomain = "";
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

