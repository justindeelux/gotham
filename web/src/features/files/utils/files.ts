import { activeLocale } from "@/shared/i18n/locale";
import en from "@/features/files/locales/en";
import vi from "@/features/files/locales/vi";

import type { FileKind, FilesEntry } from "@/features/files/schemas/files";

/**
 * Pure helpers behind the read-only file browser: sorting, breadcrumbs,
 * selection eligibility, POSIX display formatting and the type label/icon.
 * Kept dependency-light (locale state plus the static files catalogs, like
 * shared/utils/format.ts) so the page, the components and the unit harness
 * share one definition.
 */

/** Which layout the browser renders. */
export type FilesViewMode = "list" | "grid";

/** Sortable columns (the list header). */
export type FilesSortKey = "name" | "size" | "modified" | "kind" | "mode";

/** Sort direction. */
export type FilesSortDirection = "asc" | "desc";

/** Icon family the row/grid marker renders. */
export type FilesIconKind =
  | "folder"
  | "file"
  | "doc"
  | "image"
  | "archive"
  | "terminal"
  | "link"
  | "lock";

/** catalogFor selects the files dictionary for one locale. */
function catalogFor(locale?: string | null): typeof en {
  return (locale ?? activeLocale.value) === "vi" ? vi : en;
}

// Directories lead every listing regardless of direction, then regular files,
// then symlinks and special files (mirrors docs/design/files.html).
const kindRank: Record<FileKind, number> = {
  directory: 0,
  file: 1,
  symlink: 2,
  special: 3,
  unspecified: 4,
};

const imageExtensions = new Set([
  "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "tiff", "avif",
]);
const archiveExtensions = new Set([
  "zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar", "zst",
]);
const videoExtensions = new Set(["mp4", "mov", "mkv", "webm", "avi"]);
const audioExtensions = new Set(["mp3", "wav", "ogg", "flac", "m4a"]);
const logExtensions = new Set(["log", "out"]);

// Known labels are proper nouns and stay verbatim in every locale.
const extensionLabels: Record<string, string> = {
  yml: "YAML",
  yaml: "YAML",
  json: "JSON",
  jsonc: "JSON",
  env: "ENV",
  toml: "TOML",
  ini: "INI",
  conf: "Config",
  cfg: "Config",
  csv: "CSV",
  xml: "XML",
  pdf: "PDF",
  html: "HTML",
  css: "CSS",
  md: "Markdown",
  markdown: "Markdown",
  txt: "Text",
  sh: "Shell",
  bash: "Shell",
  zsh: "Shell",
  dockerfile: "Dockerfile",
  go: "Go",
  py: "Python",
  rb: "Ruby",
  rs: "Rust",
  java: "Java",
  c: "C",
  cpp: "C++",
  h: "C header",
  ts: "TypeScript",
  tsx: "TypeScript",
  js: "JavaScript",
  jsx: "JavaScript",
  sql: "SQL",
  lock: "Lockfile",
};

/** entryExtension returns the lower-case extension without the dot. */
export function entryExtension(name: string): string {
  const dot = name.lastIndexOf(".");
  if (dot <= 0 || dot === name.length - 1) {
    return "";
  }
  return name.slice(dot + 1).toLowerCase();
}

/** isSelectable reports whether a row offers the selection checkbox. */
export function isSelectable(entry: FilesEntry): boolean {
  return entry.kind === "file" || entry.kind === "directory";
}

/** joinPath appends one entry name to a relative path. */
export function joinPath(base: string, name: string): string {
  return base === "" ? name : `${base}/${name}`;
}

/** parentPath returns the enclosing relative path ("" at the root). */
export function parentPath(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}

/** BreadcrumbSegment is one clickable crumb: the root, then each path part. */
export interface BreadcrumbSegment {
  /** name is the display label (root name, or one directory name). */
  name: string;
  /** path is the relative path the crumb navigates to ("" = the root). */
  path: string;
  /** isRoot marks the first crumb (rendered with the drive icon). */
  isRoot: boolean;
}

/** breadcrumbSegments builds root → … → current path crumbs. */
export function breadcrumbSegments(
  rootName: string,
  path: string,
): BreadcrumbSegment[] {
  const segments: BreadcrumbSegment[] = [
    { name: rootName, path: "", isRoot: true },
  ];
  let accumulated = "";
  for (const part of path.split("/").filter(Boolean)) {
    accumulated = joinPath(accumulated, part);
    segments.push({ name: part, path: accumulated, isRoot: false });
  }
  return segments;
}

/** entryMatchesQuery is the client-side "filter this folder" predicate. */
export function entryMatchesQuery(entry: FilesEntry, query: string): boolean {
  const needle = query.trim().toLowerCase();
  return needle === "" || entry.name.toLowerCase().includes(needle);
}

