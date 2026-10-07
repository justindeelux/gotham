import { http, teamHeaders } from "@/shared/api/http";
import { conflictDetail, isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";

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
  environment_id: string;
  environment_name: string;
  project_id: string;
  /** The Gotham project the environment belongs to. */
  project_name: string;
  engine: string;
  version: string;
  status: DatabaseStatus;
  server_id: string;
  server_name: string;
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
  /** Environment the database belongs to (required since PE-2). */
  environment_id: string;
  server_id: string;
  public_port?: number;
}

/** Body accepted by PATCH /databases/{id}: rename only. */
export interface RenameDatabaseInput {
  name: string;
}

/**
 * Body accepted by PATCH /databases/{id} for the resource settings (see
 * updateRequest). `environment_id` moves the database within the caller's
 * team (409 on a name collision in the target); `server_id` changes the
 * node, which the backend refuses once created (409 `a database cannot
 * change server once created`) — the settings render that refusal inline.
 */
export interface UpdateDatabaseInput {
  name?: string;
  environment_id?: string;
  server_id?: string;
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
 * surfaces. `filter` optionally scopes the list to one environment or
 * project (?environment_id= / ?project_id=).
 */
export async function listDatabases(
  teamId = "",
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
    headers: teamHeaders(teamId),
    params,
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
 * updateDatabase applies a partial update (rename, move environment, change
 * node) to a database the caller owns (PATCH → 200).
 */
export async function updateDatabase(
  id: string,
  input: UpdateDatabaseInput,
): Promise<Database> {
  const response = await http.patch<DatabaseEnvelope>(`/databases/${id}`, input);
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

/**
 * describeDatabaseError maps a thrown error to a user-facing message.
 * Classification stays on the raw status/message (never on translated text).
 * Exact backend refusals (409 with a message) pass through raw so inline
 * guards keep their wording; unknown failures render a localized summary
 * with the useful raw diagnostic retained as plain text.
 */
export function describeDatabaseError(error: unknown): string {
  const t = (key: string): string => String(i18n.global.t(key));
  /** withSummary renders a curated summary, keeping nonempty raw detail. */
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
      // The backend names the refusal exactly (a duplicate name, `a deploy
      // is in progress`, `a database cannot change server once created`), so
      // the message passes through for the move/server-change settings to
      // render inline.
      return (
        conflictDetail(stripErrorPrefix(error.message)) ||
        t("databases.errors.nameTaken")
      );
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
