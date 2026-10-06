import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import { toTargetBody } from "@/features/databases/utils/backupTarget";

/**
 * Typed client for the backup routes served by `internal/databases`
 * (see backup_routes.go for the contract):
 *
 *   POST   /databases/{id}/backup
 *   POST   /databases/{id}/restore
 *   GET    /databases/{id}/backups
 *   GET    /databases/{id}/backups/{backupId}
 *   DELETE /databases/{id}/backups/{backupId}
 *   GET    /databases/{id}/schedules
 *   POST   /databases/{id}/schedules
 *   PATCH  /databases/{id}/schedules/{scheduleId}
 *   DELETE /databases/{id}/schedules/{scheduleId}
 *   GET    /databases/backup-targets
 *   POST   /databases/backup-targets
 *   PATCH  /databases/backup-targets/{targetId}
 *   DELETE /databases/backup-targets/{targetId}
 *   POST   /databases/backup-targets/{targetId}/test
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * Storage credentials never appear on these rows: targets report only
 * `has_credentials`, and an empty credential field on update leaves the stored
 * secret untouched.
 */

/** What triggered a backup run (see backup_model.go). */
export type BackupRunType = "manual" | "scheduled";

/** Lifecycle of one backup run: running → completed | failed. */
export type BackupRunStatus = "running" | "completed" | "failed";

/** Storage backend of a backup target (see backup_model.go). */
export type BackupTargetKind = "s3" | "local";

/** One backup run as returned by the control-plane API. */
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

/** One cron entry of the automatic backups. */
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

/**
 * One storage destination. The credentials are reported only as
 * `has_credentials` — per `newTargetResponse` this is derived from the kind
 * (an s3 target always carries sealed credentials, a local one never does),
 * so secrets are never echoed back.
 */
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

/** A queued restore as answered by POST .../restore (HTTP 202). */
export interface RestoreResult {
  restore_id: string;
  backup_id: string;
  database_id: string;
  location: string;
  status: BackupRunStatus;
}

/** Lifecycle of a durable restore run (see backup_model.go RestoreStatus). */
export type RestoreStatus = "running" | "completed" | "failed";

/**
 * One durable restore run read back from GET .../restores. Unlike the 202
 * answer this row survives, so a client can follow a queued restore to its
 * completed/failed state.
 */
export interface DatabaseRestore {
  id: string;
  database_id: string;
  backup_id: string;
  status: RestoreStatus;
  error?: string;
  created_at: string;
  finished_at?: string;
}

/** The answer of the "test connection" action (always HTTP 200). */
export interface TargetCheck {
  ok: boolean;
  message: string;
}

/** Body accepted by POST .../schedules. */
export interface CreateBackupScheduleInput {
  cron: string;
  target_id?: string;
  enabled?: boolean;
}

/**
 * Body accepted by PATCH .../schedules/{scheduleId}. The cron expression is
 * required: the server always recomputes the next run from it, so callers
 * toggling `enabled` must resend the schedule's current cron.
 */
export interface UpdateBackupScheduleInput {
  cron: string;
  target_id?: string;
  enabled?: boolean;
}

/** Body accepted by POST /databases/backup-targets. */
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

/**
 * Body accepted by PATCH /databases/backup-targets/{targetId}. Empty fields
 * leave the stored values untouched — in particular, blank credentials never
 * wipe a stored key.
 */
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

/** Wire envelope for a single backup. */
interface BackupEnvelope {
  backup: DatabaseBackup;
}

/** Wire envelope for a backup list. */
interface BackupListEnvelope {
  backups: DatabaseBackup[];
}

/** Wire envelope for a single schedule. */
interface ScheduleEnvelope {
  schedule: BackupSchedule;
}

/** Wire envelope for a schedule list. */
interface ScheduleListEnvelope {
  schedules: BackupSchedule[];
}

/** Wire envelope for a single target. */
interface TargetEnvelope {
  target: BackupTarget;
}

/** Wire envelope for a target list. */
interface TargetListEnvelope {
  targets: BackupTarget[];
}

/** Wire envelope for a queued restore. */
interface RestoreEnvelope {
  restore: RestoreResult;
}

/** Wire envelope for a restore list. */
interface RestoreListEnvelope {
  restores: DatabaseRestore[];
}

/** Wire envelope for a connection test. */
interface TargetCheckEnvelope {
  check: TargetCheck;
}

/** listBackups returns the backup runs of one database, newest first. */
export async function listBackups(
  databaseId: string,
): Promise<DatabaseBackup[]> {
  const response = await http.get<BackupListEnvelope>(
    `/databases/${databaseId}/backups`,
  );
  return response.data.backups ?? [];
}

