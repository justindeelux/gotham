import { z } from "zod";

/**
 * Editor preferences (JUS-219): theme, font, indentation and display toggles
 * chosen in the editor's settings popover. Every field carries its own
 * `.catch(default)`, so one corrupt or unknown value only resets that field;
 * an unknown version or a non-object payload falls back to the full defaults.
 * Unknown keys are stripped by zod, so a newer client's extra fields never
 * break an older one.
 */
export const EDITOR_SETTINGS_VERSION = 1 as const;

/** EDITOR_FONT_SIZES is the offered size scale, in CSS pixels. */
export const EDITOR_FONT_SIZES = [11, 12, 13, 14, 16, 18, 20] as const;

/** EDITOR_TAB_SIZES is the offered indentation widths. */
export const EDITOR_TAB_SIZES = [2, 4, 8] as const;

/** editorSettingsSchema validates the preference fields themselves. */
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
  // Added by JUS-227. A preference file written before that has no such key:
  // `.default(true)` fills it in instead of failing the whole parse, and
  // `.catch(true)` resets only this field when the stored value is bogus.
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

/** DEFAULT_EDITOR_SETTINGS is the factory preference set. */
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

/** editorSettingsSnapshotSchema is the versioned shape written to storage. */
export const editorSettingsSnapshotSchema = editorSettingsSchema.extend({
  version: z.literal(EDITOR_SETTINGS_VERSION),
});

/**
 * parseEditorSettings resolves untrusted stored data into a complete setting
 * set. A wrong version is treated as unreadable (defaults) rather than
 * misread; a missing or mistyped field keeps its default.
 */
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
