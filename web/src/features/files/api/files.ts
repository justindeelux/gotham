import { isApiError } from "@/features/servers";
import type { EditorEncoding } from "@/features/files/utils/editor";
import { i18n } from "@/shared/i18n";
import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type {
  FilesContent,
  FilesEntry,
  FilesFolderSize,
  FilesListing,
  FilesRoot,
  FilesSearchListing,
  FilesUploadResult,
  FilesVersion,
  FilesVersionContent,
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
  filesVersionContentSchema,
  filesVersionListSchema,
  filesWriteEnvelopeSchema,
  trashListingSchema,
} from "@/features/files/schemas/files";

export const fileTransferTimeoutMs = 0;

export async function listRoots(
  serverId: string,
): Promise<FilesRoot[]> {
  const response = await http.get(`/servers/${serverId}/files/roots`, {
  });
  const data = parseWith(filesRootListSchema, response.data, {
    context: "FilesRootList",
  });
  return Array.isArray(data.roots) ? data.roots : [];
}

export interface ListEntriesOptions {
  root: string;
  path?: string;
  cursor?: string;
}

export async function listEntries(
  serverId: string,
  options: ListEntriesOptions,
): Promise<FilesListing> {
  const response = await http.get(`/servers/${serverId}/files/list`, {
    params: {
      root: options.root,
      path: options.path ?? "",
      cursor: options.cursor ?? "",
    },
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

export async function statEntry(
  serverId: string,
  root: string,
  path: string,
): Promise<FilesEntry | null> {
  const response = await http.get(`/servers/${serverId}/files/stat`, {
    params: { root, path },
  });
  const data = parseWith(filesStatSchema, response.data, {
    context: "FilesStat",
  });
  return data.entry ?? null;
}

export async function calculateFolderSize(
  serverId: string,
  root: string,
  path: string,
  signal?: AbortSignal,
): Promise<FilesFolderSize> {
  const response = await http.get(`/servers/${serverId}/files/size`, {
    params: { root, path },
    timeout: fileTransferTimeoutMs,
    signal,
  });
  return parseWith(filesFolderSizeSchema, response.data, {
    context: "FilesFolderSize",
  });
}

export interface SearchEntriesOptions {
  root: string;
  path?: string;
  query: string;
  recursive?: boolean;
  signal?: AbortSignal;
}

export async function searchEntries(
  serverId: string,
  options: SearchEntriesOptions,
): Promise<FilesSearchListing> {
  const response = await http.get(`/servers/${serverId}/files/search`, {
    params: {
      root: options.root,
      path: options.path ?? "",
      query: options.query,
      recursive: options.recursive === true ? "true" : "false",
    },
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

export async function getContent(
  serverId: string,
  root: string,
  path: string,
): Promise<FilesContent> {
  const response = await http.get(`/servers/${serverId}/files/content`, {
    params: { root, path },
  });
  return parseWith(filesContentSchema, response.data, {
    context: "FilesContent",
  });
}

export interface SaveContentOptions {
  root: string;
  path: string;
  content: string;
  etag?: string;
  overwrite?: boolean;
  encoding?: EditorEncoding;
}

export async function putContent(
  serverId: string,
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
        encoding: options.encoding ?? "utf-8",
      },
      headers: {
        "Content-Type": "text/plain; charset=utf-8",
        "If-Match": overwrite ? "*" : (options.etag ?? ""),
      },
      timeout: fileTransferTimeoutMs,
    },
  );
  const data = parseWith(filesWriteEnvelopeSchema, response.data, {
    context: "FilesWrite",
  });
  return data.entry;
}

export async function listFileVersions(
  serverId: string,
  root: string,
  path: string,
): Promise<FilesVersion[]> {
  const response = await http.get(`/servers/${serverId}/files/history`, {
    params: { root, path },
  });
  const data = parseWith(filesVersionListSchema, response.data, {
    context: "FilesVersionList",
  });
  return data.versions;
}

export async function getFileVersion(
  serverId: string,
  root: string,
  path: string,
  versionId: string,
): Promise<FilesVersionContent> {
  const response = await http.get(
    `/servers/${serverId}/files/history/${encodeURIComponent(versionId)}`,
    { params: { root, path } },
  );
  return parseWith(filesVersionContentSchema, response.data, {
    context: "FilesVersionContent",
  });
}

export async function restoreFileVersion(
  serverId: string,
  root: string,
  path: string,
  versionId: string,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/history/${encodeURIComponent(versionId)}/restore`,
    null,
    {
      params: { root, path },
        timeout: fileTransferTimeoutMs,
    },
  );
  const data = parseWith(filesWriteEnvelopeSchema, response.data, {
    context: "FilesVersionRestore",
  });
  return data.entry;
}

export async function deleteFileVersion(
  serverId: string,
  root: string,
  path: string,
  versionId: string,
): Promise<void> {
  await http.delete(
    `/servers/${serverId}/files/history/${encodeURIComponent(versionId)}`,
    { params: { root, path } },
  );
}

export async function issueDownloadTicket(
  serverId: string,
  root: string,
  path: string,
): Promise<string> {
  const response = await http.post(
    `/servers/${serverId}/files/download-ticket`,
    null,
    { params: { root, path } },
  );
  const data = parseWith(downloadTicketSchema, response.data, {
    context: "DownloadTicket",
  });
  return data.ticket;
}

export function downloadUrl(serverId: string, ticket: string): string {
  return `/api/v1/servers/${serverId}/files/download?ticket=${encodeURIComponent(ticket)}`;
}

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

export function imageMimeType(name: string): string {
  const dot = name.lastIndexOf(".");
  const ext = dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
  return imageMimeTypes[ext] ?? "application/octet-stream";
}

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

export function isConflictError(error: unknown): boolean {
  return filesErrorKey(error) === "files.errors.conflict";
}

export function isCanceledError(error: unknown): boolean {
  if (isApiError(error)) {
    return error.status === null && /cancel/i.test(error.message);
  }
  return error instanceof Error && error.name === "CanceledError";
}

export interface CreateEntryOptions {
  root: string;
  path: string;
  mode?: number;
}

function parseEntry(data: unknown, context: string): FilesEntry {
  const parsed = parseWith(filesStatSchema, data, { context });
  return parsed.entry;
}

export async function createFolder(
  serverId: string,
  options: CreateEntryOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/mkdir`,
    { root: options.root, path: options.path, mode: options.mode ?? 0o755 },

  );
  return parseEntry(response.data, "FilesMkdir");
}

export async function createFile(
  serverId: string,
  options: CreateEntryOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/create`,
    { root: options.root, path: options.path, mode: options.mode ?? 0o644 },

  );
  return parseEntry(response.data, "FilesCreate");
}

export interface LocationOptions {
  root: string;
  source: string;
  destination: string;
  overwrite?: boolean;
}

async function postLocation(
  route: "rename" | "move" | "copy",
  context: string,
  serverId: string,
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

  );
  return parseEntry(response.data, context);
}

export async function renameEntry(
  serverId: string,
  options: LocationOptions,
): Promise<FilesEntry> {
  return postLocation("rename", "FilesRename", serverId, options);
}

export async function moveEntry(
  serverId: string,
  options: LocationOptions,
): Promise<FilesEntry> {
  return postLocation("move", "FilesMove", serverId, options);
}

export async function copyEntry(
  serverId: string,
  options: LocationOptions,
): Promise<FilesEntry> {
  return postLocation("copy", "FilesCopy", serverId, options);
}

export async function deleteEntry(
  serverId: string,
  root: string,
  path: string,
  recursive: boolean,
): Promise<void> {
  await http.delete(`/servers/${serverId}/files/entry`, {
    params: { root, path, recursive: recursive ? "true" : "false" },
  });
}

export async function listTrash(
  serverId: string,
  root: string,
): Promise<TrashListing> {
  const response = await http.get(`/servers/${serverId}/files/trash`, {
    params: { root },
  });
  const data = parseWith(trashListingSchema, response.data, {
    context: "TrashListing",
  });
  return {
    entries: Array.isArray(data.entries) ? data.entries : [],
    truncated: data.truncated === true,
  };
}

export interface RestoreTrashOptions {
  root: string;
  id: string;
  destination?: string;
  overwrite?: boolean;
}

export async function restoreTrash(
  serverId: string,
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

  );
  return parseEntry(response.data, "TrashRestore");
}

export async function purgeTrash(
  serverId: string,
  root: string,
  id: string,
): Promise<void> {
  await http.post(
    `/servers/${serverId}/files/trash/purge`,
    { root, id },

  );
}

export async function emptyTrash(
  serverId: string,
  root: string,
): Promise<number> {
  const response = await http.post(
    `/servers/${serverId}/files/trash/empty`,
    { root },

  );
  const data = parseWith(emptyTrashEnvelopeSchema, response.data, {
    context: "TrashEmpty",
  });
  return typeof data.removed === "number" ? data.removed : 0;
}

export interface ChmodOptions {
  root: string;
  path: string;
  mode: number;
}

export async function chmodEntry(
  serverId: string,
  options: ChmodOptions,
): Promise<FilesEntry> {
  const response = await http.post(
    `/servers/${serverId}/files/chmod`,
    { root: options.root, path: options.path, mode: options.mode },

  );
  return parseEntry(response.data, "FilesChmod");
}

export type ArchiveFormat = "zip" | "tar" | "tar.gz";

export interface CompressOptions {
  root: string;
  sources: string[];
  destination: string;
  format: ArchiveFormat;
  overwrite?: boolean;
}

export async function compressEntries(
  serverId: string,
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
    { timeout: fileTransferTimeoutMs },
  );
  return parseEntry(response.data, "FilesCompress");
}

export interface ExtractOptions {
  root: string;
  source: string;
  destination: string;
  overwrite?: boolean;
}

export async function extractEntry(
  serverId: string,
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
    { timeout: fileTransferTimeoutMs },
  );
}

export interface UploadProgress {
  loaded: number;
  total: number;
}

export interface UploadFilesOptions {
  root: string;
  path: string;
  overwrite?: boolean;
  onProgress?: (_progress: UploadProgress) => void;
  signal?: AbortSignal;
}

export async function uploadFiles(
  serverId: string,
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

export function uploadErrorKey(token: string): string {
  return uploadErrorKeys[token] ?? "common.errors.unexpected";
}

export function describeUploadError(token: string): string {
  const t = i18n.global.t.bind(i18n.global);
  return String(t(uploadErrorKey(token)));
}

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
  file_manager_disabled: "files.errors.nodeDisabled",
  truncated: "files.upload.truncated",
  error: "common.errors.unexpected",
};

export function describeFilesError(error: unknown): string {
  const t = i18n.global.t.bind(i18n.global);
  const key = filesErrorKey(error);
  return key ? String(t(key)) : String(t("common.errors.unexpected"));
}

export function isFilesDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 503;
}

export function isFilesDisabledOnNode(error: unknown): boolean {
  return isApiError(error) && error.code === "file_manager_disabled";
}

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

const codeKeys: Record<string, string> = {
  file_manager_disabled: "files.errors.nodeDisabled",
  archive_limit: "files.errors.archiveLimit",
  archive_entry: "files.errors.archiveEntry",
  invalid_archive: "files.errors.invalidArchive",
  history_unavailable: "files.editor.historyUnavailable",
  unrepresentable: "files.errors.unrepresentable",
};
