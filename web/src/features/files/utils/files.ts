import { activeLocale } from "@/shared/i18n/locale";
import { formatBytes } from "@/shared/utils/format";
import en from "@/features/files/locales/en";
import vi from "@/features/files/locales/vi";

import type { ArchiveFormat } from "@/features/files/api/files";
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
// then symlinks and special files.
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

/** archiveFormatFromName returns the extract format the name's extension selects. */
export function archiveFormatFromName(name: string): ArchiveFormat | null {
  const lower = name.toLowerCase();
  if (lower.endsWith(".tar.gz") || lower.endsWith(".tgz")) {
    return "tar.gz";
  }
  if (lower.endsWith(".tar")) {
    return "tar";
  }
  if (lower.endsWith(".zip")) {
    return "zip";
  }
  return null;
}

/** isExtractableArchive reports a regular file the "Extract here…" action unpacks. */
export function isExtractableArchive(entry: FilesEntry): boolean {
  return entry.kind === "file" && archiveFormatFromName(entry.name) !== null;
}

/**
 * isImageFile reports a regular file the image viewer opens: one of the
 * viewer formats (JUS-204). It reuses the family the type label and the row
 * icon already classify as an image, so the three surfaces never disagree.
 */
export function isImageFile(entry: FilesEntry): boolean {
  return entry.kind === "file" && imageExtensions.has(entryExtension(entry.name));
}

/** EntryAction is the default action a double-click (or Enter) performs on a row. */
export type EntryAction = "directory" | "editor" | "extract" | "none";

/** EntryActionGates are the role/root gates the dispatch respects. */
export interface EntryActionGates {
  /** canOpenEditor is the owner/admin read gate the editor open needs. */
  canOpenEditor: boolean;
  /** canExtract is the write gate the Extract action needs. */
  canExtract: boolean;
}

/**
 * MAX_EDITABLE_BYTES is the editor's content ceiling: a larger body answers 413
 * on read and is offered as a download instead. It mirrors the control plane's
 * `files.MaxContentBytes` (2 MiB, internal/files/service.go).
 */
export const MAX_EDITABLE_BYTES = 2 << 20;

// Families the editor never decodes as text. The control plane decides binary
// by content (invalid UTF-8 / a NUL byte); from a listing the name is all there
// is, so the same image/audio/video/archive families the type label already
// classifies (plus fonts and PDF) are treated as non-text.
const binaryExtensions = new Set([
  ...imageExtensions,
  ...videoExtensions,
  ...audioExtensions,
  ...archiveExtensions,
  "pdf", "woff", "woff2", "ttf", "otf", "eot",
]);

/**
 * canOpenInEditor reports whether one listing row opens as editable text in the
 * code editor: a regular file within {@link MAX_EDITABLE_BYTES} and not a known
 * binary family. It is the name/size half of the editor-open decision, so a
 * double-click never opens a tab the editor can only show as download-only
 * (JUS-130, owner decision: such files do nothing).
 */
export function canOpenInEditor(entry: FilesEntry): boolean {
  return (
    entry.kind === "file" &&
    entry.size <= MAX_EDITABLE_BYTES &&
    !binaryExtensions.has(entryExtension(entry.name))
  );
}

/**
 * entryAction resolves an entry's default action (JUS-130): a folder navigates
 * into it, an extractable archive opens Extract (only when the root is
 * writable), a text/code file opens in the editor (only with the read role and
 * only when the editor can show it as text), and everything else — symlinks,
 * special files, binary/oversized files, a non-extractable archive — does
 * nothing. It is the one rule the double-click, Enter and the tests all read.
 */
export function entryAction(
  entry: FilesEntry,
  gates: EntryActionGates,
): EntryAction {
  if (entry.kind === "directory") {
    return "directory";
  }
  if (entry.kind !== "file") {
    return "none";
  }
  if (isExtractableArchive(entry)) {
    return gates.canExtract ? "extract" : "none";
  }
  if (!canOpenInEditor(entry)) {
    return "none";
  }
  return gates.canOpenEditor ? "editor" : "none";
}

