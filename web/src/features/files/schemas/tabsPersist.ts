import { z } from "zod";

/**
 * Versioned localStorage snapshot of the file browser's tab state (JUS-209):
 * the open tabs (each tab's id, root and path), the active tab and the view
 * mode. The version is a literal so a snapshot written by a newer client fails
 * validation and the page falls back to defaults instead of misreading it.
 */
export const FILES_TABS_SNAPSHOT_VERSION = 1 as const;

/** FILES_TABS_MAX_TABS caps the stored tabs; the oldest extras are dropped. */
export const FILES_TABS_MAX_TABS = 10;

/**
 * FILES_TABS_MAX_BYTES caps the raw snapshot (a runaway snapshot is ignored,
 * never stored or parsed).
 */
export const FILES_TABS_MAX_BYTES = 32 * 1024;

/** filesPersistedTabSchema validates one stored tab (id, root, path). */
export const filesPersistedTabSchema = z.object({
  id: z.string().min(1).max(128),
  root: z.string().min(1).max(256),
  path: z.string().max(1024),
});
export type FilesPersistedTab = z.infer<typeof filesPersistedTabSchema>;

/** filesTabsSnapshotSchema validates the whole stored browser state. */
export const filesTabsSnapshotSchema = z.object({
  version: z.literal(FILES_TABS_SNAPSHOT_VERSION),
  tabs: z.array(filesPersistedTabSchema).min(1),
  activeTabId: z.string().min(1),
  viewMode: z.enum(["list", "grid"]),
});
export type FilesTabsSnapshot = z.infer<typeof filesTabsSnapshotSchema>;
