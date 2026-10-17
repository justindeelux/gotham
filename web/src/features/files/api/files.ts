import { isApiError } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import { http, teamHeaders } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type {
  FilesContent,
  FilesEntry,
  FilesFolderSize,
  FilesListing,
  FilesRoot,
  FilesSearchListing,
  FilesUploadResult,
  TrashListing,
} from "@/features/files/schemas/files";
import {
  downloadTicketSchema,
  emptyTrashEnvelopeSchema,
  filesContentSchema,
  filesFolderSizeSchema,
  filesListingSchema,
  filesRootListSchema,
  filesSearchListingSchema,
  filesStatSchema,
  filesUploadEnvelopeSchema,
  filesWriteEnvelopeSchema,
  trashListingSchema,
} from "@/features/files/schemas/files";

/**
 * Typed client for the file routes served by `internal/files`
 * (see routes.go, write_routes.go for the contract):
 *
 *   GET  /servers/{id}/files/roots           any member
 *   GET  /servers/{id}/files/list            any member
 *   GET  /servers/{id}/files/stat            any member
 *   GET  /servers/{id}/files/search          any member (name-only, bounded)
 *   GET  /servers/{id}/files/content         owner/admin
 *   POST /servers/{id}/files/download-ticket owner/admin -> single-use ticket
 *   GET  /servers/{id}/files/download?ticket ticket (native browser download)
 *   POST /servers/{id}/files/mkdir|/create   owner/admin, exclusive create
 *   POST /servers/{id}/files/rename|/move|/copy owner/admin, 409 on conflict
 *   POST /servers/{id}/files/chmod           owner/admin, 0o777 bits only
 *   POST /servers/{id}/files/compress|/extract owner/admin, archive (9b)
 *   DELETE /servers/{id}/files/entry?recursive=  owner/admin (moves to the bin)
 *   GET  /servers/{id}/files/trash            any member -> bin listing
 *   POST /servers/{id}/files/trash/restore    owner/admin, 409 on conflict
 *   POST /servers/{id}/files/trash/purge      owner/admin, permanently deletes one
 *   POST /servers/{id}/files/trash/empty      owner/admin, permanently deletes all
 *   POST /servers/{id}/files/upload          owner/admin, multipart, per-file result
 *
 * The routes are team-scoped: the control plane resolves the active team from
 * the optional `X-Team-Id` header (the caller's personal team when absent) and
 * 404s a foreign or team-less server, so every call carries the team the UI
 * selected. FEATURE_FILES=false answers 503 on every route; the browser hides
 * its surface behind the disabled state instead of rendering it as an error.
 */

/**
 * fileTransferTimeoutMs disables the axios 15 s default for write transfers.
 * An upload or a large copy can run for minutes; the control plane bounds the
 * body (FILES_MAX_UPLOAD_BYTES) and extends its own read deadline, so the
 * client must not cut a healthy transfer short. 0 means "no client timeout".
 */
export const fileTransferTimeoutMs = 0;

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

/**
 * calculateFolderSize returns one folder's recursive size (JUS-162). It is a
 * read; any team member may call it. The client timeout is disabled: the agent
 * bounds the walk by an entry cap and the control plane cancels at its own RPC
 * budget, so `signal` is the caller's cancel control.
 */
export async function calculateFolderSize(
  serverId: string,
  teamId: string,
  root: string,
  path: string,
  signal?: AbortSignal,
): Promise<FilesFolderSize> {
  const response = await http.get(`/servers/${serverId}/files/size`, {
    params: { root, path },
    headers: teamHeaders(teamId),
    timeout: fileTransferTimeoutMs,
    signal,
  });
  return parseWith(filesFolderSizeSchema, response.data, {
    context: "FilesFolderSize",
  });
}

/** SearchEntriesOptions configures one name search (name-only, never content). */
export interface SearchEntriesOptions {
  root: string;
  /** path is the directory to search within; "" searches the root itself. */
  path?: string;
  /** query is the case-insensitive substring every returned name must contain. */
  query: string;
  /** recursive descends into subdirectories (the agent bounds the walk). */
  recursive?: boolean;
  /** signal aborts a superseded/cleared search so the agent stops walking. */
  signal?: AbortSignal;
}

