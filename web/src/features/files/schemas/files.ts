import { z } from "zod";

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

export const filesRootListSchema = z.object({
  roots: z.array(filesRootSchema),
});
export type FilesRootList = z.infer<typeof filesRootListSchema>;

export const filesListingSchema = z.object({
  entries: z.array(filesEntrySchema),
  truncated: z.boolean(),
  cursor: z.string().optional(),
});
export type FilesListing = z.infer<typeof filesListingSchema>;

export const filesStatSchema = z.object({
  entry: filesEntrySchema,
});
export type FilesStat = z.infer<typeof filesStatSchema>;

export const filesFolderSizeSchema = z.object({
  bytes: z.number(),
  entries: z.number(),
  skipped: z.number(),
  truncated: z.boolean(),
});
export type FilesFolderSize = z.infer<typeof filesFolderSizeSchema>;

export const filesSearchHitSchema = z.object({
  path: z.string(),
  entry: filesEntrySchema,
});
export type FilesSearchHit = z.infer<typeof filesSearchHitSchema>;

export const filesSearchListingSchema = z.object({
  results: z.array(filesSearchHitSchema),
  truncated: z.boolean(),
});
export type FilesSearchListing = z.infer<typeof filesSearchListingSchema>;

export const filesContentSchema = z.object({
  content: z.string(),
  encoding: z.string().optional(),
  etag: z.string(),
  sha256: z.string(),
  size: z.number(),
  mode: z.number(),
  truncated: z.boolean().optional(),
});
export type FilesContent = z.infer<typeof filesContentSchema>;

export const filesWriteEnvelopeSchema = z.object({
  entry: filesEntrySchema,
  created: z.boolean(),
  write_mode: z.string(),
});
export type FilesWriteEnvelope = z.infer<typeof filesWriteEnvelopeSchema>;

export const downloadTicketSchema = z.object({
  ticket: z.string(),
});
export type DownloadTicket = z.infer<typeof downloadTicketSchema>;

export const filesUploadResultSchema = z.object({
  name: z.string(),
  path: z.string(),
  entry: filesEntrySchema.optional(),
  error: z.string().optional(),
});
export type FilesUploadResult = z.infer<typeof filesUploadResultSchema>;

export const filesUploadEnvelopeSchema = z.object({
  results: z.array(filesUploadResultSchema),
});
export type FilesUploadEnvelope = z.infer<typeof filesUploadEnvelopeSchema>;

export const trashEntrySchema = z.object({
  id: z.string(),
  name: z.string(),
  original_path: z.string(),
  deleted_at: z.string(),
  size: z.number(),
  kind: fileKindSchema,
});
export type TrashEntry = z.infer<typeof trashEntrySchema>;

export const trashListingSchema = z.object({
  entries: z.array(trashEntrySchema),
  truncated: z.boolean(),
});
export type TrashListing = z.infer<typeof trashListingSchema>;

export const emptyTrashEnvelopeSchema = z.object({
  removed: z.number(),
});
export type EmptyTrashEnvelope = z.infer<typeof emptyTrashEnvelopeSchema>;

export const filesOctalModeSchema = z
  .string()
  .trim()
  .regex(/^[0-7]{3}$/, "files.permissions.invalid");
export type FilesOctalMode = z.infer<typeof filesOctalModeSchema>;

export const filesVersionSchema = z.object({
  id: z.string(),
  created_at: z.string(),
  size: z.number(),
  etag: z.string(),
});
export type FilesVersion = z.infer<typeof filesVersionSchema>;

export const filesVersionListSchema = z.object({
  versions: z.array(filesVersionSchema),
});
export type FilesVersionList = z.infer<typeof filesVersionListSchema>;

export const filesVersionContentSchema = z.object({
  version: filesVersionSchema,
  content: z.string(),
});
export type FilesVersionContent = z.infer<typeof filesVersionContentSchema>;
