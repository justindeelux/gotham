import { activeLocale } from "@/shared/i18n/locale";
import { formatBytes } from "@/shared/utils/format";
import en from "@/features/files/locales/en";
import vi from "@/features/files/locales/vi";

import type { ArchiveFormat } from "@/features/files/api/files";
import type { FileKind, FilesEntry } from "@/features/files/schemas/files";

export type FilesViewMode = "list" | "grid";

export type FilesSortKey = "name" | "size" | "modified" | "kind" | "mode";

export type FilesSortDirection = "asc" | "desc";

export type FilesIconKind =
  | "folder"
  | "file"
  | "doc"
  | "image"
  | "archive"
  | "terminal"
  | "link"
  | "lock";

function catalogFor(locale?: string | null): typeof en {
  return (locale ?? activeLocale.value) === "vi" ? vi : en;
}

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

export function entryExtension(name: string): string {
  const dot = name.lastIndexOf(".");
  if (dot <= 0 || dot === name.length - 1) {
    return "";
  }
  return name.slice(dot + 1).toLowerCase();
}

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

export function isExtractableArchive(entry: FilesEntry): boolean {
  return entry.kind === "file" && archiveFormatFromName(entry.name) !== null;
}

export function isImageFile(entry: FilesEntry): boolean {
  return entry.kind === "file" && imageExtensions.has(entryExtension(entry.name));
}

export type EntryAction = "directory" | "editor" | "extract" | "none";

export interface EntryActionGates {
  canOpenEditor: boolean;
  canExtract: boolean;
}

export const MAX_EDITABLE_BYTES = 2 << 20;

const binaryExtensions = new Set([
  ...imageExtensions,
  ...videoExtensions,
  ...audioExtensions,
  ...archiveExtensions,
  "pdf", "woff", "woff2", "ttf", "otf", "eot",
]);

export function canOpenInEditor(entry: FilesEntry): boolean {
  return entry.kind === "file" && !binaryExtensions.has(entryExtension(entry.name));
}

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

export function isSelectable(entry: FilesEntry): boolean {
  return entry.kind === "file" || entry.kind === "directory";
}

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

export const MAX_NAME_BYTES = 255;

export type NameKind = "file" | "directory";

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

export function normalizeEntryName(name: string): string {
  return name.trim();
}

export type TransferProblem = "same-folder" | "into-itself";

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

export function isFileDrag(types?: ArrayLike<string> | null): boolean {
  return types != null && Array.from(types).includes("Files");
}

export interface DropItemLike {
  kind: string;
  webkitGetAsEntry?: () => { isDirectory?: boolean } | null;
  getAsFile?: () => File | null;
}

export interface DropDataLike {
  items?: ArrayLike<DropItemLike>;
  files?: ArrayLike<File> | null;
}

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

export function joinPath(base: string, name: string): string {
  return base === "" ? name : `${base}/${name}`;
}

export interface TerminalDirectoryContext {
  entries?: FilesEntry[];
  rootPath?: string;
}

export function terminalDirectoryFor(
  entry: FilesEntry | null,
  currentPath: string,
  context: TerminalDirectoryContext = {},
): string | null {
  if (entry === null) {
    return currentPath;
  }
  if (entry.kind === "directory") {
    return joinPath(currentPath, entry.name);
  }
  if (
    entry.kind === "symlink" &&
    context.entries !== undefined &&
    symlinkTargetsDirectory(
      entry,
      currentPath,
      context.entries,
      context.rootPath ?? "",
    )
  ) {
    return joinPath(currentPath, entry.name);
  }
  return null;
}

export function parentPath(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}

export interface BreadcrumbSegment {
  name: string;
  path: string;
  isRoot: boolean;
}

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

export function entryMatchesQuery(entry: FilesEntry, query: string): boolean {
  const needle = query.trim().toLowerCase();
  return needle === "" || entry.name.toLowerCase().includes(needle);
}

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
  const folder = joinPath(dir, name).split("/");
  folder.pop();
  return resolvePathSegments(folder, target);
}

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

export function symlinkTargetsDirectory(
  entry: FilesEntry,
  currentDir: string,
  entries: FilesEntry[],
  rootPath = "",
): boolean {
  if (entry.kind !== "symlink" || !entry.symlink_target) {
    return false;
  }
  const target = resolveSymlinkTarget(
    currentDir,
    entry.name,
    entry.symlink_target,
    rootPath,
  );
  if (target === null) {
    return false;
  }
  if (target === "" || target === currentDir) {
    return true;
  }
  const prefix = currentDir === "" ? "" : `${currentDir}/`;
  if (!target.startsWith(prefix)) {
    return false;
  }
  const relative = target.slice(prefix.length);
  if (relative === "" || relative.includes("/")) {
    return false;
  }
  return entries.some(
    (item) => item.name === relative && item.kind === "directory",
  );
}

function timestampOf(entry: FilesEntry): number {
  return new Date(entry.modified_at).getTime();
}

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

export const PERMISSION_BITS = 0o777;

export function formatOctalMode(mode: number): string {
  return (mode & PERMISSION_BITS).toString(8).padStart(3, "0");
}

export function formatOctalModeFull(mode: number): string {
  const bits = mode & 0o7777;
  return bits.toString(8).padStart(bits > PERMISSION_BITS ? 4 : 3, "0");
}

export function ownerModeLabel(entry: FilesEntry): string {
  const mode = formatOctalModeFull(entry.mode);
  return entry.owner ? `${entry.owner} / ${mode}` : mode;
}

export function parseOctalMode(value: string): number {
  return Number.parseInt(value, 8) & PERMISSION_BITS;
}

export function permissionFlags(mode: number): boolean[] {
  const m = mode & PERMISSION_BITS;
  return [8, 7, 6, 5, 4, 3, 2, 1, 0].map((shift) => ((m >> shift) & 1) === 1);
}

export function modeFromFlags(flags: boolean[]): number {
  let mode = 0;
  for (let index = 0; index < 9; index += 1) {
    if (flags[index]) {
      mode |= 1 << (8 - index);
    }
  }
  return mode;
}

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
        (label.length === widest.length &&
          unitRank(label) > unitRank(widest))
      ) {
        widest = label;
      }
    }
  }
  return widest;
}

function unitRank(label: string): number {
  const unit = label.split(" ")[1] ?? "";
  if (unit === "MiB") {
    return 3;
  }
  return unit === "B" ? 0 : 1;
}

export interface TabAddress {
  id: string;
  rootName: string;
  path: string;
}

function tabSegments(tab: TabAddress): string[] {
  return tab.path === "" ? [tab.rootName] : [tab.rootName, ...tab.path.split("/")];
}

export function disambiguatedTabLabels(tabs: TabAddress[]): Map<string, string> {
  const labelAt = (segments: string[], depth: number): string =>
    segments.slice(-depth).join("/");
  const depth = new Map<string, number>(tabs.map((tab) => [tab.id, 1]));
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
