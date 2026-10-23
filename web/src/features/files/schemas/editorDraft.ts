import { z } from "zod";

export const EDITOR_DRAFT_VERSION = 1 as const;

export const EDITOR_DRAFT_TTL_MS = 7 * 24 * 60 * 60 * 1000;

export const EDITOR_DRAFT_MAX_DRAFTS = 10;

export const EDITOR_DRAFT_MAX_CHARS = 128 * 1024;

export const editorDraftSchema = z.object({
  version: z.literal(EDITOR_DRAFT_VERSION),
  root: z.string().min(1).max(256),
  path: z.string().max(1024),
  doc: z.string(),
  etag: z.string(),
  eol: z.enum(["\n", "\r\n"]),
  bom: z.boolean(),
  savedAt: z.number().int().nonnegative(),
});
export type EditorDraft = z.infer<typeof editorDraftSchema>;
