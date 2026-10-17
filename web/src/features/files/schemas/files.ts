import { z } from "zod";

/**
 * Response schemas for the read-only file routes served by `internal/files`
 * (see routes.go, service.go for the exact envelopes):
 *
 *   GET  /servers/{id}/files/roots            -> { roots: Root[] }
 *   GET  /servers/{id}/files/list             -> Listing
 *   GET  /servers/{id}/files/stat             -> { entry: Entry }
 *   GET  /servers/{id}/files/search           -> { results: { path, entry }[], truncated }
 *   GET  /servers/{id}/files/content          -> Content
 *   POST /servers/{id}/files/download-ticket  -> { ticket } (201)
 *
 * Envelopes are validated warn-only through `parseWith`, so a newer server
 * field degrades to a warning instead of a blank page. `kind` mirrors the
 * agent's FileKind enum through `kindString` in internal/files/service.go.
 * `mode` is the raw POSIX st_mode (type bits + permission bits). `owner` and
 * `group` are the agent's resolved names (numeric id when unresolvable); both
 * are empty from an agent that predates them.
 */

export const fileKindSchema = z.enum([
  "file",
  "directory",
  "symlink",
  "special",
  "unspecified",
]);
export type FileKind = z.infer<typeof fileKindSchema>;

export const filesRootSchema = z.object({
  name: z.string(),
  path: z.string(),
  writable: z.boolean(),
  free_bytes: z.number(),
});
export type FilesRoot = z.infer<typeof filesRootSchema>;

export const filesEntrySchema = z.object({
  name: z.string(),
  kind: fileKindSchema,
  size: z.number(),
  mode: z.number(),
  modified_at: z.string(),
  symlink_target: z.string().optional(),
  etag: z.string(),
  owner: z.string().optional(),
  group: z.string().optional(),
});
export type FilesEntry = z.infer<typeof filesEntrySchema>;

/** rootsEnvelopeSchema validates GET /roots. */
export const filesRootListSchema = z.object({
  roots: z.array(filesRootSchema),
});
export type FilesRootList = z.infer<typeof filesRootListSchema>;

/** listingEnvelopeSchema validates GET /list. */
export const filesListingSchema = z.object({
  entries: z.array(filesEntrySchema),
  truncated: z.boolean(),
  cursor: z.string().optional(),
});
export type FilesListing = z.infer<typeof filesListingSchema>;

/** statEnvelopeSchema validates GET /stat. */
export const filesStatSchema = z.object({
  entry: filesEntrySchema,
});
export type FilesStat = z.infer<typeof filesStatSchema>;

/**
 * folderSizeSchema validates GET /size (JUS-162): one folder's recursive size.
 * `bytes` sums regular-file sizes; `truncated` marks a partial result the
 * agent stopped at its entry cap with.
 */
export const filesFolderSizeSchema = z.object({
  bytes: z.number(),
  entries: z.number(),
  skipped: z.number(),
  truncated: z.boolean(),
});
export type FilesFolderSize = z.infer<typeof filesFolderSizeSchema>;

/**
 * filesSearchHitSchema validates one entry in GET /search: the matching entry
 * plus its root-relative `path`, so two same-named files in different
 * directories stay distinct.
 */
export const filesSearchHitSchema = z.object({
  path: z.string(),
  entry: filesEntrySchema,
});
export type FilesSearchHit = z.infer<typeof filesSearchHitSchema>;

/**
 * filesSearchListingSchema validates GET /search. `truncated` reports that a
 * depth, result or scan cap stopped the walk, so the UI can say the list is
 * partial instead of showing it as complete.
 */
export const filesSearchListingSchema = z.object({
  results: z.array(filesSearchHitSchema),
  truncated: z.boolean(),
});
export type FilesSearchListing = z.infer<typeof filesSearchListingSchema>;

/** contentSchema validates GET /content (owner/admin only, <= 2 MiB). */
export const filesContentSchema = z.object({
  content: z.string(),
  etag: z.string(),
  sha256: z.string(),
  size: z.number(),
  mode: z.number(),
});
export type FilesContent = z.infer<typeof filesContentSchema>;

/**
 * writeEnvelopeSchema validates PUT /content. `entry` carries the committed
 * file's fresh etag, which the editor stores as the next If-Match.
 */
export const filesWriteEnvelopeSchema = z.object({
  entry: filesEntrySchema,
  created: z.boolean(),
  write_mode: z.string(),
});
export type FilesWriteEnvelope = z.infer<typeof filesWriteEnvelopeSchema>;

/** downloadTicketSchema validates POST /download-ticket. */
export const downloadTicketSchema = z.object({
  ticket: z.string(),
});
export type DownloadTicket = z.infer<typeof downloadTicketSchema>;

/**
 * uploadResultSchema validates one file's outcome in a multipart upload
 * (POST /upload -> { results: [...] }). A success carries `entry`; a failure
 * carries a stable `error` token (never raw agent text) that the browser maps
 * to a catalog message. `name`/`path` are always present so a partial failure
 * still names the file it failed on.
 */
export const filesUploadResultSchema = z.object({
  name: z.string(),
  path: z.string(),
  entry: filesEntrySchema.optional(),
  error: z.string().optional(),
});
export type FilesUploadResult = z.infer<typeof filesUploadResultSchema>;

/** uploadEnvelopeSchema validates POST /upload. */
export const filesUploadEnvelopeSchema = z.object({
  results: z.array(filesUploadResultSchema),
});
export type FilesUploadEnvelope = z.infer<typeof filesUploadEnvelopeSchema>;

/**
 * trashEntrySchema validates one recycle-bin entry (GET /trash). It carries the
 * recorded original path, the deleted-at timestamp, the entry's size and kind
 * (no mode/etag: the bin is not a directory listing).
 */
export const trashEntrySchema = z.object({
  id: z.string(),
  name: z.string(),
  original_path: z.string(),
  deleted_at: z.string(),
  size: z.number(),
  kind: fileKindSchema,
});
export type TrashEntry = z.infer<typeof trashEntrySchema>;

/**
 * trashListingSchema validates GET /trash. `truncated` reports a capped listing
 * (the node holds more entries than it returned; the oldest are omitted), never
 * a silent cap, so the dialog can say so instead of lying with a short list.
 */
export const trashListingSchema = z.object({
  entries: z.array(trashEntrySchema),
  truncated: z.boolean(),
});
export type TrashListing = z.infer<typeof trashListingSchema>;

/** emptyTrashEnvelopeSchema validates POST /trash/empty (`removed` count). */
export const emptyTrashEnvelopeSchema = z.object({
  removed: z.number(),
});
export type EmptyTrashEnvelope = z.infer<typeof emptyTrashEnvelopeSchema>;

/**
 * filesOctalModeSchema validates the Permissions dialog's octal field: exactly
 * three octal digits (the low 0o777 bits). The message is a catalog key, so
 * the shared validation adapter resolves it in the active locale.
 */
export const filesOctalModeSchema = z
  .string()
  .trim()
  .regex(/^[0-7]{3}$/, "files.permissions.invalid");
export type FilesOctalMode = z.infer<typeof filesOctalModeSchema>;
