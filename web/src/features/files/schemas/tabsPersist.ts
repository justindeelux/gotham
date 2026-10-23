import { z } from "zod";

export const FILES_TABS_SNAPSHOT_VERSION = 1 as const;

export const FILES_TABS_MAX_TABS = 10;

export const FILES_TABS_MAX_BYTES = 32 * 1024;

export const filesPersistedTabSchema = z.object({
  id: z.string().min(1).max(128),
  root: z.string().min(1).max(256),
  path: z.string().max(1024),
});
export type FilesPersistedTab = z.infer<typeof filesPersistedTabSchema>;

export const filesTabsSnapshotSchema = z.object({
  version: z.literal(FILES_TABS_SNAPSHOT_VERSION),
  tabs: z.array(filesPersistedTabSchema).min(1),
  activeTabId: z.string().min(1),
  viewMode: z.enum(["list", "grid"]),
});
export type FilesTabsSnapshot = z.infer<typeof filesTabsSnapshotSchema>;
