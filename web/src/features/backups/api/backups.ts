import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import { toProviderBody } from "@/features/backups/utils/providerBody";

export type ResourceType = "application" | "database" | "service";

export interface ResourceRef {
  resource_type: ResourceType;
  resource_id: string;
}

export type BackupRunType = "manual" | "scheduled";

export type BackupRunStatus = "running" | "completed" | "failed";

export type StorageProviderKind = "s3" | "local";

export interface BackupRun {
  id: string;
  resource_type: ResourceType;
  resource_id: string;
  schedule_id?: string;
  provider_id?: string;
  provider_name?: string;
  type: BackupRunType;
  status: BackupRunStatus;
  size: number;
  location?: string;
  error?: string;
  container_id?: string;
  created_at: string;
  finished_at?: string;
}

export interface BackupSchedule {
  id: string;
  resource_type: ResourceType;
  resource_id: string;
  cron: string;
  provider_id?: string;
  enabled: boolean;
  next_run_at: string;
  last_run_at?: string;
  last_slot_at?: string;
  max_run_seconds: number;
  pause_during_backup: boolean;
  created_at: string;
  updated_at: string;
}

export type RestoreStatus = "running" | "completed" | "failed";

export interface BackupRestore {
  id: string;
  resource_type: ResourceType;
  resource_id: string;
  backup_id: string;
  status: RestoreStatus;
  error?: string;
  created_at: string;
  finished_at?: string;
}

export interface RestoreResult {
  restore_id: string;
  backup_id: string;
  resource_type: ResourceType;
  resource_id: string;
  status: RestoreStatus;
}

