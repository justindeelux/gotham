import type { FileKind, FilesEntry } from "@/features/files/schemas/files";
import { sortEntries } from "@/features/files/utils/files";

export interface ExplorerDirState {
  path: string;
  entries: FilesEntry[];
  loaded: boolean;
  loading: boolean;
  truncated: boolean;
  cursor: string;
  error: unknown;
}

export type ExplorerStatus = "loading" | "error" | "empty";

export interface ExplorerEntryRow {
  type: "entry";
  path: string;
  name: string;
  kind: FileKind;
  depth: number;
  expanded: boolean;
  setsize: number;
  posinset: number;
  entry: FilesEntry;
}

export interface ExplorerStatusRow {
  type: "status";
  path: string;
  dir: string;
  depth: number;
  status: ExplorerStatus;
  error: unknown;
  setsize: number;
  posinset: number;
}

export type ExplorerRow = ExplorerEntryRow | ExplorerStatusRow;

export const EXPLORER_ROW_HEIGHT = 26;

export type ExplorerDialogMode =
  | "create-file"
  | "create-folder"
  | "rename"
  | "delete";

export function statusRowPath(dir: string, status: ExplorerStatus): string {
  return `${dir}\u0000${status}`;
}

export function flattenTree(
  rootPath: string,
  dirs: Readonly<Record<string, ExplorerDirState>>,
  isExpanded: (_path: string) => boolean,
): ExplorerRow[] {
  const rows: ExplorerRow[] = [];
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

export function firstEntryIndex(rows: readonly ExplorerRow[]): number {
  return nextEntryIndex(rows, 0, 1);
}

export function lastEntryIndex(rows: readonly ExplorerRow[]): number {
  return nextEntryIndex(rows, rows.length - 1, -1);
}