/**
 * searchEntries searches entry NAMES under one directory. It sends no cursor
 * and no timeout override: the agent bounds the walk by depth, result and scan
 * caps and reports a capped run with `truncated`, so the call is one bounded
 * round trip. Any team member may read it, like stat and list.
 */
export async function searchEntries(
  serverId: string,
  teamId: string,
  options: SearchEntriesOptions,
): Promise<FilesSearchListing> {
  const response = await http.get(`/servers/${serverId}/files/search`, {
    params: {
      root: options.root,
      path: options.path ?? "",
      query: options.query,
      recursive: options.recursive === true ? "true" : "false",
    },
    headers: teamHeaders(teamId),
    signal: options.signal,
  });
  const data = parseWith(filesSearchListingSchema, response.data, {
    context: "FilesSearch",
  });
  return {
    results: Array.isArray(data.results) ? data.results : [],
    truncated: data.truncated === true,
  };
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

/** SaveContentOptions configures one editor save (PUT /content). */
export interface SaveContentOptions {
  root: string;
  path: string;
  /** Serialized file bytes (BOM/EOL already re-applied). */
  content: string;
  /** The etag from the last read/save; "" only with overwrite. */
  etag?: string;
  /** overwrite force-replaces a target the caller has not read (If-Match: *). */
  overwrite?: boolean;
}

/**
 * putContent saves one editable text body (owner/admin only). The body is the
 * raw file text, not a JSON envelope: the control plane streams it to the agent
 * and the agent's write header needs an exact byte count, which the browser
 * sets from the string. `If-Match` is the read etag (428 when absent, 409 when
 * stale); `overwrite` sends `If-Match: *` to force-replace.
 */
export async function putContent(
  serverId: string,
  teamId: string,
  options: SaveContentOptions,
): Promise<FilesEntry> {
  const overwrite = options.overwrite === true;
  const response = await http.put(
    `/servers/${serverId}/files/content`,
    options.content,
    {
      params: {
        root: options.root,
        path: options.path,
        overwrite: overwrite ? "true" : "false",
      },
      headers: {
        ...teamHeaders(teamId),
        "Content-Type": "text/plain; charset=utf-8",
        "If-Match": overwrite ? "*" : (options.etag ?? ""),
      },
      // A 2 MiB save over a slow link can outlive the 15 s default.
      timeout: fileTransferTimeoutMs,
    },
  );
  const data = parseWith(filesWriteEnvelopeSchema, response.data, {
    context: "FilesWrite",
  });
  return data.entry;
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
  return data.ticket;
}

/**
 * downloadUrl builds the ticket-authorised download URL. It is addressed
 * against the current origin (`/api/v1…`), never the axios baseURL, because a
 * native navigation does not pass through the axios interceptors.
 */
export function downloadUrl(serverId: string, ticket: string): string {
  return `/api/v1/servers/${serverId}/files/download?ticket=${encodeURIComponent(ticket)}`;
}

/**
 * imageMimeTypes maps the viewer formats (JUS-204) onto their MIME types. The
 * download route serves `application/octet-stream`, which raster formats
 * survive by sniffing but SVG does not, so the viewer rewraps the Blob with
 * the extension type before building the object URL.
 */
const imageMimeTypes: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  svg: "image/svg+xml",
  bmp: "image/bmp",
  ico: "image/x-icon",
  tiff: "image/tiff",
  avif: "image/avif",
};

/**
 * imageMimeType returns the viewer MIME type for one file name, falling back
 * to octet-stream for an unknown extension (the server's own type).
 */
export function imageMimeType(name: string): string {
  const dot = name.lastIndexOf(".");
  const ext = dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
  return imageMimeTypes[ext] ?? "application/octet-stream";
}

/**
 * fetchDownloadBlob loads one ticket-authorised download body as a Blob. The
 * image viewer (JUS-204) shows it through an object URL instead of navigating
 * the SPA to the attachment. The thrown shape carries `status` + `message`, so
 * {@link describeFilesError} maps it onto the files catalog like any API
 * failure.
 */
