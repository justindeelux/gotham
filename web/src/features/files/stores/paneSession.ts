import { defineStore } from "pinia";
import { ref } from "vue";

import type { FilesClipboard } from "@/features/files/stores/files";

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

export const useFilesPaneSession = defineStore("filesPanes", () => {
  const activePane = ref<FilesPaneId>("left");
  const clipboard = ref<FilesClipboard | null>(null);

  function activate(pane: FilesPaneId): void {
    activePane.value = pane;
  }

  function clearClipboard(): void {
    clipboard.value = null;
  }

  function reset(): void {
    activePane.value = "left";
    clipboard.value = null;
  }

  return { activePane, clipboard, activate, clearClipboard, reset };
});
