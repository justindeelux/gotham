import {
  EDITOR_DRAFT_MAX_CHARS,
  EDITOR_DRAFT_MAX_DRAFTS,
  EDITOR_DRAFT_TTL_MS,
  EDITOR_DRAFT_VERSION,
  editorDraftSchema,
} from "@/features/files/schemas/editorDraft";
import type { EditorDraft } from "@/features/files/schemas/editorDraft";
import { editorDraftStorageKeyPrefix } from "@/shared/api/token";

/**
 * Minimal storage surface for editor drafts, so unit tests can pass a fake
 * (jsdom's localStorage works too). Every access is wrapped in try/catch: private
 * mode, quota and blocked storage must never break the editor.
 */
export type EditorDraftStorage = Pick<
  Storage,
  "getItem" | "setItem" | "removeItem" | "key" | "length"
>;

/** EditorDraftInput is one draft's payload; the version is added on write. */
export type EditorDraftInput = Omit<EditorDraft, "version">;

/**
 * editorDraftStoragePrefix scopes drafts by server/node and user: two nodes
 * never share a draft, and another account on the same browser never sees one.
 * Empty segments mean "no identity yet" — the caller skips persistence.
 */
export function editorDraftStoragePrefix(
  serverId: string,
  userId: string,
): string {
  if (serverId === "" || userId === "") {
    return "";
  }
  return `${editorDraftStorageKeyPrefix}${encodeURIComponent(serverId)}.${encodeURIComponent(userId)}.`;
}

/**
 * editorDraftStorageKey builds the key for one file. encodeURIComponent escapes
 * "/" (so it is a safe separator) and the value is never parsed back: root and
 * path travel inside the stored draft.
 */
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

/** forgetQuietly drops one key, never throwing. */
function forgetQuietly(storage: EditorDraftStorage, key: string): void {
  try {
    storage.removeItem(key);
  } catch {
    // Blocked storage: the stale entry stays, the editor still works.
  }
}

/** keysWithPrefix lists the storage keys under one draft prefix. */
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
      // A blocked read just hides that entry.
    }
  }
  return keys;
}

/** savedAtOf reads only the timestamp, so pruning never parses big bodies. */
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

/**
 * liveDrafts scans one prefix and drops entries that are corrupt, oversized or
 * older than the retention window. Returns the surviving key/timestamp pairs.
 */
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

/**
 * readEditorDraft loads and validates one draft. Corrupt JSON, a wrong version,
 * an oversized payload or an expired timestamp returns null, and the bad entry
 * is removed so it cannot linger.
 */
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

/** removeEditorDraft drops one draft, never throwing. */
export function removeEditorDraft(
  storage: EditorDraftStorage,
  key: string,
): void {
  if (key !== "") {
    forgetQuietly(storage, key);
  }
}

/**
 * writeEditorDraft stores one draft after pruning expired entries and evicting
 * the oldest drafts beyond the retention cap. An oversized payload is refused,
 * and every failure returns false — the editor keeps working without storage.
 */
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
  // Keep room for the draft being written, oldest evicted first.
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
    // Quota: drop the oldest draft and retry once; without room this stays a
    // silent no-op so the editor never fails because storage is full.
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
