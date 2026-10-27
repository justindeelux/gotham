import { filesSplitSchema } from "@/features/files/schemas/splitView";
import type { FilesSplitState } from "@/features/files/schemas/splitView";

export type FilesSplitStorage = Pick<Storage, "getItem" | "setItem">;

export function filesSplitStorageKey(userId: string): string {
  return userId === "" ? "" : `gotham.files.split.v1.${encodeURIComponent(userId)}`;
}

export function readFilesSplit(
  storage: FilesSplitStorage,
  userId: string,
): FilesSplitState | null {
  const key = filesSplitStorageKey(userId);
  if (key === "") {
    return null;
  }
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    return null;
  }
  if (raw === null || raw === "") {
    return null;
  }
  try {
    const parsed = filesSplitSchema.safeParse(JSON.parse(raw) as unknown);
    return parsed.success ? parsed.data : null;
  } catch {
    return null;
  }
}

export function writeFilesSplit(
  storage: FilesSplitStorage,
  userId: string,
  state: FilesSplitState,
): void {
  const key = filesSplitStorageKey(userId);
  if (key === "") {
    return;
  }
  try {
    storage.setItem(key, JSON.stringify(state));
  } catch {
  }
}
