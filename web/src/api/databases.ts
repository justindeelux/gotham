import { http, teamHeaders } from "./http";
import { isApiError } from "./servers";

/**
 * Typed client for the database routes served by `internal/databases`
 * (see routes.go for the contract):
 *
 *   POST   /databases
 *   GET    /databases
 *   GET    /databases/{id}
 *   PATCH  /databases/{id}
 *   DELETE /databases/{id}
 *   GET    /databases/{id}/credentials
 *   POST   /databases/{id}/start
 *   POST   /databases/{id}/stop
 *   POST   /databases/{id}/restart
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 *
 * Credentials never appear on the database rows: the list and detail
 * endpoints carry no secrets, and the owner reads them through the dedicated
 * credentials endpoint (or the create response). Backup, restore, schedule
 * and target endpoints live in `./backups`.
 */

/** Lifecycle of a managed database (see model.go). */
export type DatabaseStatus =
  | "creating"
  | "running"
  | "stopped"
  | "error"
  | "deleting";

/** Engines accepted by the API (see engine.go). */
export type DatabaseEngineName =
  | "postgres"
  | "mysql"
  | "mariadb"
  | "mongodb"
  | "redis";

/** One managed database as returned by the control-plane API. */
export interface Database {
  id: string;
  name: string;
  engine: string;
  version: string;
  status: DatabaseStatus;
  server_id: string;
  container_id: string;
  public_port: number;
  volume: string;
  created_at: string;
  updated_at: string;
}

/**
 * Generated login details of one database. Shown to the owner only: the
 * create response and the credentials endpoint (see engine.go).
 */
export interface DatabaseCredentials {
  username: string;
  password: string;
  database: string;
  root_password?: string;
}

/** Body accepted by POST /databases (see routes.go createRequest). */
export interface CreateDatabaseInput {
  name: string;
  engine: string;
  version?: string;
  server_id: string;
  public_port?: number;
}

/** Body accepted by PATCH /databases/{id}: rename only. */
export interface RenameDatabaseInput {
  name: string;
}

/** Wire envelope for a single database. */
interface DatabaseEnvelope {
  database: Database;
}

/** Wire envelope for a database list. */
interface DatabaseListEnvelope {
  databases: Database[];
}

/** Wire envelope for POST /databases: the row plus its credentials. */
interface CreateDatabaseEnvelope {
  database: Database;
  credentials: DatabaseCredentials;
}

/** Wire envelope for the owner-only credentials endpoint. */
interface CredentialsEnvelope {
  credentials: DatabaseCredentials;
}

/**
 * Provisioning pulls an engine image and starts a container, which waits on
 * the node agent for 30-90s — far beyond the shared 15s request timeout. The
 * create call therefore overrides the timeout for itself only.
 */
const provisioningTimeoutMs = 120_000;

/** Created database together with the credentials generated for it. */
export interface CreatedDatabase {
  database: Database;
  credentials: DatabaseCredentials;
}

/**
 * listDatabases returns one team's live databases, newest first. An empty
 * teamId reads the caller's personal team, matching the other pre-teams
 * surfaces.
 */
export async function listDatabases(teamId = ""): Promise<Database[]> {
  const response = await http.get<DatabaseListEnvelope>("/databases", {
    headers: teamHeaders(teamId),
  });
  return response.data.databases ?? [];
}

/** getDatabase returns one database the caller owns. */
export async function getDatabase(id: string): Promise<Database> {
  const response = await http.get<DatabaseEnvelope>(`/databases/${id}`);
  return response.data.database;
}

/**
 * createDatabase provisions a database and returns the row with the
 * credentials it was created with. Optional fields are omitted when empty:
 * the backend rejects unknown fields and treats an empty version as the
 * engine default.
 */
export async function createDatabase(
  input: CreateDatabaseInput,
): Promise<CreatedDatabase> {
  const body: Record<string, unknown> = {
    name: input.name,
    engine: input.engine,
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

/** renameDatabase renames a database the caller owns. */
export async function renameDatabase(
  id: string,
  input: RenameDatabaseInput,
): Promise<Database> {
  const response = await http.patch<DatabaseEnvelope>(`/databases/${id}`, {
    name: input.name,
  });
  return response.data.database;
}

/**
 * deleteDatabase stops and removes the container, then soft-deletes the row.
 * The named volume is kept for the 7-day grace window. Answers 204.
 */
export async function deleteDatabase(id: string): Promise<void> {
  await http.delete(`/databases/${id}`);
}

/** getDatabaseCredentials returns the decrypted credentials of an owned database. */
export async function getDatabaseCredentials(
  id: string,
): Promise<DatabaseCredentials> {
  const response = await http.get<CredentialsEnvelope>(
    `/databases/${id}/credentials`,
  );
  return response.data.credentials;
}

/** startDatabase powers the container back on. */
export async function startDatabase(id: string): Promise<Database> {
  const response = await http.post<DatabaseEnvelope>(`/databases/${id}/start`, {});
  return response.data.database;
}

/** stopDatabase shuts the container down; the volume keeps the data. */
export async function stopDatabase(id: string): Promise<Database> {
  const response = await http.post<DatabaseEnvelope>(`/databases/${id}/stop`, {});
  return response.data.database;
}

/** restartDatabase re-runs the container in place. */
export async function restartDatabase(id: string): Promise<Database> {
  const response = await http.post<DatabaseEnvelope>(
    `/databases/${id}/restart`,
    {},
  );
  return response.data.database;
}

/** describeDatabaseError maps a thrown error to a user-facing message. */
export function describeDatabaseError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 404) {
      return "Database not found. It may have been deleted or belong to another account.";
    }
    if (error.status === 409) {
      return "A database with that name already exists.";
    }
    if (error.status === 502) {
      return "The node agent is unreachable or the healthcheck failed. Check the node status and retry.";
    }
    if (error.status === 503) {
      return "Databases are disabled on the control plane (FEATURE_DATABASES=false).";
    }
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}

/**
 * databaseEmptyDescription picks the honest empty state for the managed
 * databases table: "none exist yet" when the team owns no databases at all,
 * "no match" only when a filter or search hides existing ones. Pure so the
 * page and the harness share one definition.
 */
export function databaseEmptyDescription(totalCount: number): string {
  return totalCount === 0
    ? "No databases yet"
    : "No databases match this filter";
}

/**
 * databaseEmptyHint picks the matching follow-up line for the empty state.
 */
export function databaseEmptyHint(totalCount: number): string {
  return totalCount === 0
    ? "Create your first database with the Create database wizard."
    : "Change the filter or search, or create a database with the Create database wizard.";
}