/** archiveExtension returns the file extension for one compress format. */
export function archiveExtension(format: ArchiveFormat): string {
  switch (format) {
    case "zip":
      return ".zip";
    case "tar":
      return ".tar";
    case "tar.gz":
    default:
      return ".tar.gz";
  }
}

/** stripArchiveExtension drops a known archive suffix from a source name. */
function stripArchiveExtension(name: string): string {
  const lower = name.toLowerCase();
  for (const ext of [
    ".tar.gz", ".tgz", ".tar", ".zip", ".gz", ".bz2", ".xz", ".7z", ".rar", ".zst",
  ]) {
    if (lower.endsWith(ext)) {
      return name.slice(0, name.length - ext.length);
    }
  }
  return name;
}

/**
 * defaultArchiveName derives the default archive name: a single source keeps
 * its base (minus a known archive suffix), several sources use "archive". The
 * format decides the extension.
 */
export function defaultArchiveName(
  sources: FilesEntry[],
  format: ArchiveFormat,
): string {
  const base =
    sources.length === 1
      ? stripArchiveExtension(sources[0]?.name ?? "") || "archive"
      : "archive";
  return `${base}${archiveExtension(format)}`;
}

/** isSelectable reports whether a row offers the selection checkbox. */
export function isSelectable(entry: FilesEntry): boolean {
  return entry.kind === "file" || entry.kind === "directory";
}

/**
 * rangeNames returns the names of the selectable entries between `anchor` and
 * `target` inclusive, in the array's own order (JUS-138). The browser passes the
 * current sorted+filtered listing, so the scan is over the entry order and never
 * the DOM — a range spans rows a windowed list did not render (JUS-137). The
 * anchor and target may arrive in either order. Non-selectable entries (symlinks,
 * special files) carry no checkbox, so they are skipped rather than counted in a
 * selection they cannot join. Returns [] when either name is absent (a stale
 * anchor after a filter/sort/folder change).
 */
export function rangeNames(
  entries: FilesEntry[],
  anchor: string,
  target: string,
): string[] {
  const from = entries.findIndex((entry) => entry.name === anchor);
  const to = entries.findIndex((entry) => entry.name === target);
  if (from === -1 || to === -1) {
    return [];
  }
  const start = Math.min(from, to);
  const end = Math.max(from, to);
  return entries
    .slice(start, end + 1)
    .filter(isSelectable)
    .map((entry) => entry.name);
}

/**
 * typeaheadIndex resolves one type-to-focus keystroke against the visible
 * listing order (JUS-139): the index of the first name starting with `prefix`
 * (case-insensitive), scanning from `from` and wrapping, or -1 when nothing
 * matches. When every character of `prefix` is the same letter it is collapsed
 * to that one letter and the scan starts at `from`, so repeated presses of one
 * key cycle through the names that share it; a mixed prefix scans the whole
 * listing. The caller passes the sorted+filtered order, so a match is found
 * even when the row is outside the rendered window (JUS-137).
 */
export function typeaheadIndex(
  names: string[],
  prefix: string,
  from = 0,
): number {
  if (names.length === 0 || prefix === "") {
    return -1;
  }
  const first = prefix.slice(0, 1);
  const cycling = prefix.split("").every((char) => char === first);
  const needle = (cycling ? first : prefix).toLowerCase();
  const start = cycling ? from : 0;
  for (let offset = 0; offset < names.length; offset += 1) {
    const index = (start + offset) % names.length;
    if (names[index].toLowerCase().startsWith(needle)) {
      return index;
    }
  }
  return -1;
}

/**
 * MAX_NAME_BYTES is the maximum UTF-8 byte length the server accepts for a
 * single path segment (an entry name). The boundary is bytes, not code points,
 * so a multi-byte name can be shorter in characters and still fail.
 */
export const MAX_NAME_BYTES = 255;

