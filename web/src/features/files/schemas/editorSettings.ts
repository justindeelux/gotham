import { z } from "zod";

export const EDITOR_SETTINGS_VERSION = 1 as const;

export const EDITOR_FONT_SIZES = [11, 12, 13, 14, 16, 18, 20] as const;

export const EDITOR_TAB_SIZES = [2, 4, 8] as const;

export const editorSettingsSchema = z.object({
  theme: z.enum(["dark", "light"]).catch("dark"),
  fontSize: z
    .number()
    .int()
    .min(EDITOR_FONT_SIZES[0])
    .max(EDITOR_FONT_SIZES[EDITOR_FONT_SIZES.length - 1])
    .catch(14),
  fontFamily: z.enum(["mono", "sans"]).catch("mono"),
  tabSize: z.union([z.literal(2), z.literal(4), z.literal(8)]).catch(4),
  insertSpaces: z.boolean().catch(true),
  autocomplete: z.boolean().catch(true).default(true),
  snippets: z.boolean().catch(true).default(true),
  lineNumbers: z.boolean().catch(true),
  showInvisibles: z.boolean().catch(false),
  indentGuides: z.boolean().catch(true),
  foldGutter: z.boolean().catch(true),
  highlightActiveLine: z.boolean().catch(true),
  wrap: z.boolean().catch(false),
});
export type EditorSettings = z.infer<typeof editorSettingsSchema>;

export const DEFAULT_EDITOR_SETTINGS: EditorSettings = {
  theme: "dark",
  fontSize: 14,
  fontFamily: "mono",
  tabSize: 4,
  insertSpaces: true,
  autocomplete: true,
  snippets: true,
  lineNumbers: true,
  showInvisibles: false,
  indentGuides: true,
  foldGutter: true,
  highlightActiveLine: true,
  wrap: false,
};

export const editorSettingsSnapshotSchema = editorSettingsSchema.extend({
  version: z.literal(EDITOR_SETTINGS_VERSION),
});

export function parseEditorSettings(data: unknown): EditorSettings {
  if (typeof data !== "object" || data === null) {
    return DEFAULT_EDITOR_SETTINGS;
  }
  if ((data as { version?: unknown }).version !== EDITOR_SETTINGS_VERSION) {
    return DEFAULT_EDITOR_SETTINGS;
  }
  const parsed = editorSettingsSchema.safeParse(data);
  return parsed.success ? parsed.data : DEFAULT_EDITOR_SETTINGS;
}