export async function fetchDownloadBlob(
  serverId: string,
  ticket: string,
  name: string,
  signal?: AbortSignal,
): Promise<Blob> {
  const response = await fetch(downloadUrl(serverId, ticket), { signal });
  if (!response.ok) {
    throw { status: response.status, message: `Download failed: ${response.status}` };
  }
  const blob = await response.blob();
  return new Blob([blob], { type: imageMimeType(name) });
}

/** filesErrorKey maps an HTTP status (or a stable server code) onto its files catalog key. */
export function filesErrorKey(error: unknown): string | null {
  if (isApiError(error)) {
    if (error.code && codeKeys[error.code]) {
      return codeKeys[error.code];
    }
    if (typeof error.status === "number") {
      return statusKeys[error.status] ?? null;
    }
  }
  return null;
}

/** isConflictError reports a 409 (an entry already exists / changed). */
export function isConflictError(error: unknown): boolean {
  return filesErrorKey(error) === "files.errors.conflict";
}

/** isCanceledError reports a request the caller aborted (never an error toast). */
export function isCanceledError(error: unknown): boolean {
  if (isApiError(error)) {
    return error.status === null && /cancel/i.test(error.message);
  }
  return error instanceof Error && error.name === "CanceledError";
}

/** CreateEntryOptions configures an exclusive create of a file or folder. */
export interface CreateEntryOptions {
  root: string;
  path: string;
  /** POSIX mode; the server applies its own default when omitted. */
  mode?: number;
}

/** parseEntry unwraps the { entry } envelope shared by the write responses. */
function parseEntry(data: unknown, context: string): FilesEntry {
  const parsed = parseWith(filesStatSchema, data, { context });
  return parsed.entry;
}

/** createFolder creates one directory, exclusively (409 if it exists). */
export async function createFolder(
  serverId: string,
  teamId: string,
  options: CreateEntryOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/mkdir`,
    { root: options.root, path: options.path, mode: options.mode ?? 0o755 },
    { headers: teamHeaders(teamId) },
  );
  return parseEntry(response.data, "FilesMkdir");
}

/** createFile creates one empty file, exclusively (409 if it exists). */
export async function createFile(
  serverId: string,
  teamId: string,
  options: CreateEntryOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/create`,
    { root: options.root, path: options.path, mode: options.mode ?? 0o644 },
    { headers: teamHeaders(teamId) },
  );
  return parseEntry(response.data, "FilesCreate");
}

/** LocationOptions addresses a single-root rename, move or copy. */
export interface LocationOptions {
  root: string;
  source: string;
  destination: string;
  /** overwrite replaces an existing destination; default false (409). */
  overwrite?: boolean;
}

/** locationRequest posts one rename/move/copy body and returns the entry. */
async function postLocation(
  route: "rename" | "move" | "copy",
  context: string,
  serverId: string,
  teamId: string,
  options: LocationOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/${route}`,
    {
      root: options.root,
      source: options.source,
      destination: options.destination,
      overwrite: options.overwrite === true,
    },
    { headers: teamHeaders(teamId) },
  );
  return parseEntry(response.data, context);
}

/** renameEntry renames one entry inside the current root. */
export async function renameEntry(
  serverId: string,
  teamId: string,
  options: LocationOptions,
): Promise<FilesEntry> {
  return postLocation("rename", "FilesRename", serverId, teamId, options);
}

/** moveEntry moves one entry inside the current root. */
export async function moveEntry(
  serverId: string,
  teamId: string,
  options: LocationOptions,
): Promise<FilesEntry> {
  return postLocation("move", "FilesMove", serverId, teamId, options);
}

/** copyEntry copies one entry inside the current root. */
export async function copyEntry(
  serverId: string,
  teamId: string,
  options: LocationOptions,
): Promise<FilesEntry> {
  return postLocation("copy", "FilesCopy", serverId, teamId, options);
}

/**
 * deleteEntry removes one entry (recursive for a folder). A delete always moves
 * the entry to the root's recycle bin — permanent deletion is offered only from
 * the bin (purge one / empty). The control plane requires owner/admin.
 */
export async function deleteEntry(
  serverId: string,
  teamId: string,
  root: string,
  path: string,
  recursive: boolean,
): Promise<void> {
  await http.delete(`/servers/${serverId}/files/entry`, {
    params: { root, path, recursive: recursive ? "true" : "false" },
    headers: teamHeaders(teamId),
  });
}

/** listTrash reads one root's recycle bin, newest first (any member). */
export async function listTrash(
  serverId: string,
  teamId: string,
  root: string,
): Promise<TrashListing> {
  const response = await http.get(`/servers/${serverId}/files/trash`, {
    params: { root },
    headers: teamHeaders(teamId),
  });
  const data = parseWith(trashListingSchema, response.data, {
    context: "TrashListing",
  });
  return {
    entries: Array.isArray(data.entries) ? data.entries : [],
    truncated: data.truncated === true,
  };
}

/** RestoreTrashOptions addresses one bin restore. */
export interface RestoreTrashOptions {
  root: string;
  /** id is the bin entry's opaque id (from the listing), not a path. */
  id: string;
  /** destination overrides the recorded original path when non-empty. */
  destination?: string;
  /** overwrite replaces an existing destination instead of 409ing. */
  overwrite?: boolean;
}

/**
 * restoreTrash moves one bin entry back to its recorded path (or an explicit
 * destination) and returns the restored entry. An occupied destination is a
 * 409 unless `overwrite` is set, so the caller can explain the conflict.
 */
export async function restoreTrash(
  serverId: string,
  teamId: string,
  options: RestoreTrashOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/trash/restore`,
    {
      root: options.root,
      id: options.id,
      destination: options.destination ?? "",
      overwrite: options.overwrite === true,
    },
    { headers: teamHeaders(teamId) },
  );
  return parseEntry(response.data, "TrashRestore");
}

