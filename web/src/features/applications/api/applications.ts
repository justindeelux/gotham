import { http, teamHeaders } from "@/shared/api/http";
import type { SourceType } from "@/features/applications/schemas/applications";
import { i18n } from "@/shared/i18n";
import { conflictDetail, isApiError, stripErrorPrefix } from "@/features/servers";

/**
 * Typed client for the application routes served by `internal/deploy`
 * (see `Mount` in routes.go):
 *
 *   GET    /applications
 *   POST   /applications
 *   GET    /applications/{id}
 *   PUT    /applications/{id}
 *   DELETE /applications/{id}
 *   GET    /applications/{id}/env
 *   PUT    /applications/{id}/env
 *   GET    /applications/{id}/storages
 *   PUT    /applications/{id}/storages
 *   POST   /applications/{id}/stop
 *   POST   /applications/{id}/start
 *   POST   /applications/{id}/deploy
 *   GET    /applications/{id}/deployments
 *   POST   /applications/{id}/rollback
 *   POST   /applications/{id}/deploy-key
 *   GET    /applications/{id}/deploy-key
 *   PUT    /applications/{id}/git-credential
 *   GET    /applications/{id}/git-credential
 *   POST   /applications/{id}/test-connection
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * The `Application` interface below mirrors the domain model
 * (`internal/deploy/model.go`) and its wire representation
 * (`applicationResponse` in routes.go): `server_id` is null while no node is
 * assigned.
 */

/** An application as stored by the deploy domain (see model.go). */
export interface Application {
  id: string;
  name: string;
  environment_id: string;
  environment_name: string;
  project_id: string;
  /** The Gotham project the environment belongs to. */
  project_name: string;
  provider: string;
  repo: string;
  clone_url: string;
  /** How the application fetches its code (GS-2 source model). */
  source_type: SourceType;
  /** Linked GitHub App connection (GS-5), empty when unlinked. */
  github_app_id: string;
  /** Pasted Dockerfile text for dockerfile applications (GS-7). Detail routes only; absent on lists. */
  dockerfile_content?: string;
  /** Optional --build-arg pairs for dockerfile applications. Detail routes only; absent on lists. */
  build_args?: Record<string, string>;
  /** Pasted compose text (compose sources, GS-8). Detail routes only; absent on lists. */
  compose_content?: string;
  /** In-repo compose file path of a repo-backed compose source. */
  compose_file?: string;
  /** Compose service the domain/port routing targets. */
  compose_service?: string;
  branch: string;
  build_pack: string;
  /** Prebuilt reference of an image source (GS-9); empty otherwise. */
  image_ref: string;
  /** Whether a private-registry credential is stored (never the credential). */
  has_registry_credential: boolean;
  base_domain: string;
  /** True when a legacy duplicate binding was disabled; an explicit domain update re-enables it. */
  base_domain_disabled: boolean;
  port: number;
  host_port: number;
  server_id: string | null;
  server_name: string;
  created_at: string;
  updated_at: string;
}

/** Deploy intent: a normal deploy or a rollback (see state.go). */
export type DeploymentKind = "deploy" | "rollback";

/**
 * Deploy lifecycle state (see state.go):
 * queued → cloning → building → pushing → starting → running | failed.
 */
export type DeploymentState =
  | "queued"
  | "cloning"
  | "building"
  | "pushing"
  | "starting"
  | "running"
  | "failed";

