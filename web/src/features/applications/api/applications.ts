import { http } from "@/shared/api/http";
import type { SourceType } from "@/features/applications/schemas/applications";
import type { VolumeListing } from "@/features/applications/schemas/volumes";
import { volumeListingSchema } from "@/features/applications/schemas/volumes";
import { parseWith } from "@/shared/validation/parse";
import { i18n } from "@/shared/i18n";
import { conflictDetail, isApiError, stripErrorPrefix } from "@/features/servers";
import type { RuntimeHealth } from "@/shared/types/runtimeHealth";

export interface Application {
  id: string;
  name: string;
  environment_id: string;
  environment_name: string;
  project_id: string;
  project_name: string;
  provider: string;
  repo: string;
  clone_url: string;
  source_type: SourceType;
  github_app_id: string;
  dockerfile_content?: string;
  build_args?: Record<string, string>;
  compose_content?: string;
  compose_file?: string;
  compose_service?: string;
  branch: string;
  build_pack: string;
  image_ref: string;
  has_registry_credential: boolean;
  primary_domain: string;
  server_id: string | null;
  server_name: string;
  pending_changes: string[];
  runtime_health?: RuntimeHealth | null;
  created_at: string;
  updated_at: string;
}

export type DeploymentKind = "deploy" | "rollback";

export type DeploymentState =
  | "queued"
  | "cloning"
  | "building"
  | "pushing"
  | "starting"
  | "running"
  | "failed";

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
  commit_sha?: string;
  commit_message?: string;
  commit_author?: string;
  committed_at?: string;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface EnvVar {
  key: string;
  value: string;
}

export interface StorageMapping {
  name: string;
  host_path: string;
  container_path: string;
}

export type BuildPack = "" | "dockerfile" | "railpack" | "buildpacks" | "static";

export interface CreateApplicationInput {
  name: string;
  environment_id: string;
  provider: string;
  repo: string;
  clone_url: string;
  source_type: SourceType;
  github_app_id?: string;
  dockerfile_content?: string;
  build_args?: Record<string, string>;
  compose_content?: string;
  compose_file?: string;
  compose_service?: string;
  branch: string;
  build_pack: string;
  image_ref: string;
  registry_username?: string;
  registry_password?: string;
  server_id: string;
  env: EnvVar[];
  storage: StorageMapping[];
}

export interface UpdateApplicationInput {
  name?: string;
  environment_id?: string;
  branch?: string;
  build_pack?: string;
  image_ref?: string;
  registry_username?: string;
  registry_password?: string;
  server_id?: string;
  dockerfile_content?: string;
  build_args?: Record<string, string>;
  compose_content?: string;
  compose_file?: string;
  compose_service?: string;
  monitor_enabled?: boolean;
  monitor_grace_seconds?: number;
}

export interface RollbackInput {
  deployment_id?: string;
}

interface DomainListEnvelope {
  domains: ApplicationDomain[];
}

interface DomainEnvelope {
  domain: ApplicationDomain;
}

export type DomainProtocol = "http" | "https";

export interface ApplicationDomain {
  id: string;
  application_id: string;
  domain: string;
  is_primary: boolean;
  disabled: boolean;
  port: number;
  service: string;
  protocol: DomainProtocol;
  created_at: string;
  updated_at: string;
}

export interface DomainInput {
  domain: string;
  port: number;
  service?: string;
  protocol: DomainProtocol;
}

export async function listDomains(appId: string): Promise<ApplicationDomain[]> {
  const response = await http.get<DomainListEnvelope>(
    `/applications/${appId}/domains`,
  );
  return response.data.domains ?? [];
}

export async function addDomain(
  appId: string,
  input: DomainInput,
): Promise<ApplicationDomain> {
  const response = await http.post<DomainEnvelope>(
    `/applications/${appId}/domains`,
    input,
  );
  return response.data.domain;
}

export async function updateDomain(
  appId: string,
  domainId: string,
  input: DomainInput,
): Promise<ApplicationDomain> {
  const response = await http.put<DomainEnvelope>(
    `/applications/${appId}/domains/${domainId}`,
    input,
  );
  return response.data.domain;
}

export async function removeDomain(
  appId: string,
  domainId: string,
): Promise<void> {
  await http.delete(`/applications/${appId}/domains/${domainId}`);
}

export async function setPrimaryDomain(
  appId: string,
  domainId: string,
): Promise<void> {
  await http.post(`/applications/${appId}/domains/${domainId}/primary`, {});
}

interface DeploymentEnvelope {
  deployment: Deployment;
}

interface DeploymentListEnvelope {
  deployments: Deployment[];
}

export interface RecentDeployment extends Deployment {
  application_name: string;
  application_repo: string;
  application_branch: string;
}

interface RecentDeploymentListEnvelope {
  deployments: RecentDeployment[];
}

export async function listRecentDeployments(
  limit = 0,
): Promise<RecentDeployment[]> {
  const response = await http.get<RecentDeploymentListEnvelope>("/deployments", {
    params: limit > 0 ? { limit } : {},
  });
  return response.data.deployments ?? [];
}

interface ApplicationListEnvelope {
  applications: Application[];
}