/** purgeTrash permanently deletes one bin entry (owner/admin). */
export async function purgeTrash(
  serverId: string,
  teamId: string,
  root: string,
  id: string,
): Promise<void> {
  await http.post(
    `/servers/${serverId}/files/trash/purge`,
    { root, id },
    { headers: teamHeaders(teamId) },
  );
}

/** emptyTrash permanently deletes every bin entry and reports how many. */
export async function emptyTrash(
  serverId: string,
  teamId: string,
  root: string,
): Promise<number> {
  const response = await http.post(
    `/servers/${serverId}/files/trash/empty`,
    { root },
    { headers: teamHeaders(teamId) },
  );
  const data = parseWith(emptyTrashEnvelopeSchema, response.data, {
    context: "TrashEmpty",
  });
  return typeof data.removed === "number" ? data.removed : 0;
}

/** ChmodOptions addresses one permission change. */
export interface ChmodOptions {
  root: string;
  path: string;
  /** The new permission bits (low 0o777 only; special bits are refused). */
  mode: number;
}

/**
 * chmodEntry changes one entry's permission bits (owner/admin only). The
 * server refuses setuid/setgid/sticky and returns the updated entry, which the
 * caller uses to refresh the listing.
 */
export async function chmodEntry(
  serverId: string,
  teamId: string,
  options: ChmodOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/chmod`,
    { root: options.root, path: options.path, mode: options.mode },
    { headers: teamHeaders(teamId) },
  );
  return parseEntry(response.data, "FilesChmod");
}

/** ArchiveFormat is the container/compression the compress dialog offers. */
export type ArchiveFormat = "zip" | "tar" | "tar.gz";

/** CompressOptions configures one archive build. */
export interface CompressOptions {
  root: string;
  /** sources are root-relative paths of the files/folders to archive. */
  sources: string[];
  /** destination is the archive's root-relative path. */
  destination: string;
  format: ArchiveFormat;
  /** overwrite replaces an existing archive; default false (409). */
  overwrite?: boolean;
}

/**
 * compressEntries builds one archive from sources inside a single root. The
 * control plane streams the agent's progress and returns the created archive's
 * entry; the request timeout is disabled because a large tree outlives the 15 s
 * default.
 */
export async function compressEntries(
  serverId: string,
  teamId: string,
  options: CompressOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/compress`,
    {
      root: options.root,
      sources: options.sources,
      destination: options.destination,
      format: options.format,
      overwrite: options.overwrite === true,
    },
    { headers: teamHeaders(teamId), timeout: fileTransferTimeoutMs },
  );
  return parseEntry(response.data, "FilesCompress");
}