/** One deploy attempt of an application (see routes.go wire shape). */
export interface Deployment {
  id: string;
  application_id: string;
  kind: DeploymentKind;
  state: DeploymentState;
  image_tag: string;
  registry_image: string;
  digest: string;
  error: string;
  attempt: number;
  container_id: string;
  rollback_from: string;
  /** Built commit the deployment was built from (JUS-82); empty when unknown. */
  commit_sha?: string;
  /** First-line subject plus body of the built commit; empty when unknown. */
  commit_message?: string;
  /** Author of the built commit; empty when unknown. */
  commit_author?: string;
  /** Commit timestamp (ISO); empty when unknown. */
  committed_at?: string;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

/** A plain KEY=VALUE setting sent to the container (see model.go). */
export interface EnvVar {
  key: string;
  value: string;
}

/** A persistent node directory mounted into the container (see model.go). */
export interface StorageMapping {
  name: string;
  host_path: string;
  container_path: string;
}

/**
 * Build packs accepted by `builds.ParseEngineKind`. The empty string selects
 * auto-detection; `nixpacks`/`herokuish` are backend aliases and are not
 * offered as choices.
 */
export type BuildPack = "" | "dockerfile" | "railpack" | "buildpacks" | "static";

/** Body sent to create an application (mirrors the applications table). */
export interface CreateApplicationInput {
  name: string;
  /** Environment the application belongs to (required since PE-2). */
  environment_id: string;
  provider: string;
  repo: string;
  clone_url: string;
  /** How the application fetches its code (GS-2 source model). */
  source_type: SourceType;
  /** Links the application to its GitHub App connection (GS-5); omit to leave unlinked. */
  github_app_id?: string;
  /** Pasted Dockerfile text for the dockerfile source type (GS-7). */
  dockerfile_content?: string;
  /** Optional --build-arg pairs for the dockerfile source type. */
  build_args?: Record<string, string>;
  /** Pasted compose text for the compose source type (GS-8). */
  compose_content?: string;
  /** In-repo compose file path for a repo-backed compose source. */
  compose_file?: string;
  /** Compose service the domain/port routing targets. */
  compose_service?: string;
  branch: string;
  build_pack: string;
  /** Prebuilt reference for image sources (GS-9). */
  image_ref: string;
  /**
   * Private-registry credential for image sources, plaintext on the way in
   * and sealed at rest. Never returned by the API.
   */
  registry_username?: string;
  registry_password?: string;
  base_domain: string;
  port: number;
  host_port: number;
  server_id: string;
  env: EnvVar[];
  storage: StorageMapping[];
}

/** Body accepted by PUT /applications/{id} (see updateApplicationRequest). */
export interface UpdateApplicationInput {
  name?: string;
  /**
   * environment_id moves the application within the caller's team. A move
   * that collides with a name in the target answers 409; a base application
   * with open previews answers 409 (`close the open previews first`).
   */
  environment_id?: string;
  branch?: string;
  build_pack?: string;
  /** Replacement prebuilt reference of an image source (GS-9). */
  image_ref?: string;
  /**
   * Registry credential rotation of an image source; either half may be set
   * independently and an empty value clears that half. Never returned.
   */
  registry_username?: string;
  registry_password?: string;
  base_domain?: string;
  port?: number;
  host_port?: number;
  /**
   * server_id pins the application to a node. An empty string clears the
   * assignment; omit the field to leave it unchanged.
   */
  server_id?: string;
  /** Replaces the stored Dockerfile text (dockerfile applications only). */
  dockerfile_content?: string;
  /** Replaces the whole --build-arg collection (absent leaves it unchanged). */
  build_args?: Record<string, string>;
  /** Replaces the stored compose text (compose applications only, GS-8). */
  compose_content?: string;
  /** Replaces the in-repo compose file path (absent leaves it unchanged). */
  compose_file?: string;
  /** Replaces the routed compose web service. */
  compose_service?: string;
}

/** Optional body of POST .../rollback (see routes.go). */
export interface RollbackInput {
  deployment_id?: string;
}

/** Wire envelope for a single deployment. */
interface DeploymentEnvelope {
  deployment: Deployment;
}

/** Wire envelope for a deployment list. */
interface DeploymentListEnvelope {
  deployments: Deployment[];
}

/** Wire envelope for an application list. */
interface ApplicationListEnvelope {
  applications: Application[];
}

/** Wire envelope for a single application. */
interface ApplicationEnvelope {
  application: Application;
  /**
   * Outcome of the automatic provider-hook install on create (see
   * `webhookOutcome` in `internal/deploy/routes.go`). Absent when no install
   * was attempted — a pasted public URL, or no hook lifecycle wired.
   */
  webhook?: HookOutcome;
}

/**
 * Outcome of the automatic provider-hook install when an application is
 * created. `installed: false` means the application was created but automatic
 * deploys are off; `error` names the idempotent retry route.
 */
export interface HookOutcome {
  installed: boolean;
  error?: string;
}

/** Result of a successful create: the application plus its hook outcome. */
export interface CreatedApplication {
  application: Application;
  webhook?: HookOutcome;
}

/** Wire envelope for the environment collection (see envListEnvelope). */
interface EnvListEnvelope {
  env: EnvVar[];
}

/** Wire envelope for the storage collection (see storageListEnvelope). */
interface StorageListEnvelope {
  storage: StorageMapping[];
}

/** Non-terminal states: a deployment still moving through the machine. */
const activeStates: ReadonlySet<DeploymentState> = new Set([
  "queued",
  "cloning",
  "building",
  "pushing",
  "starting",
]);

/** isActiveDeployment reports whether a deployment can still transition. */
export function isActiveDeployment(deployment: Deployment): boolean {
  return activeStates.has(deployment.state);
}

/**
 * countRunning counts applications whose newest deployment reached "running".
 * Pure so the dashboard tile and the harness share one definition.
 */
export function countRunning(
  latestStates: Array<DeploymentState | null | undefined>,
): number {
  return latestStates.filter((state) => state === "running").length;
}

/**
 * deployChannel returns the realtime channel carrying one deployment's logs.
 * It mirrors `DeployChannel` in `internal/deploy/events.go`
 * (`logs:{serverID}:{deploymentID}`) so the existing WS bridge forwards deploy
 * logs without any change.
 */
export function deployChannel(serverId: string, deploymentId: string): string {
  return `logs:${serverId}:${deploymentId}`;
}

/**
 * listDeployments returns an application's deployments, newest first. A
 * positive limit bounds the page to that many rows (GET ?limit=, clamped
 * server-side); readers that only need the latest state pass 1 so the full
 * history is never transferred. Omitting the limit returns the full history
 * for the build-history surfaces; an explicit zero or negative limit is a
 * 400, so callers must omit rather than send 0.
 */
export async function listDeployments(
  appId: string,
  limit = 0,
): Promise<Deployment[]> {
  const response = await http.get<DeploymentListEnvelope>(
    `/applications/${appId}/deployments`,
    limit > 0 ? { params: { limit } } : undefined,
  );
  return response.data.deployments ?? [];
}

/**
 * LatestStates is the outcome of reading one newest deployment per
 * application: the states in input order (null when the application has no
 * deployments) plus the count of per-application reads that failed, so the
 * caller can mark its figure incomplete instead of falsely low.
 */
export interface LatestStates {
  states: Array<DeploymentState | null>;
  failed: number;
}

/**
 * latestDeploymentStates reads the newest deployment of every application,
 * fetching at most the latest row per application with bounded concurrency
 * (default 4) instead of one unbounded full-history request per application.
 * A failed per-application read counts toward `failed` and yields null
 * rather than failing the whole summary.
 */
export async function latestDeploymentStates(
  appIds: string[],
  concurrency = 4,
): Promise<LatestStates> {
  const states: Array<DeploymentState | null> = new Array(appIds.length).fill(
    null,
  );
  let failed = 0;
  const lanes = Math.max(1, Math.floor(concurrency));
  for (let start = 0; start < appIds.length; start += lanes) {
    const batch = await Promise.all(
      appIds.slice(start, start + lanes).map((appId, offset) =>
        listDeployments(appId, 1).then(
          (deployments): { index: number; state: DeploymentState | null } => ({
            index: start + offset,
            state: deployments.length > 0 ? deployments[0].state : null,
          }),
          (): { index: number; state: DeploymentState | null } => {
            failed += 1;
            return { index: start + offset, state: null };
          },
        ),
      ),
    );
    for (const { index, state } of batch) {
      states[index] = state;
    }
  }
  return { states, failed };
}

/**
 * triggerDeploy queues a deployment of the application's current revision.
 * The backend answers 202 as soon as the job is queued.
 */
export async function triggerDeploy(appId: string): Promise<Deployment> {
  const response = await http.post<DeploymentEnvelope>(
    `/applications/${appId}/deploy`,
    {},
  );
  return response.data.deployment;
}

/**
 * rollbackDeployment queues a rollback to a previous release's image. With no
 * deployment id the server picks the previous successful release.
 */
export async function rollbackDeployment(
  appId: string,
  input: RollbackInput = {},
): Promise<Deployment> {
  const response = await http.post<DeploymentEnvelope>(
    `/applications/${appId}/rollback`,
    input.deployment_id ? input : {},
  );
  return response.data.deployment;
}

/**
 * listApplications returns one team's applications, newest first
 * (GET /applications → 200). An empty teamId reads the caller's personal
 * team, matching the other pre-teams surfaces. `filter` optionally scopes
 * the list to one environment or project (?environment_id= / ?project_id=,
 * mutually exclusive server-side).
 */
export async function listApplications(
  teamId = "",
  filter: { environment_id?: string; project_id?: string } = {},
): Promise<Application[]> {
  const params: Record<string, string> = {};
  if (filter.environment_id) {
    params.environment_id = filter.environment_id;
  }
  if (filter.project_id) {
    params.project_id = filter.project_id;
  }
  const response = await http.get<ApplicationListEnvelope>("/applications", {
    headers: teamHeaders(teamId),
    params,
  });
  return response.data.applications ?? [];
}

/**
 * getApplication returns one application (GET /applications/{id} → 200).
 * Another user's row answers 404, so ids cannot be probed.
 */
export async function getApplication(id: string): Promise<Application> {
  const response = await http.get<ApplicationEnvelope>(`/applications/${id}`);
  return response.data.application;
}

/**
 * createApplication stores a new application with its environment and storage
 * in one transaction (POST /applications → 201). The result also carries the
 * automatic provider-hook outcome, when one was attempted: the application is
 * stored even when its hook install failed, and the caller should surface that
 * (`webhook.installed === false`) to the user.
 */
export async function createApplication(
  input: CreateApplicationInput,
): Promise<CreatedApplication> {
  const response = await http.post<ApplicationEnvelope>(
    "/applications",
    input,
  );
  return response.data;
}

/**
 * updateApplication applies a partial update to the mutable fields
 * (PUT /applications/{id} → 200). Absent fields stay unchanged.
 */
export async function updateApplication(
  id: string,
  input: UpdateApplicationInput,
): Promise<Application> {
  const response = await http.put<ApplicationEnvelope>(
    `/applications/${id}`,
    input,
  );
  return response.data.application;
}

/**
 * deleteApplication stops the current container best effort and removes the
 * row (DELETE /applications/{id} → 204, no body).
 */
export async function deleteApplication(id: string): Promise<void> {
  await http.delete(`/applications/${id}`);
}

/**
 * getEnv returns the environment: plain values verbatim, sealed secrets as
 * `secret:<id>` references (GET .../env → 200). Plaintext never leaves the
 * server.
 */
export async function getEnv(appId: string): Promise<EnvVar[]> {
  const response = await http.get<EnvListEnvelope>(
    `/applications/${appId}/env`,
  );
  return response.data.env ?? [];
}

/**
 * replaceEnv rewrites the whole environment collection and returns it as
 * stored (PUT .../env → 200). A `secret:` value that still points at a stored
 * secret keeps its ciphertext; any other `secret:` value is sealed as new
 * plaintext.
 */
export async function replaceEnv(
  appId: string,
  env: EnvVar[],
): Promise<EnvVar[]> {
  const response = await http.put<EnvListEnvelope>(
    `/applications/${appId}/env`,
    { env },
  );
  return response.data.env ?? [];
}

/** getStorages returns the application's storage mappings (GET → 200). */
export async function getStorages(appId: string): Promise<StorageMapping[]> {
  const response = await http.get<StorageListEnvelope>(
    `/applications/${appId}/storages`,
  );
  return response.data.storage ?? [];
}

/**
 * replaceStorages rewrites the whole storage collection and returns it as
 * stored (PUT .../storages → 200).
 */
export async function replaceStorages(
  appId: string,
  storage: StorageMapping[],
): Promise<StorageMapping[]> {
  const response = await http.put<StorageListEnvelope>(
    `/applications/${appId}/storages`,
    { storage },
  );
  return response.data.storage ?? [];
}

/**
 * stopApplication stops the container of the newest deployment and answers
 * with that deployment (POST .../stop → 200).
 */
export async function stopApplication(appId: string): Promise<Deployment> {
  const response = await http.post<DeploymentEnvelope>(
    `/applications/${appId}/stop`,
    {},
  );
  return response.data.deployment;
}

/**
 * DeployKey is an application's SSH deploy key as the API sees it
 * (see deployKeyResponse in routes.go): the public half, its fingerprint
 * and the provider's key id. The private half is sealed on the server and
 * never appears here.
 */
export interface DeployKey {
  id: string;
  application_id: string;
  provider: string;
  repo: string;
  provider_key_id?: string;
  fingerprint: string;
  public_key: string;
  created_at: string;
}

/** Wire envelope for a single deploy key. */
interface DeployKeyEnvelope {
  deploy_key: DeployKey;
}

/**
 * getDeployKey returns an application's deploy key (GET .../deploy-key →
 * 200). An application without one answers 404, so the caller knows to
 * offer generation instead.
 */
export async function getDeployKey(appId: string): Promise<DeployKey> {
  const response = await http.get<DeployKeyEnvelope>(
    `/applications/${appId}/deploy-key`,
  );
  return response.data.deploy_key;
}

/**
 * createDeployKey generates the application's ed25519 keypair and returns
 * the public half (POST .../deploy-key → 201). Repeating the call returns
 * the key that already exists. For a provider-less git_private source no
 * Git-host call happens: the operator registers the public half by hand.
 */
export async function createDeployKey(appId: string): Promise<DeployKey> {
  const response = await http.post<DeployKeyEnvelope>(
    `/applications/${appId}/deploy-key`,
    {},
  );
  return response.data.deploy_key;
}

/**
 * deleteDeployKey removes the application's deploy key (DELETE .../deploy-key
 * → 200). An application without one answers deleted=false.
 */
export async function deleteDeployKey(appId: string): Promise<boolean> {
  const response = await http.delete<{ deleted: boolean }>(
    `/applications/${appId}/deploy-key`,
  );
  return response.data.deleted;
}

/**
 * GitCredentialState is an application's HTTPS credential as the API sees
 * it: whether a token is set and the username it carries. The token itself
 * is never returned.
 */
export interface GitCredentialState {
  has_credential: boolean;
  username?: string;
}

/**
 * getGitCredential reports whether an application's HTTPS token is set
 * (GET .../git-credential → 200).
 */
export async function getGitCredential(
  appId: string,
): Promise<GitCredentialState> {
  const response = await http.get<GitCredentialState>(
    `/applications/${appId}/git-credential`,
  );
  return response.data;
}

/**
 * setGitCredential stores (or rotates) an application's HTTPS token
 * (PUT .../git-credential → 200). The answer confirms what is set, never
 * the token.
 */
export async function setGitCredential(
  appId: string,
  username: string,
  token: string,
): Promise<GitCredentialState> {
  const response = await http.put<GitCredentialState>(
    `/applications/${appId}/git-credential`,
    { username, token },
  );
  return response.data;
}

/**
 * deleteGitCredential removes an application's HTTPS token
 * (DELETE .../git-credential → 200). An application without one answers
 * deleted=false.
 */
export async function deleteGitCredential(appId: string): Promise<boolean> {
  const response = await http.delete<{ deleted: boolean }>(
    `/applications/${appId}/git-credential`,
  );
  return response.data.deleted;
}

/**
 * ConnectionResult is the classified outcome of probing an application's
 * remote with git ls-remote and the stored credential
 * (POST .../test-connection → 200, even when the probe failed).
 */
export interface ConnectionResult {
  ok: boolean;
  message: string;
  host?: string;
}

/**
 * testConnection probes an application's remote and answers the classified
 * outcome (POST .../test-connection → 200).
 */
export async function testConnection(
  appId: string,
): Promise<ConnectionResult> {
  const response = await http.post<ConnectionResult>(
    `/applications/${appId}/test-connection`,
    {},
  );
  return response.data;
}

/**
 * startApplication restarts the container of the newest deployment
 * (POST .../start → 200).
 */
export async function startApplication(appId: string): Promise<Deployment> {
  const response = await http.post<DeploymentEnvelope>(
    `/applications/${appId}/start`,
    {},
  );
  return response.data.deployment;
}

/**
 * Lifecycle action whose 404 has a specific meaning instead of "app missing".
 * A stop/start 404 means there is no container in that state, not that the
 * application row vanished (C4-18).
 */
export type ApplicationControlAction = "stop" | "start";

/**
 * withRaw pairs a curated summary with its retained raw diagnostic. Unknown
 * failures require a localized summary with useful plain-text detail (plan
 * §3): the summary is translated, the diagnostic passes through byte-identical
 * (still redacted upstream), and an empty diagnostic renders the summary alone.
 */
function withRaw(summary: string, raw: string): string {
  return raw ? `${summary} ${raw}` : summary;
}

/** conflictSummary maps a raw 409 refusal onto its distinct curated summary. */
function conflictSummary(raw: string): string {
  const t = i18n.global.t.bind(i18n.global);
  if (raw === "") {
    // No detail to retain: keep the long-standing in-flight fallback copy.
    return String(t("applications.errors.conflictDeploy"));
  }
  if (raw.includes("a deploy is in progress")) {
    return String(t("applications.errors.conflictDeploy"));
  }
  if (raw.includes("close the open previews first")) {
    return String(t("applications.errors.previewsOpen"));
  }
  if (raw.includes("in the target environment")) {
    return String(t("applications.errors.nameConflict"));
  }
  // Any other refusal keeps a generic conflict summary, never the in-flight hint.
  return String(t("applications.errors.conflict"));
}

/**
 * describeApplicationError maps a thrown error to a user-facing message. The
 * mapping mirrors `writeServiceError` in `internal/deploy/routes.go`: 404 is
 * a missing (or foreign) application, 409 an in-flight deployment, 502 an
 * unreachable node agent and 503 a disabled feature flag. `action` sharpens the
 * 404 copy for the container lifecycle routes, where a 404 means "no container
 * in the target state" rather than "application not found".
 *
 * Classification always reads the raw status/message (never translated text);
 * only the curated summaries below resolve through the current-locale catalog
 * at invocation time. Raw server diagnostics pass through untouched.
 */
export function describeApplicationError(
  error: unknown,
  action?: ApplicationControlAction,
): string {
  const t = i18n.global.t.bind(i18n.global);
  const unexpected = (): string => String(t("common.errors.unexpected"));
  if (isApiError(error)) {
    if (error.status === 401) {
      return String(t("applications.errors.sessionExpired"));
    }
    if (error.status === 400) {
      return withRaw(
        String(t("applications.errors.invalidRequest")),
        stripErrorPrefix(error.message),
      );
    }
    if (error.status === 404) {
      if (action === "stop") {
        return String(t("applications.errors.noRunningContainer"));
      }
      if (action === "start") {
        return String(t("applications.errors.noContainerStart"));
      }
      return String(t("applications.errors.notFound"));
    }
    if (error.status === 409) {
      // The backend names the refusal exactly (`a deploy is in progress`
      // while one runs, `close the open previews first` for a base
      // application with open previews, or the name-collision text on a
      // move), so each maps to its own curated summary with the raw detail
      // retained for the move/server-change settings to render inline.
      // Classification reads the raw English refusal, never display text.
      const raw = stripErrorPrefix(error.message);
      return withRaw(conflictSummary(raw), raw === "" ? "" : conflictDetail(raw));
    }
    if (error.status === 502) {
      return withRaw(
        String(t("applications.errors.agentUnreachable")),
        stripErrorPrefix(error.message),
      );
    }
    if (error.status === 503) {
      return String(t("applications.errors.appsDisabled"));
    }
    return withRaw(
      String(t("common.errors.requestFailed")),
      stripErrorPrefix(error.message),
    );
  }
  if (error instanceof Error) {
    return withRaw(unexpected(), stripErrorPrefix(error.message));
  }
  return unexpected();
}
