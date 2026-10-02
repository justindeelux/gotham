import { http, teamHeaders } from "./http";
import { isApiError } from "./servers";

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
  provider: string;
  repo: string;
  clone_url: string;
  branch: string;
  build_pack: string;
  base_domain: string;
  port: number;
  host_port: number;
  server_id: string | null;
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
  provider: string;
  repo: string;
  clone_url: string;
  branch: string;
  build_pack: string;
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
  branch?: string;
  build_pack?: string;
  base_domain?: string;
  port?: number;
  host_port?: number;
  /**
   * server_id pins the application to a node. An empty string clears the
   * assignment; omit the field to leave it unchanged.
   */
  server_id?: string;
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
 * deployChannel returns the realtime channel carrying one deployment's logs.
 * It mirrors `DeployChannel` in `internal/deploy/events.go`
 * (`logs:{serverID}:{deploymentID}`) so the existing WS bridge forwards deploy
 * logs without any change.
 */
export function deployChannel(serverId: string, deploymentId: string): string {
  return `logs:${serverId}:${deploymentId}`;
}

/** listDeployments returns an application's deployments, newest first. */
export async function listDeployments(appId: string): Promise<Deployment[]> {
  const response = await http.get<DeploymentListEnvelope>(
    `/applications/${appId}/deployments`,
  );
  return response.data.deployments ?? [];
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
 * team, matching the other pre-teams surfaces.
 */
export async function listApplications(teamId = ""): Promise<Application[]> {
  const response = await http.get<ApplicationListEnvelope>("/applications", {
    headers: teamHeaders(teamId),
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
 * describeApplicationError maps a thrown error to a user-facing message. The
 * mapping mirrors `writeServiceError` in `internal/deploy/routes.go`: 404 is
 * a missing (or foreign) application, 409 an in-flight deployment, 502 an
 * unreachable node agent and 503 a disabled feature flag. `action` sharpens the
 * 404 copy for the container lifecycle routes, where a 404 means "no container
 * in the target state" rather than "application not found".
 */
export function describeApplicationError(
  error: unknown,
  action?: ApplicationControlAction,
): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 400) {
      return error.message || "Invalid request. Check the highlighted fields and retry.";
    }
    if (error.status === 404) {
      if (action === "stop") {
        return "No running container to stop. It may already be stopped.";
      }
      if (action === "start") {
        return "No container to start. Deploy the application first.";
      }
      return "Application not found. It may have been deleted or belong to another account.";
    }
    if (error.status === 409) {
      return "A deployment is already in progress for this application. Wait for it to finish and retry.";
    }
    if (error.status === 502) {
      return "The node agent is unreachable. Check the node status and retry.";
    }
    if (error.status === 503) {
      return "Applications are disabled on the control plane (FEATURE_APPLICATIONS=false).";
    }
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}
