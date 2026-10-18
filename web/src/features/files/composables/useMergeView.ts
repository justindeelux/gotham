import type { MergeSession, MergeSessionOptions } from "@/features/files/editor/merge";

/**
 * Dynamic entry point for the read-only history diff. The merge view lives in
 * the same lazy CodeMirror chunk as the editor (the type-only import above is
 * erased), so a diff tab never pulls CodeMirror into the entry bundle.
 */
let bundle: Promise<typeof import("@/features/files/editor/merge")> | null = null;

/** createMergeSession loads the editor chunk and builds one diff view. */
export async function createMergeSession(
  options: MergeSessionOptions,
): Promise<MergeSession> {
  bundle ??= import("@/features/files/editor/merge");
  const module = await bundle;
  return module.createMergeSession(options);
}

export type { MergeSession, MergeSessionOptions };
