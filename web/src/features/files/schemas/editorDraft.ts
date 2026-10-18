import { z } from "zod";

/**
 * Versioned localStorage draft of one dirty editor buffer (JUS-229). The
 * version is a literal so a draft written by a newer client fails validation
 * and is ignored (and dropped) instead of being restored as garbage. `etag` is
 * the base version the draft was typed against, so a later load can warn when
 * the file changed underneath it.
 */
export const EDITOR_DRAFT_VERSION = 1 as const;

/** EDITOR_DRAFT_TTL_MS bounds how long an untouched draft is kept. */
export const EDITOR_DRAFT_TTL_MS = 7 * 24 * 60 * 60 * 1000;

/**
 * EDITOR_DRAFT_MAX_DRAFTS caps the stored drafts per server+user; the oldest
 * are evicted so many edited files can never fill the browser quota.
 */
export const EDITOR_DRAFT_MAX_DRAFTS = 10;

/**
 * EDITOR_DRAFT_MAX_BYTES caps one draft's raw JSON. A bigger buffer (up to the
 * editor's 2 MiB read limit) is simply not persisted: recovery degrades
 * silently instead of breaking the editor.
 */
export const EDITOR_DRAFT_MAX_BYTES = 192 * 1024;

/** editorDraftSchema validates one stored draft entry. */
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
