import {
  FILES_TABS_MAX_BYTES,
  FILES_TABS_MAX_TABS,
  filesTabsSnapshotSchema,
} from "@/features/files/schemas/tabsPersist";
import type { FilesTabsSnapshot } from "@/features/files/schemas/tabsPersist";
import type { FilesPaneId } from "@/features/files/stores/paneSession";
import { parseWith } from "@/shared/validation/parse";

export type FilesTabsStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

export function filesTabsStorageKey(
  serverId: string,
  userId: string,
  pane: FilesPaneId = "left",
): string {
  if (serverId === "" || userId === "") {
    return "";
  }
  return `gotham.files.tabs.v2.${encodeURIComponent(serverId)}.${encodeURIComponent(userId)}.${pane}`;
}

/** Pre-pane key: its snapshot becomes the left pane's tabs on first load. */
export function legacyFilesTabsStorageKey(
  serverId: string,
  userId: string,
): string {
  if (serverId === "" || userId === "") {
    return "";
  }
  return `gotham.files.tabs.v1.${encodeURIComponent(serverId)}.${encodeURIComponent(userId)}`;
}

function forgetQuietly(storage: FilesTabsStorage, key: string): void {
  try {
    storage.removeItem(key);
  } catch {
  }
}

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