/** timestampOf parses modified_at into epoch millis (NaN when unknown). */
function timestampOf(entry: FilesEntry): number {
  return new Date(entry.modified_at).getTime();
}

/** compareBy applies one sort key before the name tie-break. */
function compareBy(a: FilesEntry, b: FilesEntry, key: FilesSortKey): number {
  switch (key) {
    case "size":
      return a.size - b.size;
    case "modified":
      return timestampOf(a) - timestampOf(b);
    case "mode":
      return a.mode - b.mode;
    case "kind":
      return kindRank[a.kind] - kindRank[b.kind];
    case "name":
    default:
      return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  }
}

/**
 * sortEntries returns a new array ordered kind-first (directories, files,
 * symlinks, special) then by the active key, with a stable name tie-break.
 * The input is never mutated.
 */
export function sortEntries(
  entries: FilesEntry[],
  key: FilesSortKey,
  direction: FilesSortDirection,
): FilesEntry[] {
  const factor = direction === "asc" ? 1 : -1;
  return [...entries].sort((a, b) => {
    const group = kindRank[a.kind] - kindRank[b.kind];
    if (group !== 0) {
      return group;
    }
    const primary = compareBy(a, b, key);
    if (primary !== 0) {
      return primary * factor;
    }
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  });
}

/** formatMode renders a POSIX st_mode as "drwxr-xr-x". */
export function formatMode(mode: number): string {
  const typeBits = mode & 0o170000;
  const typeChar =
    typeBits === 0o040000
      ? "d"
      : typeBits === 0o120000
        ? "l"
        : typeBits === 0o140000
          ? "s"
          : typeBits === 0o010000
            ? "p"
            : typeBits === 0o020000
              ? "c"
              : typeBits === 0o060000
                ? "b"
                : "-";
  const rwx = (bits: number): string =>
    `${bits & 4 ? "r" : "-"}${bits & 2 ? "w" : "-"}${bits & 1 ? "x" : "-"}`;
  return (
    typeChar +
    rwx((mode >> 6) & 7) +
    rwx((mode >> 3) & 7) +
    rwx(mode & 7)
  );
}

/**
 * formatTimestamp renders modified_at as "YYYY-MM-DD HH:mm" in UTC. Timestamps
 * are shown as fixed monospace values (like the mockup), so the output does
 * not follow the UI locale and unit checks stay deterministic.
 */
export function formatTimestamp(iso: string | null | undefined): string {
  if (!iso) {
    return "—";
  }
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return "—";
  }
  const pad = (value: number): string => String(value).padStart(2, "0");
  return (
    `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}` +
    ` ${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`
  );
}

/** entryTypeLabel renders the Type column ("Folder", "YAML", "Image"…). */
export function entryTypeLabel(
  entry: FilesEntry,
  locale?: string | null,
): string {
  const t = catalogFor(locale).types;
  if (entry.kind === "directory") {
    return t.folder;
  }
  if (entry.kind === "symlink") {
    return t.symlink;
  }
  if (entry.kind === "special") {
    const typeBits = entry.mode & 0o170000;
    if (typeBits === 0o140000) {
      return t.socket;
    }
    if (typeBits === 0o010000) {
      return t.fifo;
    }
    if (typeBits === 0o020000 || typeBits === 0o060000) {
      return t.device;
    }
    return t.special;
  }
  const extension = entryExtension(entry.name);
  if (imageExtensions.has(extension)) {
    return t.image;
  }
  if (archiveExtensions.has(extension)) {
    return t.archive;
  }
  if (videoExtensions.has(extension)) {
    return t.video;
  }
  if (audioExtensions.has(extension)) {
    return t.audio;
  }
  if (logExtensions.has(extension)) {
    return t.log;
  }
  return extensionLabels[extension] ?? t.file;
}

/** entryIconKind maps an entry onto the row/grid marker icon family. */
export function entryIconKind(entry: FilesEntry): FilesIconKind {
  switch (entry.kind) {
    case "directory":
      return "folder";
    case "symlink":
      return "link";
    case "special":
    case "unspecified":
      return entry.kind === "unspecified" ? "file" : "terminal";
    default:
      break;
  }
  const extension = entryExtension(entry.name);
  if (imageExtensions.has(extension)) {
    return "image";
  }
  if (archiveExtensions.has(extension)) {
    return "archive";
  }
  if (logExtensions.has(extension)) {
    return "terminal";
  }
  if (extension === "yml" || extension === "yaml" || extension === "json") {
    return "doc";
  }
  return "file";
}

/** countByKind tallies the listing for the footer summary. */
export function countByKind(entries: FilesEntry[]): Record<FileKind, number> {
  const counts: Record<FileKind, number> = {
    directory: 0,
    file: 0,
    symlink: 0,
    special: 0,
    unspecified: 0,
  };
  for (const entry of entries) {
    counts[entry.kind] += 1;
  }
  return counts;
}
