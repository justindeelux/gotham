import { z } from "zod";

/**
 * Floating code editor window geometry (JUS-232): persisted per user in
 * localStorage, restored on open and clamped to the viewport. Every field
 * carries its own `.catch(default)`, so one corrupt value only resets that
 * field; an unknown version or a non-object payload falls back to defaults.
 */
export const EDITOR_WINDOW_VERSION = 1 as const;

/** EDITOR_WINDOW_DEFAULT_* is the default window size (~30% above 1024x720). */
export const EDITOR_WINDOW_DEFAULT_WIDTH = 1331 as const;
export const EDITOR_WINDOW_DEFAULT_HEIGHT = 936 as const;

/** EDITOR_WINDOW_MIN_* is the smallest usable window (drag floor). */
export const EDITOR_WINDOW_MIN_WIDTH = 480 as const;
export const EDITOR_WINDOW_MIN_HEIGHT = 320 as const;

/** EDITOR_WINDOW_MAX_DIMENSION caps one persisted window side. */
export const EDITOR_WINDOW_MAX_DIMENSION = 4096 as const;

/**
 * EDITOR_WINDOW_NARROW_VIEWPORT is the width below which the floating window
 * nearly fills the screen, so a dragged offset is meaningless and resets to
 * the pinned default.
 */
export const EDITOR_WINDOW_NARROW_VIEWPORT = 640 as const;

/** editorWindowSchema validates the persisted geometry fields. */
export const editorWindowSchema = z.object({
  width: z.number().int().min(EDITOR_WINDOW_MIN_WIDTH).max(EDITOR_WINDOW_MAX_DIMENSION).catch(EDITOR_WINDOW_DEFAULT_WIDTH),
  height: z.number().int().min(EDITOR_WINDOW_MIN_HEIGHT).max(EDITOR_WINDOW_MAX_DIMENSION).catch(EDITOR_WINDOW_DEFAULT_HEIGHT),
  x: z.number().int().min(0).max(8192).nullable().catch(null).default(null),
  y: z.number().int().min(0).max(8192).nullable().catch(null).default(null),
  minimized: z.boolean().catch(false),
});
export type EditorWindowState = z.infer<typeof editorWindowSchema>;

/** DEFAULT_EDITOR_WINDOW is the factory geometry (pinned default position). */
export const DEFAULT_EDITOR_WINDOW: EditorWindowState = {
  width: EDITOR_WINDOW_DEFAULT_WIDTH,
  height: EDITOR_WINDOW_DEFAULT_HEIGHT,
  x: null,
  y: null,
  minimized: false,
};

/** editorWindowSnapshotSchema is the versioned shape written to storage. */
export const editorWindowSnapshotSchema = editorWindowSchema.extend({
  version: z.literal(EDITOR_WINDOW_VERSION),
});

/**
 * parseEditorWindow resolves untrusted stored data into complete geometry. A
 * wrong version is treated as unreadable (defaults) rather than misread; a
 * missing or mistyped field keeps its default.
 */
export function parseEditorWindow(data: unknown): EditorWindowState {
  if (typeof data !== "object" || data === null) {
    return DEFAULT_EDITOR_WINDOW;
  }
  if ((data as { version?: unknown }).version !== EDITOR_WINDOW_VERSION) {
    return DEFAULT_EDITOR_WINDOW;
  }
  const parsed = editorWindowSchema.safeParse(data);
  return parsed.success ? parsed.data : DEFAULT_EDITOR_WINDOW;
}