export interface StorageProvider {
  id: string;
  name: string;
  kind: StorageProviderKind;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  builtin: boolean;
  has_credentials: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProviderCheck {
  ok: boolean;
  message: string;
}

export interface CreateProviderInput {
  name: string;
  kind: StorageProviderKind;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  access_key?: string;
  secret_key?: string;
}

export interface UpdateProviderInput extends CreateProviderInput {}

export interface CreateScheduleInput {
  resource: ResourceRef;
  cron: string;
  provider_id?: string;
  enabled?: boolean;
  max_run_seconds?: number;
  pause_during_backup?: boolean;
}

export interface UpdateScheduleInput {
  cron: string;
  provider_id?: string;
  enabled?: boolean;
  max_run_seconds?: number;
  pause_during_backup?: boolean;
}

export interface BackupContent {
  name: string;
  kind: "volume" | "bind" | "dump";
  target?: string;
}

export type BackupProtectionStatus =
  | "not_configured"
  | "scheduled"
  | "pending"
  | "protected"
  | "running"
  | "overdue"
  | "stuck";

export interface BackupProtectionSchedule {
  schedule_id: string;
  cron: string;
  target_id?: string;
  target_name?: string;
  status: BackupProtectionStatus;
  last_success_at?: string;
  last_backup_id?: string;
  expected_slot_at?: string;
  deadline_at?: string;
  overdue_since?: string;
  overdue_seconds: number;
  running_since?: string;
  running_backup_id?: string;
  max_run_seconds: number;
}

export interface BackupProtectionSummary {
  resource_type: "application" | "database" | "service";
  resource_id: string;
  database_id?: string;
  status: BackupProtectionStatus;
  schedules: BackupProtectionSchedule[];
}

interface BackupEnvelope {
  backup: BackupRun;
}

interface BackupListEnvelope {
  backups: BackupRun[];
}

interface RestoreEnvelope {
  restore: RestoreResult;
}

interface RestoreListEnvelope {
  restores: BackupRestore[];
}

interface ScheduleEnvelope {
  schedule: BackupSchedule;
}

interface ScheduleListEnvelope {
  schedules: BackupSchedule[];
}

interface ProviderEnvelope {
  provider: StorageProvider;
}

interface ProviderListEnvelope {
  providers: StorageProvider[];
}

interface ProviderCheckEnvelope {
  check: ProviderCheck;
}

interface BackupProtectionEnvelope {
  protection: BackupProtectionSummary;
}

function resourceParams(ref: ResourceRef): Record<string, string> {
  return {
    resource_type: ref.resource_type,
    resource_id: ref.resource_id,
  };
}

export async function listBackups(filter: {
  resource?: ResourceRef;
  status?: BackupRunStatus;
  limit?: number;
  offset?: number;
} = {}): Promise<BackupRun[]> {
  const params: Record<string, string | number> = {};
  if (filter.resource) {
    Object.assign(params, resourceParams(filter.resource));
  }
  if (filter.status) {
    params.status = filter.status;
  }
  if (filter.limit !== undefined) {
    params.limit = filter.limit;
  }
  if (filter.offset !== undefined) {
    params.offset = filter.offset;
  }
  const response = await http.get<BackupListEnvelope>("/backups", { params });
  return response.data.backups ?? [];
}

export async function createBackup(
  resource: ResourceRef,
  providerId?: string,
  pauseDuringBackup?: boolean,
): Promise<BackupRun> {
  const body: Record<string, unknown> = { ...resource };
  if (providerId && providerId.trim() !== "") {
    body.provider_id = providerId;
  }
  if (pauseDuringBackup !== undefined) {
    body.pause_during_backup = pauseDuringBackup;
  }
  const response = await http.post<BackupEnvelope>("/backups", body);
  return response.data.backup;
}

export async function deleteBackup(backupId: string): Promise<void> {
  await http.delete(`/backups/${backupId}`);
}

export async function restoreBackup(backupId: string): Promise<RestoreResult> {
  const response = await http.post<RestoreEnvelope>(
    `/backups/${backupId}/restore`,
    {},
  );
  return response.data.restore;
}

export async function listRestores(
  resource?: ResourceRef,
): Promise<BackupRestore[]> {
  const response = await http.get<RestoreListEnvelope>("/backups/restores", {
    params: resource ? resourceParams(resource) : undefined,
  });
  return response.data.restores ?? [];
}

export async function listSchedules(
  resource?: ResourceRef,
): Promise<BackupSchedule[]> {
  const response = await http.get<ScheduleListEnvelope>("/backups/schedules", {
    params: resource ? resourceParams(resource) : undefined,
  });
  return response.data.schedules ?? [];
}

function scheduleBody(input: UpdateScheduleInput): Record<string, unknown> {
  const body: Record<string, unknown> = { cron: input.cron };
  if (input.provider_id !== undefined) {
    body.provider_id = input.provider_id;
  }
  if (input.enabled !== undefined) {
    body.enabled = input.enabled;
  }
  if (input.max_run_seconds !== undefined) {
    body.max_run_seconds = input.max_run_seconds;
  }
  if (input.pause_during_backup !== undefined) {
    body.pause_during_backup = input.pause_during_backup;
  }
  return body;
}

export async function createSchedule(
  input: CreateScheduleInput,
): Promise<BackupSchedule> {
  const response = await http.post<ScheduleEnvelope>(
    "/backups/schedules",
    { ...scheduleBody(input), ...input.resource },
  );
  return response.data.schedule;
}

export async function updateSchedule(
  scheduleId: string,
  input: UpdateScheduleInput,
): Promise<BackupSchedule> {
  const response = await http.patch<ScheduleEnvelope>(
    `/backups/schedules/${scheduleId}`,
    scheduleBody(input),
  );
  return response.data.schedule;
}

export async function getBackupContents(
  resourceType: ResourceType,
  resourceId: string,
): Promise<BackupContent[]> {
  const resource = `${resourceType}s`;
  const response = await http.get<{ contents: BackupContent[] }>(
    `/${resource}/${resourceId}/backup-contents`,
  );
  return response.data.contents ?? [];
}

export async function deleteSchedule(scheduleId: string): Promise<void> {
  await http.delete(`/backups/schedules/${scheduleId}`);
}

export async function listProviders(): Promise<StorageProvider[]> {
  const response = await http.get<ProviderListEnvelope>("/backups/providers");
  return response.data.providers ?? [];
}

export async function createProvider(
  input: CreateProviderInput,
): Promise<StorageProvider> {
  const response = await http.post<ProviderEnvelope>(
    "/backups/providers",
    toProviderBody(input),
  );
  return response.data.provider;
}

export async function updateProvider(
  providerId: string,
  input: UpdateProviderInput,
): Promise<StorageProvider> {
  const response = await http.patch<ProviderEnvelope>(
    `/backups/providers/${providerId}`,
    toProviderBody(input),
  );
  return response.data.provider;
}

export async function deleteProvider(providerId: string): Promise<void> {
  await http.delete(`/backups/providers/${providerId}`);
}

export async function testProvider(
  providerId: string,
): Promise<ProviderCheck> {
  const response = await http.post<ProviderCheckEnvelope>(
    `/backups/providers/${providerId}/test`,
    {},
  );
  return response.data.check;
}

export type BackupProtectionResource = "applications" | "databases" | "services";

export async function getResourceBackupProtection(
  resource: BackupProtectionResource,
  resourceId: string,
): Promise<BackupProtectionSummary> {
  const response = await http.get<BackupProtectionEnvelope>(
    `/${resource}/${resourceId}/backup-protection`,
  );
  return response.data.protection;
}

export async function getBackupProtection(
  databaseId: string,
): Promise<BackupProtectionSummary> {
  return getResourceBackupProtection("databases", databaseId);
}

export function describeBackupError(error: unknown): string {
  const t = (key: string): string => String(i18n.global.t(key));
  const withSummary = (summaryKey: string, raw: string): string =>
    raw === ""
      ? t(summaryKey)
      : String(
          i18n.global.t("databases.errors.withDetail", {
            summary: t(summaryKey),
            detail: raw,
          }),
        );
  if (isApiError(error)) {
    if (error.status === 400) {
      const raw = stripErrorPrefix(error.message);
      return raw === ""
        ? t("databases.errors.backupInvalidRequest")
        : withSummary("databases.errors.requestRefused", raw);
    }
    if (error.status === 401) {
      return t("databases.errors.sessionExpired");
    }
    if (error.status === 404) {
      return t("databases.errors.backupNotFound");
    }
    if (error.status === 409) {
      const raw = stripErrorPrefix(error.message);
      return raw === ""
        ? t("databases.errors.backupConflict")
        : withSummary("databases.errors.requestRefused", raw);
    }
    if (error.status === 502) {
      return withSummary(
        "databases.errors.backupAgentUnreachable",
        stripErrorPrefix(error.message),
      );
    }
    if (error.status === 503) {
      return t("databases.errors.featureDisabled");
    }
    return withSummary(
      "common.errors.requestFailed",
      stripErrorPrefix(error.message),
    );
  }
  if (error instanceof Error) {
    return withSummary(
      "common.errors.unexpected",
      stripErrorPrefix(error.message),
    );
  }
  return t("common.errors.unexpected");
}
