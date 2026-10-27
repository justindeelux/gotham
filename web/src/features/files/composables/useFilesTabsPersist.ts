import { onBeforeUnmount, watch } from "vue";

import { useAuthStore } from "@/features/auth/stores/auth";
import type { FilesTabsSnapshot } from "@/features/files/schemas/tabsPersist";
import { useFilesStore } from "@/features/files/stores/files";
import type { FilesPaneId } from "@/features/files/stores/paneSession";
import {
  filesTabsStorageKey,
  legacyFilesTabsStorageKey,
  readFilesTabsSnapshot,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";

export const FILES_TABS_PERSIST_DEBOUNCE_MS = 250;

export function useFilesTabsPersist(
  getServerId: () => string,
  pane: FilesPaneId = "left",
): {
  initBrowser: (_serverId: string) => Promise<void>;
} {
  const files = useFilesStore(pane);
  const auth = useAuthStore();

  let timer: ReturnType<typeof setTimeout> | null = null;
  let boundServer = "";
  let boundUser = "";
  const decided = new Set<string>();

  function storage(): Pick<Storage, "getItem" | "setItem" | "removeItem"> | null {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  }

  function persistNow(serverId: string, userId: string): void {
    const key = filesTabsStorageKey(serverId, userId, pane);
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

  function schedulePersist(): void {
    if (timer !== null) {
      return;
    }
    timer = setTimeout(() => {
      timer = null;
      persistNow(getServerId(), auth.user?.id ?? "");
    }, FILES_TABS_PERSIST_DEBOUNCE_MS);
  }

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
    flush();
  });

  function readFirstUseSnapshot(
    store: Pick<Storage, "getItem" | "setItem" | "removeItem"> | null,
    serverId: string,
    userId: string,
    key: string,
  ): FilesTabsSnapshot | null {
    if (store === null) {
      return null;
    }
    const own = key !== "" ? readFilesTabsSnapshot(store, key) : null;
    if (own !== null || pane !== "left") {
      return own;
    }
    const legacy = legacyFilesTabsStorageKey(serverId, userId);
    return legacy !== "" ? readFilesTabsSnapshot(store, legacy) : null;
  }

  async function initBrowser(serverId: string): Promise<void> {
    flush();
    const userId = auth.user?.id ?? "";
    const key = filesTabsStorageKey(serverId, userId, pane);
    const store = storage();
    const snapshot = readFirstUseSnapshot(store, serverId, userId, key);
    await files.init(serverId, snapshot);
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