/** NameKind distinguishes the create dialogs (an empty file or a folder). */
export type NameKind = "file" | "directory";

/**
 * validateEntryName returns null when name is a legal path segment, otherwise
 * the files catalog key of the first rule it breaks. It mirrors the server's
 * rules: the name is trimmed, must be non-empty, must not be "." or "..", must
 * not contain "/", "\" or NUL, and must be at most 255 bytes. The accepted
 * value is the trimmed string; callers submit {@link normalizeEntryName}.
 */
export function validateEntryName(name: string): string | null {
  const trimmed = name.trim();
  if (trimmed === "") {
    return "files.name.required";
  }
  if (trimmed === "." || trimmed === "..") {
    return "files.name.reserved";
  }
  if (trimmed.includes("/") || trimmed.includes("\\") || trimmed.includes("\0")) {
    return "files.name.invalid";
  }
  if (new TextEncoder().encode(trimmed).length > MAX_NAME_BYTES) {
    return "files.name.tooLong";
  }
  return null;
}

/** normalizeEntryName returns the trimmed name the dialogs submit. */
export function normalizeEntryName(name: string): string {
  return name.trim();
}

/** TransferProblem names why a move/copy destination is refused before the call. */
export type TransferProblem = "same-folder" | "into-itself";

/**
 * destinationProblem pre-validates a move/copy destination. The agent refuses a
 * transfer onto itself with 400 (`src == dst || isWithin(src, dst)`), so the
 * dialog must not offer the source folder or a descendant of a moved folder.
 * Returns null when the destination is valid.
 */
export function destinationProblem(
  source: string,
  destination: string,
  entries: Array<{ name: string; kind: FileKind }>,
): TransferProblem | null {
  const from = source.replace(/^\/+|\/+$/g, "");
  const to = destination.replace(/^\/+|\/+$/g, "");
  if (to === from) {
    return "same-folder";
  }
  for (const entry of entries) {
    if (entry.kind !== "directory") {
      continue;
    }
    const folder = joinPath(from, entry.name);
    if (to === folder || to.startsWith(`${folder}/`)) {
      return "into-itself";
    }
  }
  return null;
}

/**
 * isFileDrag reports whether a drag carries OS files. A non-file drag (a text
 * selection, a page element) must not be hijacked by the drop zone.
 */
export function isFileDrag(types?: ArrayLike<string> | null): boolean {
  return types != null && Array.from(types).includes("Files");
}

/** DropItemLike is the minimal DataTransferItem surface the classifier reads. */
export interface DropItemLike {
  kind: string;
  webkitGetAsEntry?: () => { isDirectory?: boolean } | null;
  getAsFile?: () => File | null;
}

/** DropDataLike is the minimal DataTransfer surface the classifier reads. */
export interface DropDataLike {
  items?: ArrayLike<DropItemLike>;
  files?: ArrayLike<File> | null;
}

/**
 * filesFromDrop separates a drop into uploadable files and rejected folders.
 * A dropped directory is a zero-byte File via dataTransfer.files, so it must be
 * detected through webkitGetAsEntry and reported instead of uploaded as an
 * empty file.
 */
