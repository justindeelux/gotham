import { http } from "@/shared/api/http";
import { conflictDetail, isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import type { RuntimeHealth } from "@/shared/types/runtimeHealth";

export type DatabaseStatus =
  | "creating"
  | "running"
  | "stopped"
  | "error"
  | "deleting";

export type DatabaseEngineName =
  | "postgres"
  | "mysql"
  | "mariadb"
  | "mongodb"
  | "redis";

export interface Database {
  id: string;
  name: string;
  environment_id: string;
  environment_name: string;
  project_id: string;
  project_name: string;
  engine: string;
  version: string;
  status: DatabaseStatus;
  server_id: string;
  server_name: string;
  container_id: string;
  public_port: number;
  volume: string;
  runtime_health?: RuntimeHealth | null;
  created_at: string;
  updated_at: string;
}

export interface DatabaseCredentials {
  username: string;
  password: string;
  database: string;
  root_password?: string;
}

export interface CreateDatabaseInput {
  name: string;
  engine: string;
  version?: string;
  environment_id: string;
  server_id: string;
  public_port?: number;
}

export interface RenameDatabaseInput {
  name: string;
}

export interface UpdateDatabaseInput {
  name?: string;
  environment_id?: string;
  server_id?: string;
  monitor_enabled?: boolean;
  monitor_grace_seconds?: number;
}

interface DatabaseEnvelope {
  database: Database;
}

interface DatabaseListEnvelope {
  databases: Database[];
}

interface CreateDatabaseEnvelope {
  database: Database;
  credentials: DatabaseCredentials;
}

interface CredentialsEnvelope {
  credentials: DatabaseCredentials;
}

const provisioningTimeoutMs = 120_000;

export interface CreatedDatabase {
  database: Database;
  credentials: DatabaseCredentials;
}

export async function listDatabases(
  filter: { environment_id?: string; project_id?: string } = {},
): Promise<Database[]> {
  const params: Record<string, string> = {};
  if (filter.environment_id) {
    params.environment_id = filter.environment_id;
  }
  if (filter.project_id) {
    params.project_id = filter.project_id;
  }
  const response = await http.get<DatabaseListEnvelope>("/databases", {
    params,
  });
  return response.data.databases ?? [];
}

export async function getDatabase(id: string): Promise<Database> {
  const response = await http.get<DatabaseEnvelope>(`/databases/${id}`);
  return response.data.database;
}

export async function createDatabase(
  input: CreateDatabaseInput,
): Promise<CreatedDatabase> {
  const body: Record<string, unknown> = {
    name: input.name,
    engine: input.engine,
    environment_id: input.environment_id,
    server_id: input.server_id,
  };
  if (input.version && input.version.trim() !== "") {
    body.version = input.version.trim();
  }
  if (input.public_port && input.public_port > 0) {
    body.public_port = input.public_port;
  }
  const response = await http.post<CreateDatabaseEnvelope>(
    "/databases",
    body,
    { timeout: provisioningTimeoutMs },
  );
  return {
    database: response.data.database,
    credentials: response.data.credentials,
  };
}

export async function renameDatabase(
  id: string,
  input: RenameDatabaseInput,
): Promise<Database> {
  const response = await http.patch<DatabaseEnvelope>(`/databases/${id}`, {
    name: input.name,
  });
  return response.data.database;
}

export async function updateDatabase(
  id: string,
  input: UpdateDatabaseInput,
): Promise<Database> {
  const response = await http.patch<DatabaseEnvelope>(`/databases/${id}`, input);
  return response.data.database;
}

export async function deleteDatabase(id: string): Promise<void> {
  await http.delete(`/databases/${id}`);
}

export async function getDatabaseCredentials(
  id: string,
): Promise<DatabaseCredentials> {
  const response = await http.get<CredentialsEnvelope>(
    `/databases/${id}/credentials`,
  );
  return response.data.credentials;
}

export async function startDatabase(id: string): Promise<Database> {
  const response = await http.post<DatabaseEnvelope>(`/databases/${id}/start`, {});
  return response.data.database;
}

export async function stopDatabase(id: string): Promise<Database> {
  const response = await http.post<DatabaseEnvelope>(`/databases/${id}/stop`, {});
  return response.data.database;
}

export async function restartDatabase(id: string): Promise<Database> {
  const response = await http.post<DatabaseEnvelope>(
    `/databases/${id}/restart`,
    {},
  );
  return response.data.database;
}

export function describeDatabaseError(error: unknown): string {
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
    if (error.status === 404) {
      return t("databases.errors.databaseNotFound");
    }
    if (error.status === 409) {
      const detail = conflictDetail(stripErrorPrefix(error.message));
      return detail === ""
        ? t("databases.errors.nameTaken")
        : withSummary("databases.errors.requestRefused", detail);
    }
    if (error.status === 502) {
      return withSummary(
        "databases.errors.databaseAgentUnreachable",
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

export function databaseEmptyDescription(totalCount: number): string {
  return totalCount === 0
    ? "No databases yet"
    : "No databases match this filter";
}

export function databaseEmptyHint(totalCount: number): string {
  return totalCount === 0
    ? "Create your first database with the Create database wizard."
    : "Change the filter or search, or create a database with the Create database wizard.";
}
