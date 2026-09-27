import { http } from "./http";
import { isApiError } from "./servers";

/**
 * Typed client for the deploy routes served by `internal/deploy`:
 *
 *   POST /applications/{id}/deploy
 *   GET  /applications/{id}/deployments
 *   POST /applications/{id}/rollback
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * There is no applications CRUD route in this backend build (BE-4.3 mounts
 * only the three deploy endpoints above), so the `Application` interface below
 * mirrors the domain model (`internal/deploy/model.go`) for display and for
 * the creation-wizard payload only — `listApplications`, `getApplication` and
 * `createApplication` call the conventional REST shape and surface the
 * backend's answer honestly (currently 404) instead of fabricating rows.
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

/** Wire envelope for an application list (pending backend route). */
interface ApplicationListEnvelope {
  applications: Application[];
}

/** Wire envelope for a single application (pending backend route). */
interface ApplicationEnvelope {
  application: Application;
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
 * listApplications reads the conventional list shape. The route is not mounted
 * in this backend build, so callers must handle the rejection — the
 * applications page renders an explicit empty state instead of fake rows.
 */
export async function listApplications(): Promise<Application[]> {
  const response = await http.get<ApplicationListEnvelope>("/applications");
  return response.data.applications ?? [];
}

/**
 * getApplication reads one application. The route is not mounted in this
 * backend build; the detail page falls back to the deployment history.
 */
export async function getApplication(id: string): Promise<Application> {
  const response = await http.get<ApplicationEnvelope>(`/applications/${id}`);
  return response.data.application;
}

/**
 * createApplication posts the wizard payload. The route is not mounted in this
 * backend build — the rejection surfaces through describeApplicationError so
 * the failure is explicit and the payload shape stays ready for the backend.
 */
export async function createApplication(
  input: CreateApplicationInput,
): Promise<Application> {
  const response = await http.post<ApplicationEnvelope>(
    "/applications",
    input,
  );
  return response.data.application;
}

/** describeApplicationError maps a thrown error to a user-facing message. */
export function describeApplicationError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 404) {
      return "The applications API is not available in this backend build yet.";
    }
    if (error.status === 409) {
      return "A deployment is already in progress for this application.";
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
