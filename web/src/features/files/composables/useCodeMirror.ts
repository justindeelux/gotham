import type {
  EditorAccessibility,
  EditorSession,
  EditorSessionOptions,
} from "@/features/files/editor/codemirror";

/**
 * Dynamic entry point for the code editor. The CodeMirror bundle (and every
 * grammar it imports) is reached only through this module's lazy `import()`, so
 * it lives in its own chunk and never inflates the entry bundle. The type-only
 * import above is erased at build time and creates no runtime edge.
 */
let bundle: Promise<typeof import("@/features/files/editor/codemirror")> | null = null;

/** createEditorSession loads the editor chunk and builds one session. */
export async function createEditorSession(
  options: EditorSessionOptions,
): Promise<EditorSession> {
  bundle ??= import("@/features/files/editor/codemirror");
  const module = await bundle;
  return module.createEditorSession(options);
}

export type { EditorAccessibility, EditorSession, EditorSessionOptions };
