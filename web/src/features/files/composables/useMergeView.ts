import type { MergeSession, MergeSessionOptions } from "@/features/files/editor/merge";

let bundle: Promise<typeof import("@/features/files/editor/merge")> | null = null;

export async function createMergeSession(
  options: MergeSessionOptions,
): Promise<MergeSession> {
  bundle ??= import("@/features/files/editor/merge");
  const module = await bundle;
  return module.createMergeSession(options);
}

export type { MergeSession, MergeSessionOptions };
