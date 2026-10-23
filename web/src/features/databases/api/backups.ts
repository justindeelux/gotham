import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import { toTargetBody } from "@/features/databases/utils/backupTarget";

export type BackupRunType = "manual" | "scheduled";

export type BackupRunStatus = "running" | "completed" | "failed";

export type BackupTargetKind = "s3" | "local";

export interface DatabaseBackup {
  id: string;
  database_id: string;
  schedule_id?: string;
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
  database_id: string;
  cron: string;
  target_id?: string;
  enabled: boolean;
  next_run_at: string;
  last_run_at?: string;
  created_at: string;
  updated_at: string;
}

export interface BackupTarget {
  id: string;
  name: string;
  kind: BackupTargetKind;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  has_credentials: boolean;
  created_at: string;
  updated_at: string;
}

export interface RestoreResult {
  restore_id: string;
  backup_id: string;
  database_id: string;
  location: string;
  status: BackupRunStatus;
}

export type RestoreStatus = "running" | "completed" | "failed";

export interface DatabaseRestore {
  id: string;
  database_id: string;
  backup_id: string;
  status: RestoreStatus;
  error?: string;
  created_at: string;
  finished_at?: string;
}

export interface TargetCheck {
  ok: boolean;
  message: string;
}

export interface CreateBackupScheduleInput {
  cron: string;
  target_id?: string;
  enabled?: boolean;
}

export interface UpdateBackupScheduleInput {
  cron: string;
  target_id?: string;
  enabled?: boolean;
}

export interface CreateBackupTargetInput {
  name: string;
  kind: BackupTargetKind;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  access_key?: string;
  secret_key?: string;
}

export interface UpdateBackupTargetInput {
  name?: string;
  kind?: BackupTargetKind;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  access_key?: string;
  secret_key?: string;
}

interface BackupEnvelope {
  backup: DatabaseBackup;
}

interface BackupListEnvelope {
  backups: DatabaseBackup[];
}

interface ScheduleEnvelope {
  schedule: BackupSchedule;
}

interface ScheduleListEnvelope {
  schedules: BackupSchedule[];
}

interface TargetEnvelope {
  target: BackupTarget;
}

interface TargetListEnvelope {
  targets: BackupTarget[];
}

interface RestoreEnvelope {
  restore: RestoreResult;
}

interface RestoreListEnvelope {
  restores: DatabaseRestore[];
}

interface TargetCheckEnvelope {
  check: TargetCheck;
}

export async function listBackups(
  databaseId: string,
): Promise<DatabaseBackup[]> {
  const response = await http.get<BackupListEnvelope>(
    `/databases/${databaseId}/backups`,
  );
  return response.data.backups ?? [];
}

export async function createBackup(
  databaseId: string,
  targetId?: string,
): Promise<DatabaseBackup> {
  const body: Record<string, unknown> =
    targetId && targetId.trim() !== "" ? { target_id: targetId } : {};
  const response = await http.post<BackupEnvelope>(
    `/databases/${databaseId}/backup`,
    body,
  );
  return response.data.backup;
}

export async function deleteBackup(
  databaseId: string,
  backupId: string,
): Promise<void> {
  await http.delete(`/databases/${databaseId}/backups/${backupId}`);
}

export async function restoreBackup(
  databaseId: string,
  backupId: string,
): Promise<RestoreResult> {
  const response = await http.post<RestoreEnvelope>(
    `/databases/${databaseId}/restore`,
    { backup_id: backupId },
  );
  return response.data.restore;
}

export async function listRestores(
  databaseId: string,
): Promise<DatabaseRestore[]> {
  const response = await http.get<RestoreListEnvelope>(
    `/databases/${databaseId}/restores`,
  );
  return response.data.restores ?? [];
}

export async function listSchedules(
  databaseId: string,
): Promise<BackupSchedule[]> {
  const response = await http.get<ScheduleListEnvelope>(
    `/databases/${databaseId}/schedules`,
  );
  return response.data.schedules ?? [];
}

export async function createSchedule(
  databaseId: string,
  input: CreateBackupScheduleInput,
): Promise<BackupSchedule> {
  const body: Record<string, unknown> = { cron: input.cron };
  if (input.target_id && input.target_id.trim() !== "") {
    body.target_id = input.target_id;
  }
  if (input.enabled !== undefined) {
    body.enabled = input.enabled;
  }
  const response = await http.post<ScheduleEnvelope>(
    `/databases/${databaseId}/schedules`,
    body,
  );
  return response.data.schedule;
}

export async function updateSchedule(
  databaseId: string,
  scheduleId: string,
  input: UpdateBackupScheduleInput,
): Promise<BackupSchedule> {
  const body: Record<string, unknown> = { cron: input.cron };
  if (input.target_id !== undefined) {
    body.target_id = input.target_id;
  }
  if (input.enabled !== undefined) {
    body.enabled = input.enabled;
  }
  const response = await http.patch<ScheduleEnvelope>(
    `/databases/${databaseId}/schedules/${scheduleId}`,
    body,
  );
  return response.data.schedule;
}

export async function deleteSchedule(
  databaseId: string,
  scheduleId: string,
): Promise<void> {
  await http.delete(`/databases/${databaseId}/schedules/${scheduleId}`);
}

export async function listTargets(): Promise<BackupTarget[]> {
  const response = await http.get<TargetListEnvelope>(
    "/databases/backup-targets",
  );
  return response.data.targets ?? [];
}

export async function createTarget(
  input: CreateBackupTargetInput,
): Promise<BackupTarget> {
  const response = await http.post<TargetEnvelope>(
    "/databases/backup-targets",
    toTargetBody(input),
  );
  return response.data.target;
}

export async function updateTarget(
  targetId: string,
  input: UpdateBackupTargetInput,
): Promise<BackupTarget> {
  const response = await http.patch<TargetEnvelope>(
    `/databases/backup-targets/${targetId}`,
    toTargetBody(input),
  );
  return response.data.target;
}

export async function deleteTarget(targetId: string): Promise<void> {
  await http.delete(`/databases/backup-targets/${targetId}`);
}

export async function testTarget(targetId: string): Promise<TargetCheck> {
  const response = await http.post<TargetCheckEnvelope>(
    `/databases/backup-targets/${targetId}/test`,
    {},
  );
  return response.data.check;
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
