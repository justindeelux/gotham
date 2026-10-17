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
 * localStorage: debounced writes on change, a flush on pagehide/hide, and a
 * restore after the roots load. Every storage access is guarded — without
 * storage the page works with defaults. No identity (no user) means no
 * persistence, so one browser never leaks tabs across accounts.
 */
export function useFilesTabsPersist(getServerId: () => string): {
  initBrowser: (_serverId: string) => Promise<void>;
} {
  const files = useFilesStore();
  const auth = useAuthStore();

  let timer: ReturnType<typeof setTimeout> | null = null;
  /** boundServer/User pin a write to the state it was read from. */
  let boundServer = "";
  let boundUser = "";

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
    if (serverId === "" || userId === "" || files.serverId !== serverId) {
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
    writeFilesTabsSnapshot(store, filesTabsStorageKey(serverId, userId), snapshot);
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
   * initBrowser loads one server's listing, then restores its stored tabs.
   * A pending write for the previous server is flushed first so a fast node
   * switch never loses the tabs being left behind.
   */
  async function initBrowser(serverId: string): Promise<void> {
    flush();
    await files.init(serverId);
    boundServer = serverId;
    boundUser = auth.user?.id ?? "";
    if (files.serverId !== serverId) {
      return;
    }
    const store = storage();
    if (store === null || boundUser === "") {
      return;
    }
    const snapshot = readFilesTabsSnapshot(
      store,
      filesTabsStorageKey(serverId, boundUser),
    );
    if (snapshot === null || files.serverId !== serverId) {
      return;
    }
    await files.restorePersistedTabs(snapshot);
  }

  return { initBrowser };
}
