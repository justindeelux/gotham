import { z } from "zod";

export const EDITOR_WINDOW_VERSION = 1 as const;

export const EDITOR_WINDOW_DEFAULT_WIDTH = 1331 as const;
export const EDITOR_WINDOW_DEFAULT_HEIGHT = 936 as const;

export const EDITOR_WINDOW_MIN_WIDTH = 480 as const;
export const EDITOR_WINDOW_MIN_HEIGHT = 320 as const;

export const EDITOR_WINDOW_MAX_DIMENSION = 4096 as const;

export const EDITOR_WINDOW_CHROME_MARGIN = 104 as const;

export const EDITOR_WINDOW_SIDE_MARGIN = 48 as const;

export const EDITOR_WINDOW_NARROW_VIEWPORT = 640 as const;

export const editorWindowSchema = z.object({
  width: z.number().int().min(EDITOR_WINDOW_MIN_WIDTH).max(EDITOR_WINDOW_MAX_DIMENSION).catch(EDITOR_WINDOW_DEFAULT_WIDTH),
  height: z.number().int().min(EDITOR_WINDOW_MIN_HEIGHT).max(EDITOR_WINDOW_MAX_DIMENSION).catch(EDITOR_WINDOW_DEFAULT_HEIGHT),
  x: z.number().int().min(0).max(8192).nullable().catch(null).default(null),
  y: z.number().int().min(0).max(8192).nullable().catch(null).default(null),
  minimized: z.boolean().catch(false),
});
export type EditorWindowState = z.infer<typeof editorWindowSchema>;

export const DEFAULT_EDITOR_WINDOW: EditorWindowState = {
  width: EDITOR_WINDOW_DEFAULT_WIDTH,
  height: EDITOR_WINDOW_DEFAULT_HEIGHT,
  x: null,
  y: null,
  minimized: false,
};

export const editorWindowSnapshotSchema = editorWindowSchema.extend({
  version: z.literal(EDITOR_WINDOW_VERSION),
});

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
