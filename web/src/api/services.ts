import { http } from "./http";
import { isApiError } from "./servers";

/**
 * Typed client for the compose-service routes served by `internal/services`
 * (see routes.go for the contract):
 *
 *   POST   /services
 *   GET    /services
 *   GET    /services/{id}
 *   PATCH  /services/{id}
 *   DELETE /services/{id}
 *   POST   /services/{id}/deploy
 *   POST   /services/{id}/stop
 *   POST   /services/{id}/restart
 *   GET    /services/{id}/deploys
 *   GET    /services/{id}/containers
 *   GET    /services/{id}/logs           (plain-text chunked stream)
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * The service response carries `env` (the `${VAR}` substitution input) and,
 * on every read but the list, the stored `compose_yaml`. A service created
 * from a template keeps the rendered document here and the template's secret
 * values in `env`; the UI must pass that map back on writes and never render a
 * secret outside its masked input.
 */

/** Lifecycle of a service (see Status in internal/services/model.go). */
export type ServiceStatus =
  | "creating"
  | "running"
  | "stopped"
  | "error"
  | "deleting";

/** State of one deploy attempt (see DeployState in internal/services/model.go). */
export type ServiceDeployState = "deploying" | "running" | "failed" | "stopped";

/**
 * One compose service mapped to a public host through the `gotham.domain`
 * label convention (see DomainRoute in internal/services/model.go).
 */
export interface ServiceDomainRoute {
  service: string;
  domain: string;
  port: number;
}

/** One compose service as the API returns it (see serviceResponse in routes.go). */
export interface Service {
  id: string;
  name: string;
  status: ServiceStatus;
  server_id: string;
  project_name: string;
  /** Present on get/create/update/lifecycle responses, omitted in the list. */
  compose_yaml?: string;
  env: Record<string, string>;
  domains: ServiceDomainRoute[];
  created_at: string;
  updated_at: string;
}

/** One deploy attempt (see deployResponse in routes.go). */
export interface ServiceDeploy {
  id: string;
  service_id: string;
  state: ServiceDeployState;
  error?: string;
  created_at: string;
  updated_at: string;
  finished_at?: string;
}

/** One project container as the node reports it (see ComposeContainer). */
export interface ComposeServiceContainer {
  service: string;
  container_id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  health?: string;
  ports?: string[];
}

/** Body of POST /services. */
export interface CreateServiceInput {
  name: string;
  server_id: string;
  compose_yaml: string;
  /** `${VAR}` substitution input; template secret values live here only. */
  env?: Record<string, string>;
}

/**
 * Body of PATCH /services/{id}. Omitted fields stay unchanged; an empty `env`
 * map clears the environment (`env` is omitted, not cleared, when undefined).
 */
export interface UpdateServiceInput {
  name?: string;
  compose_yaml?: string;
  env?: Record<string, string>;
}

/** Result of POST /services/{id}/deploy: the service plus the attempt that ran. */
export interface DeployOutcome {
  service: Service;
  deploy: ServiceDeploy;
}

/** Wire envelope for one service. */
interface ServiceEnvelope {
  service: Service;
}

/** Wire envelope for a service list. */
interface ServiceListEnvelope {
  services: Service[];
}

/** Wire envelope for a deploy result. */
interface DeployEnvelope {
  service: Service;
  deploy: ServiceDeploy;
}

/** Wire envelope for a deploy history. */
interface DeployListEnvelope {
  deploys: ServiceDeploy[];
}

/** Wire envelope for a container list. */
interface ContainerListEnvelope {
  containers: ComposeServiceContainer[];
}

/** listServices returns the caller's services, newest first (GET → 200). */
export async function listServices(): Promise<Service[]> {
  const response = await http.get<ServiceListEnvelope>("/services");
  return response.data.services ?? [];
}

/** getService returns one service including its stored compose document. */
export async function getService(id: string): Promise<Service> {
  const response = await http.get<ServiceEnvelope>(`/services/${id}`);
  return response.data.service;
}

/**
 * createService stores a validated service (POST → 201). The control plane
 * validates the document against `env` and redacts every environment value
 * from later error messages, so secrets never surface outside this map.
 */
export async function createService(
  input: CreateServiceInput,
): Promise<Service> {
  const response = await http.post<ServiceEnvelope>("/services", input);
  return response.data.service;
}

/**
 * updateService patches the mutable fields (PATCH → 200). Saving a document
 * only stores it: the running project switches on the next deploy.
 */
export async function updateService(
  id: string,
  input: UpdateServiceInput,
): Promise<Service> {
  const response = await http.patch<ServiceEnvelope>(`/services/${id}`, input);
  return response.data.service;
}

