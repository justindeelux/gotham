import { isApiError } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import { http, teamHeaders } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type {
  FilesContent,
  FilesEntry,
  FilesListing,
  FilesRoot,
} from "@/features/files/schemas/files";
import {
  downloadTicketSchema,
  filesContentSchema,
  filesListingSchema,
  filesRootListSchema,
  filesStatSchema,
} from "@/features/files/schemas/files";

/**
 * Typed read-only client for the file routes served by `internal/files`
 * (see routes.go for the contract):
 *
 *   GET  /servers/{id}/files/roots           any member
 *   GET  /servers/{id}/files/list            any member
 *   GET  /servers/{id}/files/stat            any member
 *   GET  /servers/{id}/files/content         owner/admin
 *   POST /servers/{id}/files/download-ticket owner/admin -> single-use ticket
 *   GET  /servers/{id}/files/download?ticket ticket (native browser download)
 *
 * The routes are team-scoped: the control plane resolves the active team from
 * the optional `X-Team-Id` header (the caller's personal team when absent) and
 * 404s a foreign or team-less server, so every call carries the team the UI
 * selected. FEATURE_FILES=false answers 503 on every route; the browser hides
 * its surface behind the disabled state instead of rendering it as an error.
 *
 * Write endpoints (upload, rename, delete, chmod) are not exposed here — this
 * module is the read-only foundation (P18 step 6).
 */

/** listRoots returns the agent's browsable roots for one server. */
export async function listRoots(
  serverId: string,
  teamId = "",
): Promise<FilesRoot[]> {
  const response = await http.get(`/servers/${serverId}/files/roots`, {
    headers: teamHeaders(teamId),
  });
  const data = parseWith(filesRootListSchema, response.data, {
    context: "FilesRootList",
  });
  return Array.isArray(data.roots) ? data.roots : [];
}

/** Options for one directory listing page. */
export interface ListEntriesOptions {
  root: string;
  path?: string;
  /** Opaque pagination cursor from the previous page ("" for the first). */
  cursor?: string;
}

/** listEntries reads one page of a directory (≤ 5,000 entries). */
export async function listEntries(
  serverId: string,
  teamId: string,
  options: ListEntriesOptions,
): Promise<FilesListing> {
  const response = await http.get(`/servers/${serverId}/files/list`, {
    params: {
      root: options.root,
      path: options.path ?? "",
      cursor: options.cursor ?? "",
    },
    headers: teamHeaders(teamId),
  });
  const data = parseWith(filesListingSchema, response.data, {
    context: "FilesListing",
  });
  return {
    entries: Array.isArray(data.entries) ? data.entries : [],
    truncated: data.truncated === true,
    cursor: typeof data.cursor === "string" ? data.cursor : "",
  };
}

/** statEntry reads one entry's metadata. */
export async function statEntry(
  serverId: string,
  teamId: string,
  root: string,
  path: string,
): Promise<FilesEntry | null> {
  const response = await http.get(`/servers/${serverId}/files/stat`, {
    params: { root, path },
    headers: teamHeaders(teamId),
  });
  const data = parseWith(filesStatSchema, response.data, {
    context: "FilesStat",
  });
  return data.entry ?? null;
}

/** getContent reads one editable text body (owner/admin only, ≤ 2 MiB). */
export async function getContent(
  serverId: string,
  teamId: string,
  root: string,
  path: string,
): Promise<FilesContent> {
  const response = await http.get(`/servers/${serverId}/files/content`, {
    params: { root, path },
    headers: teamHeaders(teamId),
  });
  return parseWith(filesContentSchema, response.data, {
    context: "FilesContent",
  });
}

/**
 * issueDownloadTicket mints a single-use, short-lived ticket for one file. The
 * ticket is the only credential on the unauthenticated download route, because
 * a native browser download cannot attach the bearer token.
 */
export async function issueDownloadTicket(
  serverId: string,
  teamId: string,
  root: string,
  path: string,
): Promise<string> {
  const response = await http.post(
    `/servers/${serverId}/files/download-ticket`,
    null,
    { params: { root, path }, headers: teamHeaders(teamId) },
  );
  const data = parseWith(downloadTicketSchema, response.data, {
    context: "DownloadTicket",
  });
  return typeof data.ticket === "string" ? data.ticket : "";
}

/**
 * downloadUrl builds the ticket-authorised download URL. It is addressed
 * against the current origin (`/api/v1…`), never the axios baseURL, because a
 * native navigation does not pass through the axios interceptors.
 */
export function downloadUrl(serverId: string, ticket: string): string {
  return `/api/v1/servers/${serverId}/files/download?ticket=${encodeURIComponent(ticket)}`;
}

/** filesErrorKey maps an HTTP status onto its files catalog key. */
export function filesErrorKey(error: unknown): string | null {
  if (isApiError(error) && typeof error.status === "number") {
    return statusKeys[error.status] ?? null;
  }
  return null;
}

/**
 * describeFilesError renders a failure in the active locale. Classification
 * reads the raw HTTP status only; the server's English `{message}` is never
 * shown, so a node's internal diagnostic cannot leak into the UI.
 */
export function describeFilesError(error: unknown): string {
  const t = i18n.global.t.bind(i18n.global);
  const key = filesErrorKey(error);
  return key ? String(t(key)) : String(t("common.errors.unexpected"));
}

/** isFilesDisabled reports FEATURE_FILES=false (503) for one request. */
export function isFilesDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 503;
}

/**
 * statusKeys maps every file route status onto a curated catalog key
 * (the error contract in internal/files/routes.go writeError).
 */
const statusKeys: Record<number, string> = {
  400: "files.errors.invalid",
  401: "files.errors.sessionExpired",
  403: "files.errors.forbidden",
  404: "files.errors.notFound",
  409: "files.errors.conflict",
  413: "files.errors.tooLarge",
  415: "files.errors.notEditable",
  428: "files.errors.precondition",
  429: "files.errors.busy",
  500: "common.errors.unexpected",
  501: "files.errors.agentOutdated",
  502: "files.errors.agentUnavailable",
  503: "files.errors.disabled",
};
