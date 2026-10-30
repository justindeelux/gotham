import { http } from "@/shared/api/http";
import { activeLocale, i18n } from "@/shared/i18n";
import { conflictDetail, isApiError, stripErrorPrefix } from "@/features/servers";

export type ServiceStatus =
  | "creating"
  | "running"
  | "stopped"
  | "error"
  | "deleting";

export type ServiceDeployState = "deploying" | "running" | "failed" | "stopped";

export interface ServiceDomainRoute {
  service: string;
  domain: string;
  port: number;
}

export interface DeleteWarnings {
  warnings?: string[];
}

export type ServiceDomainStatus = "routed" | "held" | "shadowed" | "unknown";
export type ServiceDomainProtocol = "http" | "https";
export type ServiceDomainSource = "managed" | "compose";

export interface ServiceDomain {
  id?: string;
  service_id: string;
  domain: string;
  compose_service: string;
  port: number;
  protocol: ServiceDomainProtocol;
  disabled: boolean;
  source: ServiceDomainSource;
  status: ServiceDomainStatus;
  reason?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ServiceDomainInput {
  domain: string;
  compose_service: string;
  port: number;
  protocol: ServiceDomainProtocol;
  disabled?: boolean;
}

export interface Service {
  id: string;
  name: string;
  status: ServiceStatus;
  server_id: string;
  server_name: string;
  environment_id: string;
  environment_name: string;
  project_id: string;
  project_name: string;
  compose_project: string;
  template?: string;
  compose_yaml?: string;
  env: Record<string, string>;
  domains: ServiceDomainRoute[];
  pending_changes?: string[];
  created_at: string;
  updated_at: string;
}

export interface ServiceDeploy {
  id: string;
  service_id: string;
  state: ServiceDeployState;
  error?: string;
  created_at: string;
  updated_at: string;
  finished_at?: string;
}

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

export interface CreateServiceInput {
  name: string;
  environment_id: string;
  server_id: string;
  compose_yaml: string;
  env?: Record<string, string>;
  deploy?: boolean;
  template?: string;
  domains?: ServiceDomainInput[];
}

export interface UpdateServiceInput {
  name?: string;
  environment_id?: string;
  server_id?: string;
  compose_yaml?: string;
  env?: Record<string, string>;
}

export interface DeployOutcome {
  service: Service;
  deploy: ServiceDeploy;
}

interface ServiceEnvelope {
  service: Service;
}

interface ServiceListEnvelope {
  services: Service[];
}

interface DeployEnvelope {
  service: Service;
  deploy: ServiceDeploy;
}

interface DeployListEnvelope {
  deploys: ServiceDeploy[];
}

interface ContainerListEnvelope {
  containers: ComposeServiceContainer[];
}

interface DomainListEnvelope {
  domains: ServiceDomain[];
}

interface DomainEnvelope {
  domain: ServiceDomain;
}

const lifecycleTimeoutMs = 15 * 60_000;

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

export async function getService(id: string): Promise<Service> {
  const response = await http.get<ServiceEnvelope>(`/services/${id}`);
  return response.data.service;
}

export async function createService(
  input: CreateServiceInput,
): Promise<Service> {
  const response = await http.post<ServiceEnvelope>("/services", input);
  return response.data.service;
}

export async function updateService(
  id: string,
  input: UpdateServiceInput,
): Promise<Service> {
  const response = await http.patch<ServiceEnvelope>(`/services/${id}`, input);
  return response.data.service;
}

export async function deleteService(id: string): Promise<string[]> {
  const response = await http.delete<DeleteWarnings>(
    `/services/${id}`,
    { timeout: lifecycleTimeoutMs },
  );
  return response.data?.warnings ?? [];
}

export async function deployService(id: string): Promise<DeployOutcome> {
  // The control plane answers 202 and finishes the deploy in the background;
  // the detail page follows it through polling.
  const response = await http.post<DeployEnvelope>(`/services/${id}/deploy`, {});
  return { service: response.data.service, deploy: response.data.deploy };
}

export async function stopService(id: string): Promise<Service> {
  const response = await http.post<ServiceEnvelope>(
    `/services/${id}/stop`,
    {},
    { timeout: lifecycleTimeoutMs },
  );
  return response.data.service;
}

export async function restartService(id: string): Promise<Service> {
  const response = await http.post<ServiceEnvelope>(
    `/services/${id}/restart`,
    {},
    { timeout: lifecycleTimeoutMs },
  );
  return response.data.service;
}

export async function listServiceDeploys(id: string): Promise<ServiceDeploy[]> {
  const response = await http.get<DeployListEnvelope>(`/services/${id}/deploys`);
  return response.data.deploys ?? [];
}

export async function listServiceContainers(
  id: string,
): Promise<ComposeServiceContainer[]> {
  const response = await http.get<ContainerListEnvelope>(
    `/services/${id}/containers`,
  );
  return response.data.containers ?? [];
}

export async function listServiceDomains(id: string): Promise<ServiceDomain[]> {
  const response = await http.get<DomainListEnvelope>(`/services/${id}/domains`);
  return response.data.domains ?? [];
}

export async function addServiceDomain(
  id: string,
  input: ServiceDomainInput,
): Promise<ServiceDomain> {
  const response = await http.post<DomainEnvelope>(
    `/services/${id}/domains`,
    input,
  );
  return response.data.domain;
}

export async function updateServiceDomain(
  id: string,
  domainId: string,
  input: ServiceDomainInput,
): Promise<ServiceDomain> {
  const response = await http.put<DomainEnvelope>(
    `/services/${id}/domains/${domainId}`,
    input,
  );
  return response.data.domain;
}

export async function removeServiceDomain(
  id: string,
  domainId: string,
): Promise<void> {
  await http.delete(`/services/${id}/domains/${domainId}`);
}

export interface ServiceLogsOptions {
  service?: string;
  tail?: number;
  follow?: boolean;
}

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

export function serviceStatusLabel(status: ServiceStatus): string {
  void activeLocale.value;
  const key = `services.status.${status}`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return status;
}

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

export function deployStateLabel(state: ServiceDeployState): string {
  void activeLocale.value;
  const key = `services.deployState.${state}`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return state;
}

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

export function describeServiceError(error: unknown): string {
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
      const detail = stripErrorPrefix(error.message);
      return detail
        ? text("services.errors.badRequestWithDetail", { detail })
        : text("services.errors.badRequest");
    }
    if (error.status === 404) {
      const detail = stripErrorPrefix(error.message);
      return detail
        ? text("services.errors.notFoundWithDetail", { detail })
        : text("services.errors.notFound");
    }
    if (error.status === 409) {
      const detail = conflictDetail(stripErrorPrefix(error.message));
      return detail
        ? text("services.errors.conflictWithDetail", { detail })
        : text("services.errors.conflictFallback");
    }
    if (error.status === 502) {
      const detail = stripErrorPrefix(error.message);
      if (detail === "") {
        return text("services.errors.nodeUnreachable");
      }
      const key = "services.errors.nodeAgentError";
      if (i18n.global.te(key)) {
        return String(i18n.global.t(key, { detail }));
      }
      return `Node agent error: ${detail}`;
    }
    if (error.status === 503) {
      return text("services.errors.disabled");
    }
    return unknownFailure(stripErrorPrefix(error.message), text("common.errors.requestFailed"));
  }
  if (error instanceof Error) {
    return unknownFailure(stripErrorPrefix(error.message), text("common.errors.unexpected"));
  }
  return text("common.errors.unexpected");
}

function unknownFailure(detail: string, emptyFallback: string): string {
  if (detail === "") {
    return emptyFallback;
  }
  const key = "services.errors.unknownWithDetail";
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key, { detail }));
  }
  return `Request failed: ${detail}`;
}