/**
 * deleteService stops the project (named volumes stay) and soft-deletes the
 * row (DELETE → 204, no body).
 */
export async function deleteService(id: string): Promise<void> {
  await http.delete(`/services/${id}`);
}

/** deployService renders, validates and starts the project (POST → 200). */
export async function deployService(id: string): Promise<DeployOutcome> {
  const response = await http.post<DeployEnvelope>(`/services/${id}/deploy`, {});
  return { service: response.data.service, deploy: response.data.deploy };
}

/** stopService takes the project down; named volumes keep their data. */
export async function stopService(id: string): Promise<Service> {
  const response = await http.post<ServiceEnvelope>(`/services/${id}/stop`, {});
  return response.data.service;
}

/** restartService restarts a running project in place, or starts a stopped one. */
export async function restartService(id: string): Promise<Service> {
  const response = await http.post<ServiceEnvelope>(
    `/services/${id}/restart`,
    {},
  );
  return response.data.service;
}

/** listServiceDeploys returns the deploy history, newest first. */
export async function listServiceDeploys(id: string): Promise<ServiceDeploy[]> {
  const response = await http.get<DeployListEnvelope>(`/services/${id}/deploys`);
  return response.data.deploys ?? [];
}

/**
 * listServiceContainers lists the project's containers from the node. It dials
 * the node agent, so an unreachable agent answers 502 — callers load it on
 * demand rather than on page render.
 */
export async function listServiceContainers(
  id: string,
): Promise<ComposeServiceContainer[]> {
  const response = await http.get<ContainerListEnvelope>(
    `/services/${id}/containers`,
  );
  return response.data.containers ?? [];
}

/** Options of one log read (see the logs handler). */
export interface ServiceLogsOptions {
  /** One compose service of the project; empty streams every service. */
  service?: string;
  /** History lines to fetch before following; 0 streams the whole history. */
  tail?: number;
  /** Keep the connection open and follow new output. */
  follow?: boolean;
}

/**
 * serviceLogsPath builds the plain-text log stream URL. It is absolute because
 * the streaming reader in ServiceLogs.vue uses `fetch`, not the axios
 * instance: a chunked response must be consumed as a stream.
 */
export function serviceLogsPath(
  id: string,
  options: ServiceLogsOptions = {},
): string {
  const params = new URLSearchParams();
  if (options.service) {
    params.set("service", options.service);
  }
  if (options.tail !== undefined) {
    params.set("tail", String(options.tail));
  }
  if (options.follow) {
    params.set("follow", "true");
  }
  const query = params.toString();
  return `/api/v1/services/${id}/logs${query ? `?${query}` : ""}`;
}

/** serviceStatusLabel renders the lifecycle state as display text. */
export function serviceStatusLabel(status: ServiceStatus): string {
  return status;
}

/** serviceStatusTagType maps the lifecycle state onto a tag style. */
export function serviceStatusTagType(
  status: ServiceStatus,
): "success" | "warning" | "error" | "default" {
  switch (status) {
    case "running":
      return "success";
    case "creating":
    case "deleting":
      return "warning";
    case "error":
      return "error";
    default:
      return "default";
  }
}

/** deployStateLabel renders a deploy state as display text. */
export function deployStateLabel(state: ServiceDeployState): string {
  return state;
}

/** deployStateTagType maps a deploy state onto a tag style. */
export function deployStateTagType(
  state: ServiceDeployState,
): "success" | "warning" | "error" | "default" {
  switch (state) {
    case "running":
      return "success";
    case "deploying":
      return "warning";
    case "failed":
      return "error";
    default:
      return "default";
  }
}

/**
 * describeServiceError maps a thrown error to a user-facing message. The
 * mapping mirrors `writeServiceError` in internal/services/routes.go: 400 is
 * an actionable validation message, 404 a missing (or foreign) service, 409 a
 * duplicate name, 502 an unreachable node agent and 503 a disabled feature
 * flag. Secret material never reaches a message: the control plane redacts
 * every environment value before it is stored or returned.
 */
export function describeServiceError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 400) {
      return error.message || "Invalid request. Check the compose document and retry.";
    }
    if (error.status === 404) {
      return error.message || "Service not found. It may have been deleted already.";
    }
    if (error.status === 409) {
      return "A service with that name already exists. Pick another name.";
    }
    if (error.status === 502) {
      return error.message
        ? `Node agent error: ${error.message}`
        : "The node agent is unreachable. Check the node status and retry.";
    }
    if (error.status === 503) {
      return "Services are disabled on the control plane (FEATURE_SERVICES=false).";
    }
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}
