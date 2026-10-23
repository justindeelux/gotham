import type {
  EditorAccessibility,
  EditorSession,
  EditorSessionOptions,
} from "@/features/files/editor/codemirror";

let bundle: Promise<typeof import("@/features/files/editor/codemirror")> | null = null;

export async function createEditorSession(
  options: EditorSessionOptions,
): Promise<EditorSession> {
  bundle ??= import("@/features/files/editor/codemirror");
  const module = await bundle;
  return module.createEditorSession(options);
}

export type { EditorAccessibility, EditorSession, EditorSessionOptions };