interface ApplicationEnvelope {
  application: Application;
  webhook?: HookOutcome;
}

export interface HookOutcome {
  installed: boolean;
  error?: string;
}

export interface CreatedApplication {
  application: Application;
  webhook?: HookOutcome;
}

interface EnvListEnvelope {
  env: EnvVar[];
}

interface StorageListEnvelope {
  storage: StorageMapping[];
}

const activeStates: ReadonlySet<DeploymentState> = new Set([
  "queued",
  "cloning",
  "building",
  "pushing",
  "starting",
]);

export function isActiveDeployment(deployment: Deployment): boolean {
  return activeStates.has(deployment.state);
}

export function countRunning(
  latestStates: Array<DeploymentState | null | undefined>,
): number {
  return latestStates.filter((state) => state === "running").length;
}

export function deployChannel(serverId: string, deploymentId: string): string {
  return `logs:${serverId}:${deploymentId}`;
}

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

interface DeploymentLogEnvelope {
  log: string;
}

export async function getDeploymentBuildLog(
  appId: string,
  deploymentId: string,
): Promise<string> {
  const response = await http.get<DeploymentLogEnvelope>(
    `/applications/${appId}/deployments/${deploymentId}/logs`,
  );
  return response.data.log ?? "";
}

export interface LatestStates {
  states: Array<DeploymentState | null>;
  failed: number;
}

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

export async function triggerDeploy(appId: string): Promise<Deployment> {
  const response = await http.post<DeploymentEnvelope>(
    `/applications/${appId}/deploy`,
    {},
  );
  return response.data.deployment;
}

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

export async function listApplications(
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
    params,
  });
  return response.data.applications ?? [];
}

export async function getApplication(id: string): Promise<Application> {
  const response = await http.get<ApplicationEnvelope>(`/applications/${id}`);
  return response.data.application;
}

export async function createApplication(
  input: CreateApplicationInput,
): Promise<CreatedApplication> {
  const response = await http.post<ApplicationEnvelope>(
    "/applications",
    input,
  );
  return response.data;
}

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

export async function deleteApplication(id: string): Promise<void> {
  await http.delete(`/applications/${id}`);
}

export async function getEnv(appId: string): Promise<EnvVar[]> {
  const response = await http.get<EnvListEnvelope>(
    `/applications/${appId}/env`,
  );
  return response.data.env ?? [];
}

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

export async function getStorages(appId: string): Promise<StorageMapping[]> {
  const response = await http.get<StorageListEnvelope>(
    `/applications/${appId}/storages`,
  );
  return response.data.storage ?? [];
}

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

export async function listVolumes(appId: string): Promise<VolumeListing> {
  const response = await http.get<VolumeListing>(
    `/applications/${appId}/volumes`,
  );
  return parseWith(volumeListingSchema, response.data, {
    context: "ApplicationVolumeListing",
  });
}

export async function stopApplication(appId: string): Promise<Deployment> {
  const response = await http.post<DeploymentEnvelope>(
    `/applications/${appId}/stop`,
    {},
  );
  return response.data.deployment;
}

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

interface DeployKeyEnvelope {
  deploy_key: DeployKey;
}

export async function getDeployKey(appId: string): Promise<DeployKey> {
  const response = await http.get<DeployKeyEnvelope>(
    `/applications/${appId}/deploy-key`,
  );
  return response.data.deploy_key;
}

export async function createDeployKey(appId: string): Promise<DeployKey> {
  const response = await http.post<DeployKeyEnvelope>(
    `/applications/${appId}/deploy-key`,
    {},
  );
  return response.data.deploy_key;
}

export async function deleteDeployKey(appId: string): Promise<boolean> {
  const response = await http.delete<{ deleted: boolean }>(
    `/applications/${appId}/deploy-key`,
  );
  return response.data.deleted;
}

export interface GitCredentialState {
  has_credential: boolean;
  username?: string;
}

export async function getGitCredential(
  appId: string,
): Promise<GitCredentialState> {
  const response = await http.get<GitCredentialState>(
    `/applications/${appId}/git-credential`,
  );
  return response.data;
}

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

export async function deleteGitCredential(appId: string): Promise<boolean> {
  const response = await http.delete<{ deleted: boolean }>(
    `/applications/${appId}/git-credential`,
  );
  return response.data.deleted;
}

export interface ConnectionResult {
  ok: boolean;
  message: string;
  host?: string;
}

export async function testConnection(
  appId: string,
): Promise<ConnectionResult> {
  const response = await http.post<ConnectionResult>(
    `/applications/${appId}/test-connection`,
    {},
  );
  return response.data;
}

export async function startApplication(appId: string): Promise<Deployment> {
  const response = await http.post<DeploymentEnvelope>(
    `/applications/${appId}/start`,
    {},
  );
  return response.data.deployment;
}

export type ApplicationControlAction = "stop" | "start";

function withRaw(summary: string, raw: string): string {
  return raw ? `${summary} ${raw}` : summary;
}

function conflictSummary(raw: string): string {
  const t = i18n.global.t.bind(i18n.global);
  if (raw === "") {
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
  return String(t("applications.errors.conflict"));
}

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
