import { http } from "@/shared/api/http";
import { activeLocale, i18n } from "@/shared/i18n";
import { conflictDetail, isApiError, stripErrorPrefix } from "@/features/servers";

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

/** One compose service as the API returns it (see ServiceResponse). */
export interface Service {
  id: string;
  name: string;
  status: ServiceStatus;
  server_id: string;
  server_name: string;
  environment_id: string;
  environment_name: string;
  project_id: string;
  /** The Gotham project (see compose_project for the compose name). */
  project_name: string;
  /** The compose project name (`gotham-<id>`) the node runs. */
  compose_project: string;
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
  environment_id: string;
  server_id: string;
  compose_yaml: string;
  /** `${VAR}` substitution input; template secret values live here only. */
  env?: Record<string, string>;
}

/**
 * Body of PATCH /services/{id}. Omitted fields stay unchanged; an empty `env`
 * map clears the environment (`env` is omitted, not cleared, when undefined).
 * `environment_id` moves the service within the caller's team (409 on a name
 * collision in the target); `server_id` changes the node (409 `a deploy is in
 * progress` while one runs, `a deployed service cannot change server` once
 * the service has deployed).
 */
export interface UpdateServiceInput {
  name?: string;
  environment_id?: string;
  server_id?: string;
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

/**
 * Lifecycle calls run synchronously on the control plane: deploy renders,
 * validates and starts the project through the node agent (image pulls
 * included) and the backend allows up to 15 minutes for one attempt (see
 * `defaultDeployTimeout` in internal/services/service.go). The shared 15s
 * read timeout would abort a healthy cold start and, because the agent calls
 * inherit the HTTP request context, cancelling can cancel node work. Lifecycle
 * requests therefore get their own budget; short reads keep the shared one.
 */
const lifecycleTimeoutMs = 15 * 60_000;

/**
 * listServices returns the caller's services, newest first (GET → 200).
 * `filter` optionally scopes the list to one environment or project
 * (?environment_id= / ?project_id=).
 */
export async function listServices(filter: {
  environment_id?: string;
  project_id?: string;
} = {}): Promise<Service[]> {
  const params: Record<string, string> = {};
  if (filter.environment_id) {
    params.environment_id = filter.environment_id;
  }
  if (filter.project_id) {
    params.project_id = filter.project_id;
  }
  const response = await http.get<ServiceListEnvelope>("/services", { params });
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
 * row (DELETE → 204, no body). Stopping is synchronous node work, so the call
 * uses the lifecycle budget.
 */
export async function deleteService(id: string): Promise<void> {
  await http.delete(`/services/${id}`, { timeout: lifecycleTimeoutMs });
}

/**
 * deployService renders, validates and starts the project (POST → 200). It
 * blocks until the node agent's compose run finished, hence the lifecycle
 * timeout.
 */
export async function deployService(id: string): Promise<DeployOutcome> {
  const response = await http.post<DeployEnvelope>(
    `/services/${id}/deploy`,
    {},
    { timeout: lifecycleTimeoutMs },
  );
  return { service: response.data.service, deploy: response.data.deploy };
}

/**
 * stopService takes the project down; named volumes keep their data. Compose
 * down also runs synchronously on the node.
 */
export async function stopService(id: string): Promise<Service> {
  const response = await http.post<ServiceEnvelope>(
    `/services/${id}/stop`,
    {},
    { timeout: lifecycleTimeoutMs },
  );
  return response.data.service;
}

/**
 * restartService restarts a running project in place, or starts a stopped one.
 * A start is a synchronous compose up, so it shares the lifecycle budget.
 */
export async function restartService(id: string): Promise<Service> {
  const response = await http.post<ServiceEnvelope>(
    `/services/${id}/restart`,
    {},
    { timeout: lifecycleTimeoutMs },
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

/** serviceStatusLabel renders the lifecycle state as display text.
 * Resolves in the active locale at invocation time so template callers
 * refresh on a language switch; the wire status value itself is never
 * translated. English output is unchanged. */
export function serviceStatusLabel(status: ServiceStatus): string {
  // Tracks the locale when called during render or inside a computed.
  void activeLocale.value;
  const key = `services.status.${status}`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
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

/** deployStateLabel renders a deploy state as display text.
 * Resolves in the active locale at invocation time; the wire state value
 * itself is never translated. English output is unchanged. */
export function deployStateLabel(state: ServiceDeployState): string {
  // Tracks the locale when called during render or inside a computed.
  void activeLocale.value;
  const key = `services.deployState.${state}`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
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
 *
 * Classification still runs on the raw error (status plus the stripped
 * server message, exactly as before); only the curated fallback summaries
 * resolve in the active locale. Actionable server text passes through
 * untouched. The 502 detail keeps its literal "Node agent error:" framing:
 * it prefixes raw node diagnostics, and the source-based harness pins that
 * exact shape.
 */
export function describeServiceError(error: unknown): string {
  // Tracks the locale when called during render or inside a computed, so
  // retained failures refresh on a language switch.
  void activeLocale.value;
  const text = (
    key: string,
    params?: Record<string, string | number>,
  ): string => String(i18n.global.t(key, params ?? {}));
  if (isApiError(error)) {
    if (error.status === 401) {
      return text("services.errors.sessionExpired");
    }
    if (error.status === 400) {
      return (
      stripErrorPrefix(error.message) ||
      text("services.errors.badRequest")
    );
    }
    if (error.status === 404) {
      return (
      stripErrorPrefix(error.message) ||
      text("services.errors.notFound")
    );
    }
    if (error.status === 409) {
      // The backend names the refusal exactly (a duplicate name, `a deploy
      // is in progress`, `a deployed service cannot change server`), so the
      // message passes through for the move/server-change settings to
      // render inline.
      return (
        conflictDetail(stripErrorPrefix(error.message)) ||
        text("services.errors.conflictFallback")
      );
    }
    if (error.status === 502) {
      const detail = stripErrorPrefix(error.message);
      return detail
        ? `Node agent error: ${detail}`
        : text("services.errors.nodeUnreachable");
    }
    if (error.status === 503) {
      return text("services.errors.disabled");
    }
    return stripErrorPrefix(error.message) || text("common.errors.requestFailed");
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || text("common.errors.unexpected");
  }
  return text("common.errors.unexpected");
}
