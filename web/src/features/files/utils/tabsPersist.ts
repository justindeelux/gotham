import {
  FILES_TABS_MAX_BYTES,
  FILES_TABS_MAX_TABS,
  filesTabsSnapshotSchema,
} from "@/features/files/schemas/tabsPersist";
import type { FilesTabsSnapshot } from "@/features/files/schemas/tabsPersist";
import { parseWith } from "@/shared/validation/parse";

/**
 * Minimal storage surface for the tab snapshot, so unit tests can pass a fake
 * (jsdom's localStorage works too). Every access is wrapped in try/catch by
 * the callers below: private mode, quota and blocked storage must never break
 * the page.
 */
export type FilesTabsStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

/**
 * filesTabsStorageKey scopes the snapshot by server/node and user: different
 * nodes never share state, and a different user on the same browser never sees
 * another account's tabs. Logout deliberately keeps the stored state (no wipe):
 * the key already separates accounts, so the user comes back to the same
 * layout after re-login and nothing can leak across accounts. Empty segments
 * mean "no identity yet" — the caller skips persistence instead of writing a
 * pooled key.
 */
export function filesTabsStorageKey(serverId: string, userId: string): string {
  if (serverId === "" || userId === "") {
    return "";
  }
  return `gotham.files.tabs.v1.${encodeURIComponent(serverId)}.${encodeURIComponent(userId)}`;
}

/** forgetQuietly drops one key, never throwing. */
function forgetQuietly(storage: FilesTabsStorage, key: string): void {
  try {
    storage.removeItem(key);
  } catch {
    // Blocked storage: the stale entry stays, the page still works.
  }
}

/**
 * readFilesTabsSnapshot loads and validates one snapshot. Corrupt JSON, a
 * wrong version, an empty tab list or an oversized payload returns null (and
 * the bad entry is dropped so the next save starts clean) — the page falls
 * back to defaults instead of crashing.
 */
export function readFilesTabsSnapshot(
  storage: FilesTabsStorage,
  key: string,
): FilesTabsSnapshot | null {
  if (key === "") {
    return null;
  }
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    return null;
  }
  if (raw === null || raw === "" || raw.length > FILES_TABS_MAX_BYTES) {
    if (raw !== null && raw !== "") {
      forgetQuietly(storage, key);
    }
    return null;
  }
  let data: unknown;
  try {
    data = JSON.parse(raw) as unknown;
  } catch {
    forgetQuietly(storage, key);
    return null;
  }
  // Cap before validating: extras are dropped, the rest still restores.
  if (typeof data === "object" && data !== null && Array.isArray((data as { tabs?: unknown }).tabs)) {
    (data as { tabs: unknown[] }).tabs = (data as { tabs: unknown[] }).tabs.slice(
      0,
      FILES_TABS_MAX_TABS,
    );
  }
  try {
    return parseWith(filesTabsSnapshotSchema, data, {
      context: "FilesTabsSnapshot",
      strict: true,
    });
  } catch {
    forgetQuietly(storage, key);
    return null;
  }
}

/**
 * writeFilesTabsSnapshot stores one snapshot, capped to the newest tabs.
 * Oversized payloads are refused and every failure returns false — the page
 * keeps working without storage.
 */
export function writeFilesTabsSnapshot(
  storage: FilesTabsStorage,
  key: string,
  snapshot: FilesTabsSnapshot,
): boolean {
  if (key === "") {
    return false;
  }
  try {
    const capped: FilesTabsSnapshot = {
      ...snapshot,
      tabs: snapshot.tabs.slice(0, FILES_TABS_MAX_TABS),
    };
    const raw = JSON.stringify(capped);
    if (raw.length > FILES_TABS_MAX_BYTES) {
      return false;
    }
    storage.setItem(key, raw);
    return true;
  } catch {
    return false;
  }
}