/** ExtractOptions configures one archive unpack. */
export interface ExtractOptions {
  root: string;
  /** source is the archive's root-relative path. */
  source: string;
  /** destination is the root-relative directory to unpack into ("" = root). */
  destination: string;
  /** overwrite replaces existing files; default false (409). */
  overwrite?: boolean;
}

/**
 * extractEntry unpacks one archive into a destination directory inside a single
 * root. The agent refuses symbolic-link, hard-link, device and FIFO entries and
 * never writes outside the root.
 */
export async function extractEntry(
  serverId: string,
  teamId: string,
  options: ExtractOptions,
): Promise<void> {
  await http.post(
    `/servers/${serverId}/files/extract`,
    {
      root: options.root,
      source: options.source,
      destination: options.destination,
      overwrite: options.overwrite === true,
    },
    { headers: teamHeaders(teamId), timeout: fileTransferTimeoutMs },
  );
}

/** UploadProgress is one upload's transferred bytes. */
export interface UploadProgress {
  loaded: number;
  total: number;
}

/** UploadFilesOptions configures a multipart upload into one directory. */
export interface UploadFilesOptions {
  root: string;
  path: string;
  /** overwrite replaces existing names; default false reports a conflict. */
  overwrite?: boolean;
  onProgress?: (_progress: UploadProgress) => void;
  signal?: AbortSignal;
}

/**
 * uploadFiles streams files to POST /upload as one multipart request and
 * returns the server's per-file result array. The request timeout is disabled
 * (see {@link fileTransferTimeoutMs}) because a large body outlives the 15 s
 * default; the server bounds the body and reports a per-file outcome for every
 * part, so one file's failure never hides the rest.
 */
export async function uploadFiles(
  serverId: string,
  teamId: string,
  files: File[],
  options: UploadFilesOptions,
): Promise<FilesUploadResult[]> {
  const form = new FormData();
  for (const file of files) {
    form.append("files", file, file.name);
  }
  const response = await http.post(
    `/servers/${serverId}/files/upload`,
    form,
    {
      params: {
        root: options.root,
        path: options.path ?? "",
        overwrite: options.overwrite === true ? "true" : "false",
      },
      headers: teamHeaders(teamId),
      timeout: fileTransferTimeoutMs,
      signal: options.signal,
      onUploadProgress: options.onProgress
        ? (event) => {
            const total = event.total ?? event.loaded;
            options.onProgress?.({ loaded: event.loaded, total });
          }
        : undefined,
    },
  );
  const data = parseWith(filesUploadEnvelopeSchema, response.data, {
    context: "FilesUpload",
  });
  return Array.isArray(data.results) ? data.results : [];
}

/**
 * uploadErrorKey maps one per-file upload error token (write_routes.go
 * publicError) onto its catalog key.
 */
export function uploadErrorKey(token: string): string {
  return uploadErrorKeys[token] ?? "common.errors.unexpected";
}

/** describeUploadError renders one per-file upload error in the locale. */
export function describeUploadError(token: string): string {
  const t = i18n.global.t.bind(i18n.global);
  return String(t(uploadErrorKey(token)));
}

/** uploadErrorKeys maps the server's stable error tokens onto catalog keys. */
const uploadErrorKeys: Record<string, string> = {
  not_found: "files.errors.notFound",
  forbidden: "files.errors.forbidden",
  conflict: "files.errors.conflict",
  too_large: "files.errors.tooLarge",
  invalid: "files.name.invalid",
  busy: "files.errors.busy",
  insufficient_storage: "files.errors.insufficientStorage",
  agent_outdated: "files.errors.agentOutdated",
  agent_unavailable: "files.errors.agentUnavailable",
  disabled: "files.errors.disabled",
  truncated: "files.upload.truncated",
  error: "common.errors.unexpected",
};

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
  507: "files.errors.insufficientStorage",
};

/**
 * codeKeys overrides the status mapping with a stable server token, so an
 * archive limit (413) or a refused archive member (409) gets its own accurate
 * message instead of the editor/upload or conflict copy.
 */
const codeKeys: Record<string, string> = {
  archive_limit: "files.errors.archiveLimit",
  archive_entry: "files.errors.archiveEntry",
  invalid_archive: "files.errors.invalidArchive",
};
