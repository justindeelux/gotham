import { onBeforeUnmount, watch } from "vue";

import { useAuthStore } from "@/features/auth/stores/auth";
import { useFilesStore } from "@/features/files/stores/files";
import {
  filesTabsStorageKey,
  readFilesTabsSnapshot,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";

export const FILES_TABS_PERSIST_DEBOUNCE_MS = 250;

export function useFilesTabsPersist(getServerId: () => string): {
  initBrowser: (_serverId: string) => Promise<void>;
} {
  const files = useFilesStore();
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
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  });

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
