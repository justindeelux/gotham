import { onBeforeUnmount, watch } from "vue";

import { useAuthStore } from "@/features/auth/stores/auth";
import { useFilesStore } from "@/features/files/stores/files";
import {
  filesTabsStorageKey,
  readFilesTabsSnapshot,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";

/** FILES_TABS_PERSIST_DEBOUNCE_MS batches rapid tab/path/view changes. */
export const FILES_TABS_PERSIST_DEBOUNCE_MS = 250;

/**
 * useFilesTabsPersist saves the browser's tabs and restores them per
 * user+server (JUS-209). The tab strip and every navigation stay the store's
 * job; this only moves a validated snapshot between the store and
 * localStorage: the snapshot is read BEFORE init creates/loads the default
 * tab, writes are debounced, and a flush runs on pagehide/hide. Every storage
 * access is guarded — without storage the page works with defaults. No
 * identity (no user) means no persistence, so one browser never leaks tabs
 * across accounts.
 */
export function useFilesTabsPersist(getServerId: () => string): {
  initBrowser: (_serverId: string) => Promise<void>;
} {
  const files = useFilesStore();
  const auth = useAuthStore();

  let timer: ReturnType<typeof setTimeout> | null = null;
  /** boundServer/User pin a flush to the state it was read from. */
  let boundServer = "";
  let boundUser = "";
  /**
   * decided holds the storage keys whose restore decision has finished for
   * this session (JUS-209 fix round 1). NOTHING is written for a key before
   * that: on a slow node the default tab arms the debounce while the first
   * listing is still in flight, and an ungated write would persist the blank
   * defaults over the stored snapshot before it is ever read.
   */
  const decided = new Set<string>();

  /** storage reaches localStorage without throwing when it is blocked. */
  function storage(): Pick<Storage, "getItem" | "setItem" | "removeItem"> | null {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  }

  /** persistNow writes the current tabs when they belong to `serverId`. */
  function persistNow(serverId: string, userId: string): void {
    const key = filesTabsStorageKey(serverId, userId);
    if (key === "" || !decided.has(key) || files.serverId !== serverId) {
      return;
    }
    const store = storage();
    if (store === null) {
      return;
    }
    const snapshot = files.collectTabsSnapshot();
    if (snapshot === null) {
      return;
    }
    writeFilesTabsSnapshot(store, key, snapshot);
  }

  /** schedulePersist batches writes behind a short debounce. */
  function schedulePersist(): void {
    if (timer !== null) {
      return;
    }
    timer = setTimeout(() => {
      timer = null;
      persistNow(getServerId(), auth.user?.id ?? "");
    }, FILES_TABS_PERSIST_DEBOUNCE_MS);
  }

  /** flush drops a pending write and stores the latest state immediately. */
  function flush(): void {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
    persistNow(boundServer, boundUser);
  }

  watch(
    [
      () => files.tabs.map((tab) => `${tab.id} ${tab.rootName} ${tab.path}`),
      () => files.activeTabId,
      () => files.activeTab.viewMode,
      () => auth.user?.id ?? "",
    ],
    () => schedulePersist(),
  );

  function onVisibility(): void {
    if (document.visibilityState === "hidden") {
      flush();
    }
  }

  window.addEventListener("pagehide", flush);
  document.addEventListener("visibilitychange", onVisibility);
  onBeforeUnmount(() => {
    window.removeEventListener("pagehide", flush);
    document.removeEventListener("visibilitychange", onVisibility);
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  });

  /**
   * initBrowser loads one server's listing, restoring its stored tabs. The
   * snapshot is read BEFORE init runs, so the initial load goes straight to
   * the restored tabs (one fetch for the active tab, no flash of the default
   * root); a concurrent init joins the in-flight load in the store instead of
   * restoring against an empty root list. A pending write for the previous
   * server is flushed first so a fast node switch never loses the tabs being
   * left behind.
   */
  async function initBrowser(serverId: string): Promise<void> {
    flush();
    const userId = auth.user?.id ?? "";
    const key = filesTabsStorageKey(serverId, userId);
    const store = storage();
    const snapshot =
      store !== null && key !== ""
        ? readFilesTabsSnapshot(store, key)
        : null;
    await files.init(serverId, snapshot);
    // A newer navigation may have superseded this one while it was in flight;
    // only the current server owns the bindings and the restore decision.
    if (getServerId() !== serverId) {
      return;
    }
    boundServer = serverId;
    boundUser = userId;
    if (key !== "") {
      decided.add(key);
      schedulePersist();
    }
  }

  return { initBrowser };
}
