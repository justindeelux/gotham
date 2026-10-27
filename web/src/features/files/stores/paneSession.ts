import { defineStore } from "pinia";
import { ref, watch } from "vue";

import type { FilesClipboard } from "@/features/files/stores/files";
import { FILES_SPLIT_VERSION } from "@/features/files/schemas/splitView";
import {
  readFilesSplit,
  writeFilesSplit,
} from "@/features/files/utils/splitView";
import { getSession } from "@/shared/api/token";

export const FILES_PANE_IDS = ["left", "right"] as const;

export type FilesPaneId = (typeof FILES_PANE_IDS)[number];

export function otherPane(pane: FilesPaneId): FilesPaneId {
  return pane === "left" ? "right" : "left";
}

type ListingRefresher = (
  _serverId: string,
  _root: string,
  _dirs: string[],
) => Promise<void>;

const listingRefreshers = new Map<FilesPaneId, ListingRefresher>();

export function registerListingRefresher(
  pane: FilesPaneId,
  refresh: ListingRefresher,
): void {
  listingRefreshers.set(pane, refresh);
}

export async function refreshOtherPaneListings(
  source: FilesPaneId,
  serverId: string,
  root: string,
  dirs: string[],
): Promise<void> {
  const jobs = [...listingRefreshers.entries()]
    .filter(([pane]) => pane !== source)
    .map(([, refresh]) => refresh(serverId, root, dirs));
  await Promise.all(jobs);
}

/** Refresh every mounted pane showing this location, whatever pane started it. */
export async function refreshAllPaneListings(
  serverId: string,
  root: string,
  dirs: string[],
): Promise<void> {
  const jobs = [...listingRefreshers.values()].map((refresh) =>
    refresh(serverId, root, dirs),
  );
  await Promise.all(jobs);
}

export const useFilesPaneSession = defineStore("filesPanes", () => {
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  const userId = ref<string>(getSession().user?.id ?? "");
  const stored = storage ? readFilesSplit(storage, userId.value) : null;

  const activePane = ref<FilesPaneId>("left");
  const clipboard = ref<FilesClipboard | null>(null);
  const splitEnabled = ref<boolean>(stored?.enabled ?? false);
  const secondaryServerId = ref<string>(stored?.secondaryServerId ?? "");

  function activate(pane: FilesPaneId): void {
    activePane.value = pane;
  }

  function clearClipboard(): void {
    clipboard.value = null;
  }

  function enableSplit(serverId: string): void {
    if (secondaryServerId.value === "" && serverId !== "") {
      secondaryServerId.value = serverId;
    }
    splitEnabled.value = true;
  }

  function disableSplit(): void {
    splitEnabled.value = false;
    activePane.value = "left";
  }

  function setSecondaryServerId(serverId: string): void {
    secondaryServerId.value = serverId;
  }

  function reset(): void {
    activePane.value = "left";
    clipboard.value = null;
    splitEnabled.value = false;
    secondaryServerId.value = "";
    userId.value = "";
  }

  function reloadForUser(nextUserId: string): void {
    userId.value = nextUserId;
    const state = storage ? readFilesSplit(storage, nextUserId) : null;
    splitEnabled.value = state?.enabled ?? false;
    secondaryServerId.value = state?.secondaryServerId ?? "";
    activePane.value = "left";
    clipboard.value = null;
  }

  watch([splitEnabled, secondaryServerId, userId], () => {
    if (storage === null || userId.value === "") {
      return;
    }
    writeFilesSplit(storage, userId.value, {
      version: FILES_SPLIT_VERSION,
      enabled: splitEnabled.value,
      secondaryServerId: secondaryServerId.value,
    });
  });

  return {
    activePane,
    clipboard,
    splitEnabled,
    secondaryServerId,
    activate,
    clearClipboard,
    enableSplit,
    disableSplit,
    setSecondaryServerId,
    reset,
    reloadForUser,
  };
});
