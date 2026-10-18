import type { FileKind, FilesEntry } from "@/features/files/schemas/files";
import { sortEntries } from "@/features/files/utils/files";

/**
 * Pure tree maths behind the editor's file explorer (JUS-223).
 *
 * The explorer keeps one listing per expanded directory, then flattens the
 * visible part of the tree into a single row array. The flat array is what the
 * windowed renderer, the roving `aria-activedescendant` and the arrow-key
 * navigation all index into, so only this module decides what "the next row"
 * means.
 */

/** ExplorerDirState is one directory's cached listing and load outcome. */
export interface ExplorerDirState {
  path: string;
  entries: FilesEntry[];
  loaded: boolean;
  loading: boolean;
  truncated: boolean;
  cursor: string;
  /** error is the raw API failure (rendered through `describeFilesError`). */
  error: unknown;
}

/** ExplorerStatus names the inline state a directory can render in the tree. */
export type ExplorerStatus = "loading" | "error" | "empty";

/** ExplorerEntryRow is one file or folder in the flattened tree. */
export interface ExplorerEntryRow {
  type: "entry";
  /** path is root-relative (the same address the files API takes). */
  path: string;
  name: string;
  kind: FileKind;
  depth: number;
  /** expanded is meaningful for directories only. */
  expanded: boolean;
  /** setsize/posinset describe this row among its real siblings (ARIA). */
  setsize: number;
  posinset: number;
  entry: FilesEntry;
}

/**
 * ExplorerStatusRow is an inline child state: a directory that is loading,
 * failed to load, or loaded empty. `dir` is the directory it belongs to, so the
 * inline Retry reloads exactly that folder.
 */
export interface ExplorerStatusRow {
  type: "status";
  /** path is a synthetic key (a real name never contains NUL). */
  path: string;
  dir: string;
  depth: number;
  status: ExplorerStatus;
  error: unknown;
  /** A status row replaces a directory's child list, so it is its only sibling. */
  setsize: number;
  posinset: number;
}

export type ExplorerRow = ExplorerEntryRow | ExplorerStatusRow;

/**
 * EXPLORER_ROW_HEIGHT is the single row height shared by the window maths and
 * the CSS (`--ex-row-height`), so the two can never drift.
 */
export const EXPLORER_ROW_HEIGHT = 26;

/** ExplorerDialogMode selects the editor explorer's create/rename/delete modal. */
export type ExplorerDialogMode =
  | "create-file"
  | "create-folder"
  | "rename"
  | "delete";


/** statusRowPath builds the unique row key for one directory's inline state. */
export function statusRowPath(dir: string, status: ExplorerStatus): string {
  return `${dir}\u0000${status}`;
}

/**
 * flattenTree walks the expanded directories from `rootPath` and returns the
 * visible rows in display order (folders first, then names, both case-
 * insensitive). A directory is only walked when `isExpanded` says so, which is
 * what keeps expansion lazy: an unexpanded folder contributes one row and no
 * request. A loading, failed or empty directory contributes one status row in
 * place of its children.
 */
export function flattenTree(
  rootPath: string,
  dirs: Readonly<Record<string, ExplorerDirState>>,
  isExpanded: (_path: string) => boolean,
): ExplorerRow[] {
  const rows: ExplorerRow[] = [];
  // A corrupt listing could name a folder after one of its own parents; the
  // guard turns that into a finite tree instead of an infinite render.
  const visited = new Set<string>();

  function walk(dir: string, depth: number): void {
    if (visited.has(dir)) {
      return;
    }
    visited.add(dir);
    const state = dirs[dir];
    if (!state) {
      rows.push({
        type: "status",
        path: statusRowPath(dir, "loading"),
        dir,
        depth,
        status: "loading",
        error: null,
        setsize: 1,
        posinset: 1,
      });
      return;
    }
    // A refresh keeps the previous entries on screen; only a first load shows
    // the spinner row.
    if (state.loading && !state.loaded) {
      rows.push({
        type: "status",
        path: statusRowPath(dir, "loading"),
        dir,
        depth,
        status: "loading",
        error: null,
        setsize: 1,
        posinset: 1,
      });
      return;
    }
    if (state.error !== null) {
      rows.push({
        type: "status",
        path: statusRowPath(dir, "error"),
        dir,
        depth,
        status: "error",
        error: state.error,
        setsize: 1,
        posinset: 1,
      });
      return;
    }
    if (state.loaded && state.entries.length === 0) {
      rows.push({
        type: "status",
        path: statusRowPath(dir, "empty"),
        dir,
        depth,
        status: "empty",
        error: null,
        setsize: 1,
        posinset: 1,
      });
      return;
    }
    const entries = sortEntries(state.entries, "name", "asc");
    entries.forEach((entry, index) => {
      const path = dir === "" ? entry.name : `${dir}/${entry.name}`;
      const expanded = entry.kind === "directory" && isExpanded(path);
      rows.push({
        type: "entry",
        path,
        name: entry.name,
        kind: entry.kind,
        depth,
        expanded,
        setsize: entries.length,
        posinset: index + 1,
        entry,
      });
      if (expanded) {
        walk(path, depth + 1);
      }
    });
  }

  walk(rootPath, 0);
  return rows;
}

/** nextEntryIndex finds the closest entry row from `from` moving by `delta`. */
export function nextEntryIndex(
  rows: readonly ExplorerRow[],
  from: number,
  delta: 1 | -1,
): number {
  for (let index = from; index >= 0 && index < rows.length; index += delta) {
    if (rows[index].type === "entry") {
      return index;
    }
  }
  return -1;
}

/** firstEntryIndex returns the first entry row's index, or -1. */
export function firstEntryIndex(rows: readonly ExplorerRow[]): number {
  return nextEntryIndex(rows, 0, 1);
}

/** lastEntryIndex returns the last entry row's index, or -1. */
export function lastEntryIndex(rows: readonly ExplorerRow[]): number {
  return nextEntryIndex(rows, rows.length - 1, -1);
}
