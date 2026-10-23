import type { EditorLanguageId } from "@/features/files/utils/editor";

export type CodeEditorPreset = "yaml" | "dockerfile" | "shell" | "env" | "nginx";

export const CODE_EDITOR_PRESETS: Record<CodeEditorPreset, EditorLanguageId> = {
  yaml: "yaml",
  dockerfile: "dockerfile",
  shell: "shell",
  env: "ini",
  nginx: "nginx",
};
