import type { EditorLanguageId } from "@/features/files/utils/editor";

/**
 * Config formats the embedded code editor is used for (JUS-230). A preset is
 * only a grammar choice; theme, font and every other preference still come
 * from the shared editor settings.
 */
export type CodeEditorPreset = "yaml" | "dockerfile" | "shell" | "env" | "nginx";

/** CODE_EDITOR_PRESETS maps each preset onto an existing editor grammar. */
export const CODE_EDITOR_PRESETS: Record<CodeEditorPreset, EditorLanguageId> = {
  yaml: "yaml",
  dockerfile: "dockerfile",
  shell: "shell",
  // .env files are `KEY=value` / `# comment` lines, which the properties mode
  // highlights; there is no dedicated dotenv grammar.
  env: "ini",
  nginx: "nginx",
};
