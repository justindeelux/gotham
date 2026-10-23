import {
  EDITOR_DRAFT_MAX_CHARS,
  EDITOR_DRAFT_MAX_DRAFTS,
  EDITOR_DRAFT_TTL_MS,
  EDITOR_DRAFT_VERSION,
  editorDraftSchema,
} from "@/features/files/schemas/editorDraft";
import type { EditorDraft } from "@/features/files/schemas/editorDraft";
import { editorDraftStorageKeyPrefix } from "@/shared/api/token";

export type EditorDraftStorage = Pick<
  Storage,
  "getItem" | "setItem" | "removeItem" | "key" | "length"
>;

export type EditorDraftInput = Omit<EditorDraft, "version">;

export function editorDraftStoragePrefix(
  serverId: string,
  userId: string,
): string {
  if (serverId === "" || userId === "") {
    return "";
  }
  return `${editorDraftStorageKeyPrefix}${encodeURIComponent(serverId)}.${encodeURIComponent(userId)}.`;
}

export function editorDraftStorageKey(
  prefix: string,
  root: string,
  path: string,
): string {
  if (prefix === "") {
    return "";
  }
  return `${prefix}${encodeURIComponent(root)}/${encodeURIComponent(path)}`;
}

function forgetQuietly(storage: EditorDraftStorage, key: string): void {
  try {
    storage.removeItem(key);
  } catch {
  }
}

function keysWithPrefix(storage: EditorDraftStorage, prefix: string): string[] {
  const keys: string[] = [];
  let count: number;
  try {
    count = storage.length;
  } catch {
    return keys;
  }
  for (let index = 0; index < count; index += 1) {
    try {
      const key = storage.key(index);
      if (key !== null && key.startsWith(prefix)) {
        keys.push(key);
      }
    } catch {
    }
  }
  return keys;
}

function savedAtOf(raw: string): number | null {
  try {
    const value = JSON.parse(raw) as { savedAt?: unknown };
    return typeof value.savedAt === "number" && Number.isFinite(value.savedAt)
      ? value.savedAt
      : null;
  } catch {
    return null;
  }
}

function liveDrafts(
  storage: EditorDraftStorage,
  prefix: string,
  now: number,
): { key: string; savedAt: number }[] {
  const live: { key: string; savedAt: number }[] = [];
  for (const key of keysWithPrefix(storage, prefix)) {
    let raw: string | null;
    try {
      raw = storage.getItem(key);
    } catch {
      continue;
    }
    if (raw === null) {
      continue;
    }
    const savedAt = savedAtOf(raw);
    if (
      raw.length > EDITOR_DRAFT_MAX_CHARS ||
      savedAt === null ||
      now - savedAt > EDITOR_DRAFT_TTL_MS
    ) {
      forgetQuietly(storage, key);
      continue;
    }
    live.push({ key, savedAt });
  }
  return live;
}

export function readEditorDraft(
  storage: EditorDraftStorage,
  key: string,
  now = Date.now(),
): EditorDraft | null {
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
  let data: unknown;
  try {
    data = JSON.parse(raw) as unknown;
    if (raw.length > EDITOR_DRAFT_MAX_CHARS) {
      throw new Error("oversized");
    }
  } catch {
    forgetQuietly(storage, key);
    return null;
  }
  const parsed = editorDraftSchema.safeParse(data);
  if (!parsed.success || now - parsed.data.savedAt > EDITOR_DRAFT_TTL_MS) {
    forgetQuietly(storage, key);
    return null;
  }
  return parsed.data;
}

export function removeEditorDraft(
  storage: EditorDraftStorage,
  key: string,
): void {
  if (key !== "") {
    forgetQuietly(storage, key);
  }
}

export function writeEditorDraft(
  storage: EditorDraftStorage,
  prefix: string,
  draft: EditorDraftInput,
  now = Date.now(),
): boolean {
  if (prefix === "") {
    return false;
  }
  const key = editorDraftStorageKey(prefix, draft.root, draft.path);
  const raw = JSON.stringify({ version: EDITOR_DRAFT_VERSION, ...draft });
  if (raw.length > EDITOR_DRAFT_MAX_CHARS) {
    return false;
  }
  const others = liveDrafts(storage, prefix, now).filter(
    (entry) => entry.key !== key,
  );
  const excess = others.length - (EDITOR_DRAFT_MAX_DRAFTS - 1);
  if (excess > 0) {
    others.sort((a, b) => a.savedAt - b.savedAt);
    for (const entry of others.slice(0, excess)) {
      forgetQuietly(storage, entry.key);
    }
  }
  try {
    storage.setItem(key, raw);
    return true;
  } catch {
    const survivors = liveDrafts(storage, prefix, now).filter(
      (entry) => entry.key !== key,
    );
    survivors.sort((a, b) => a.savedAt - b.savedAt);
    const oldest = survivors[0];
    if (!oldest) {
      return false;
    }
    forgetQuietly(storage, oldest.key);
    try {
      storage.setItem(key, raw);
      return true;
    } catch {
      return false;
    }
  }
}