export function filesFromDrop(
  data: DropDataLike | null,
): { files: File[]; folders: number } {
  if (!data) {
    return { files: [], folders: 0 };
  }
  const items = data.items;
  if (!items || items.length === 0) {
    return { files: data.files ? Array.from(data.files) : [], folders: 0 };
  }
  const files: File[] = [];
  let folders = 0;
  for (const item of Array.from(items)) {
    if (item.kind !== "file") {
      continue;
    }
    const entry = item.webkitGetAsEntry?.() ?? null;
    if (entry?.isDirectory) {
      folders += 1;
      continue;
    }
    const file = item.getAsFile?.() ?? null;
    if (file) {
      files.push(file);
    }
  }
  if (files.length === 0 && folders === 0 && data.files) {
    return { files: Array.from(data.files), folders: 0 };
  }
  return { files, folders };
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

/**
 * resolveSymlinkTarget maps a symlink's raw target onto a root-relative path
 * the browser can open, so "Go to target" lands on the real directory rather
 * than re-listing the link. A relative target resolves from the link's own
 * folder; an absolute target is mapped onto the root's absolute path when it
 * sits inside it. "" is a valid result: the target is the root's top level
 * (e.g. a link to ".." or to the root itself). Returns null only when the
 * target is not inside the browsable root — an empty target, one that climbs
 * above the root, or an absolute path outside it (or with an unknown root
 * path) — so the caller hides the action instead of offering a doomed one.
 */
export function resolveSymlinkTarget(
  dir: string,
  name: string,
  target: string,
  rootPath = "",
): string | null {
  if (target === "") {
    return null;
  }
  if (target.startsWith("/")) {
    if (rootPath === "") {
      return null;
    }
    const root = rootPath.replace(/\/+$/, "");
    const base = root === "" ? "/" : root;
    if (target === base) {
      return "";
    }
    const prefix = root === "" ? "/" : `${root}/`;
    if (!target.startsWith(prefix)) {
      return null;
    }
    return resolvePathSegments([], target.slice(prefix.length));
  }
  // The link's own folder is where a relative target resolves from.
  const folder = joinPath(dir, name).split("/");
  folder.pop();
  return resolvePathSegments(folder, target);
}

/** resolvePathSegments applies a target's "." / ".." segments to `base`. */
function resolvePathSegments(base: string[], target: string): string | null {
  const parts = [...base];
  for (const segment of target.split("/")) {
    if (segment === "" || segment === ".") {
      continue;
    }
    if (segment === "..") {
      if (parts.length === 0) {
        return null;
      }
      parts.pop();
      continue;
    }
    parts.push(segment);
  }
  return parts.join("/");
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
 * PERMISSION_BITS is the mask of the permission bits a chmod may change; the
 * special bits (setuid 0o4000, setgid 0o2000, sticky 0o1000) are never in it.
 */
export const PERMISSION_BITS = 0o777;

/** formatOctalMode renders the low permission bits as a 3-digit octal string. */
export function formatOctalMode(mode: number): string {
  return (mode & PERMISSION_BITS).toString(8).padStart(3, "0");
}

/**
 * formatOctalModeFull renders a mode for display: the three permission digits,
 * plus a leading special-bits digit (setuid 4, setgid 2, sticky 1) when one is
 * set — so "644" and "755" stay three digits and "4755"/"1777" are four. The
 * type bits are ignored, so files, directories and special files share it. The
 * dialog's editable field keeps using {@link formatOctalMode}: a chmod never
 * carries a special bit (the agent refuses one).
 */
export function formatOctalModeFull(mode: number): string {
  const bits = mode & 0o7777;
  return bits.toString(8).padStart(bits > PERMISSION_BITS ? 4 : 3, "0");
}

/** parseOctalMode converts a validated 3-digit octal string to a number. */
export function parseOctalMode(value: string): number {
  return Number.parseInt(value, 8) & PERMISSION_BITS;
}

/**
 * permissionFlags returns the nine permission bits in owner/group/other ×
 * rwx order (index 0 = owner read … index 8 = other execute), the order the
 * Permissions dialog's 3×3 grid renders.
 */
export function permissionFlags(mode: number): boolean[] {
  const m = mode & PERMISSION_BITS;
  return [8, 7, 6, 5, 4, 3, 2, 1, 0].map((shift) => ((m >> shift) & 1) === 1);
}

/** modeFromFlags rebuilds a mode number from the nine rwx flags. */
export function modeFromFlags(flags: boolean[]): number {
  let mode = 0;
  for (let index = 0; index < 9; index += 1) {
    if (flags[index]) {
      mode |= 1 << (8 - index);
    }
  }
  return mode;
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

/**
 * entryTypeLabel renders the Type column ("Folder", "YAML", "Image"…). It only
 * needs a name and kind (plus the mode to name a special file's subtype), so the
 * recycle-bin rows — which carry no mode — can reuse it.
 */
export function entryTypeLabel(
  entry: Pick<FilesEntry, "name" | "kind"> & { mode?: number },
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
    const typeBits = (entry.mode ?? 0) & 0o170000;
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

/**
 * widestBytesLabel is the widest `formatBytes` label any byte count in
 * `[0, total]` can produce — a true upper bound on the live selection summary.
 * `formatBytes` is non-monotonic across unit boundaries twice over: it switches
 * unit at powers of 1024, and it drops from 2 to 1 decimal at a value of 100,
 * so `toFixed` rounds a value just below a `10^k` boundary *up* into a wider
 * string (99.999 -> "100.00 KiB", 999.99 -> "1000.0 KiB"). Probing each
 * reachable rounding peak (`10^k * unit - 1`) captures those, so the reserve
 * built from this is never narrower than a subset's live label.
 */
export function widestBytesLabel(total: number): string {
  if (!(total > 0)) {
    return formatBytes(total);
  }
  let widest = formatBytes(total);
  for (let base = 1; base <= total; base *= 1024) {
    for (const boundary of [10, 100, 1000, 1024]) {
      const peak = base * boundary - 1;
      if (peak < base || peak > total) {
        continue;
      }
      const label = formatBytes(peak);
      if (
        label.length > widest.length ||
        // On a length tie prefer the widest unit glyph (MiB) so a
        // proportional-font unit can never render wider than the reserve.
        (label.length === widest.length &&
          unitRank(label) > unitRank(widest))
      ) {
        widest = label;
      }
    }
  }
  return widest;
}

/** unitRank orders `formatBytes` units by glyph width (MiB widest). */
function unitRank(label: string): number {
  const unit = label.split(" ")[1] ?? "";
  // 'M' is the widest unit initial; the single-char byte unit is narrowest.
  if (unit === "MiB") {
    return 3;
  }
  return unit === "B" ? 0 : 1;
}

/** TabAddress is the root+path identity a browser tab's label is derived from. */
export interface TabAddress {
  id: string;
  rootName: string;
  path: string;
}

/** tabSegments splits one tab's location into ordered root-relative segments. */
function tabSegments(tab: TabAddress): string[] {
  return tab.path === "" ? [tab.rootName] : [tab.rootName, ...tab.path.split("/")];
}

/**
 * disambiguatedTabLabels maps every tab to a unique visible label: the folder's
 * basename, or the root. When two tabs share a basename (two folders named
 * "config" under different parents) the parent segments are prepended one by
 * one until every label differs, so "a/x/config" and "b/x/config" no longer
 * both read "x/config". The caller keeps the full path as the tab tooltip.
 */
export function disambiguatedTabLabels(tabs: TabAddress[]): Map<string, string> {
  const labelAt = (segments: string[], depth: number): string =>
    segments.slice(-depth).join("/");
  const depth = new Map<string, number>(tabs.map((tab) => [tab.id, 1]));
  // Grow the prefix of every colliding tab until all labels differ, or the
  // label already spans the whole root-relative path (two identical locations
  // cannot be told apart, so they keep the basename).
  let changed = true;
  while (changed) {
    changed = false;
    const counts = new Map<string, number>();
    for (const tab of tabs) {
      const label = labelAt(tabSegments(tab), depth.get(tab.id) ?? 1);
      counts.set(label, (counts.get(label) ?? 0) + 1);
    }
    for (const tab of tabs) {
      const segments = tabSegments(tab);
      const d = depth.get(tab.id) ?? 1;
      if ((counts.get(labelAt(segments, d)) ?? 0) > 1 && d < segments.length) {
        depth.set(tab.id, d + 1);
        changed = true;
      }
    }
  }
  return new Map(
    tabs.map((tab) => [tab.id, labelAt(tabSegments(tab), depth.get(tab.id) ?? 1)]),
  );
}