/**
 * createBackup queues a manual backup and answers 202 with the recorded
 * `running` row. Without a target the artifact stays in the local backup
 * directory of the control plane.
 */
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

/**
 * deleteBackup removes the stored artifact first, then the row.
 * Answers 204. A still-running backup answers 409.
 */
export async function deleteBackup(
  databaseId: string,
  backupId: string,
): Promise<void> {
  await http.delete(`/databases/${databaseId}/backups/${backupId}`);
}

/**
 * restoreBackup queues a restore of a completed backup and answers 202.
 * Only completed backups can be restored; anything else answers 409.
 */
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

/**
 * listRestores returns the durable restore runs of one database, newest first.
 * The 202 from restoreBackup is only the queue acknowledgement; this is how a
 * caller learns whether the restore completed or failed.
 */
export async function listRestores(
  databaseId: string,
): Promise<DatabaseRestore[]> {
  const response = await http.get<RestoreListEnvelope>(
    `/databases/${databaseId}/restores`,
  );
  return response.data.restores ?? [];
}

/** listSchedules returns the cron entries of one database. */
export async function listSchedules(
  databaseId: string,
): Promise<BackupSchedule[]> {
  const response = await http.get<ScheduleListEnvelope>(
    `/databases/${databaseId}/schedules`,
  );
  return response.data.schedules ?? [];
}

/**
 * createSchedule stores a cron entry and answers 201 with the row and its
 * computed next run. Optional fields are omitted when empty.
 */
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

/** updateSchedule changes one cron entry (cron is always required). */
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

/** deleteSchedule removes one cron entry. Answers 204. */
export async function deleteSchedule(
  databaseId: string,
  scheduleId: string,
): Promise<void> {
  await http.delete(`/databases/${databaseId}/schedules/${scheduleId}`);
}

/** listTargets returns the caller's storage targets (never credentials). */
export async function listTargets(): Promise<BackupTarget[]> {
  const response = await http.get<TargetListEnvelope>(
    "/databases/backup-targets",
  );
  return response.data.targets ?? [];
}

/**
 * createTarget stores a target and seals the credentials it was given,
 * answering 201. Optional fields are omitted when empty.
 */
export async function createTarget(
  input: CreateBackupTargetInput,
): Promise<BackupTarget> {
  const response = await http.post<TargetEnvelope>(
    "/databases/backup-targets",
    toTargetBody(input),
  );
  return response.data.target;
}

/**
 * updateTarget changes a target the caller owns. Blank fields are omitted so
 * the server keeps the stored values — resending a form with empty credential
 * inputs cannot wipe a stored key.
 */
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

/** deleteTarget removes a target the caller owns. Answers 204. */
export async function deleteTarget(targetId: string): Promise<void> {
  await http.delete(`/databases/backup-targets/${targetId}`);
}

/**
 * testTarget verifies the target can be reached and its bucket exists. A
 * failed check is still HTTP 200 with `ok: false` — the request worked, the
 * answer is just negative.
 */
export async function testTarget(targetId: string): Promise<TargetCheck> {
  const response = await http.post<TargetCheckEnvelope>(
    `/databases/backup-targets/${targetId}/test`,
    {},
  );
  return response.data.check;
}

/**
 * describeBackupError maps a thrown error to a user-facing message.
 * Classification stays on the raw status/message (never on translated text);
 * only the curated summaries resolve through the current locale, while the
 * useful raw diagnostic from stripErrorPrefix rides along untranslated.
 */
export function describeBackupError(error: unknown): string {
  const t = (key: string): string => String(i18n.global.t(key));
  if (isApiError(error)) {
    if (error.status === 400) {
      return (
        stripErrorPrefix(error.message) ||
        t("databases.errors.backupInvalidRequest")
      );
    }
    if (error.status === 401) {
      return t("databases.errors.sessionExpired");
    }
    if (error.status === 404) {
      return t("databases.errors.backupNotFound");
    }
    if (error.status === 409) {
      return (
        stripErrorPrefix(error.message) ||
        t("databases.errors.backupConflict")
      );
    }
    if (error.status === 502) {
      return t("databases.errors.backupAgentUnreachable");
    }
    if (error.status === 503) {
      return t("databases.errors.featureDisabled");
    }
    return (
      stripErrorPrefix(error.message) || t("common.errors.requestFailed")
    );
  }
  if (error instanceof Error) {
    return (
      stripErrorPrefix(error.message) || t("common.errors.unexpected")
    );
  }
  return t("common.errors.unexpected");
}
