import { z } from "zod";

/**
 * Response schemas for the read-only file routes served by `internal/files`
 * (see routes.go, service.go for the exact envelopes):
 *
 *   GET  /servers/{id}/files/roots            -> { roots: Root[] }
 *   GET  /servers/{id}/files/list             -> Listing
 *   GET  /servers/{id}/files/stat             -> { entry: Entry }
 *   GET  /servers/{id}/files/content          -> Content
 *   POST /servers/{id}/files/download-ticket  -> { ticket } (201)
 *
 * Envelopes are validated warn-only through `parseWith`, so a newer server
 * field degrades to a warning instead of a blank page. `kind` mirrors the
 * agent's FileKind enum through `kindString` in internal/files/service.go.
 * `mode` is the raw POSIX st_mode (type bits + permission bits).
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

/** contentSchema validates GET /content (owner/admin only, <= 2 MiB). */
export const filesContentSchema = z.object({
  content: z.string(),
  etag: z.string(),
  sha256: z.string(),
  size: z.number(),
  mode: z.number(),
});
export type FilesContent = z.infer<typeof filesContentSchema>;

/** downloadTicketSchema validates POST /download-ticket. */
export const downloadTicketSchema = z.object({
  ticket: z.string(),
});
export type DownloadTicket = z.infer<typeof downloadTicketSchema>;
