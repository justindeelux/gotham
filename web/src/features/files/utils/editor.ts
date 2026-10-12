/**
 * Pure helpers behind the file code editor (P18 step 8): EOL/BOM detection
 * and re-serialisation for a byte-identical round trip, the language table
 * for the explicit CodeMirror set (no `language-data`), and the path
 * arithmetic that retargets an open tab through a rename or move. Kept
 * DOM-free so the store and the vitest suite can exercise them directly.
 */

/** EditorEol is the dominant line terminator of an opened document. */
export type EditorEol = "\n" | "\r\n";

/** DocShape is the decoded shape of one text body. */
export interface DocShape {
  /** doc is the text with a leading BOM stripped and every EOL normalised to LF. */
  doc: string;
  eol: EditorEol;
  bom: boolean;
}

const BOM = "\uFEFF";

/**
 * splitDoc decodes one text body into editor text plus the line-ending and
 * BOM metadata needed to reproduce the original bytes on save. A document that
 * mixes CRLF and LF is normalised to the first terminator seen (the mockup's
 * files are consistent); a lone CR is treated as LF.
 */
export function splitDoc(content: string): DocShape {
  const bom = content.startsWith(BOM);
  const body = bom ? content.slice(BOM.length) : content;
  const eol: EditorEol = body.includes("\r\n") ? "\r\n" : "\n";
  const doc = body.replace(/\r\n?|\n/g, "\n");
  return { doc, eol, bom };
}

/**
 * serializeDoc re-encodes editor text with its original line endings and BOM,
 * so a load followed by an unchanged save writes the same bytes.
 */
export function serializeDoc(shape: DocShape): string {
  const body = shape.doc.replace(/\n/g, shape.eol);
  return shape.bom ? BOM + body : body;
}

/** EditorLanguageId is the explicit language set the editor ships. */
export type EditorLanguageId =
  | "json"
  | "yaml"
  | "javascript"
  | "typescript"
  | "python"
  | "go"
  | "markdown"
  | "css"
  | "html"
  | "shell"
  | "dockerfile"
  | "toml"
  | "plain";

const byExtension: Record<string, EditorLanguageId> = {
  json: "json",
  jsonc: "json",
  yaml: "yaml",
  yml: "yaml",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  jsx: "javascript",
  ts: "typescript",
  mts: "typescript",
  cts: "typescript",
  tsx: "typescript",
  py: "python",
  go: "go",
  md: "markdown",
  markdown: "markdown",
  css: "css",
  html: "html",
  htm: "html",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  toml: "toml",
};

const byName: Record<string, EditorLanguageId> = {
  dockerfile: "dockerfile",
  "docker-compose.yml": "yaml",
  "docker-compose.yaml": "yaml",
  makefile: "plain",
};

/** extensionOf returns the lowercased extension after the last dot. */
function extensionOf(name: string): string {
  const base = name.slice(name.lastIndexOf("/") + 1);
  const dot = base.lastIndexOf(".");
  // A leading dot (".env") is a hidden file, not an extension.
  return dot > 0 ? base.slice(dot + 1).toLowerCase() : "";
}

/** detectLanguage maps a file name onto the explicit editor language set. */
export function detectLanguage(name: string): EditorLanguageId {
  const lower = name.slice(name.lastIndexOf("/") + 1).toLowerCase();
  if (lower in byName) {
    return byName[lower];
  }
  // Dockerfile variants: "Dockerfile", "Dockerfile.prod", "*.Dockerfile".
  if (lower === "dockerfile" || lower.startsWith("dockerfile.") || lower.endsWith(".dockerfile")) {
    return "dockerfile";
  }
  return byExtension[extensionOf(lower)] ?? "plain";
}

/** tabTitle is the basename shown on an editor tab. */
export function tabTitle(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? path : path.slice(slash + 1);
}

/** pathWithin reports whether path is dir itself or a descendant of dir. */
export function pathWithin(path: string, dir: string): boolean {
  if (dir === "") {
    return true;
  }
  return path === dir || path.startsWith(`${dir}/`);
}

/**
 * rebasedPath re-bases an open document's path when its ancestor directory
 * moves from `from` to `to`. Returns null when the tab is outside the moved
 * subtree, so the caller can leave it untouched. A root-level move ("") keeps
 * the tab's own directory prefix.
 */
export function rebasedPath(
  path: string,
  from: string,
  to: string,
): string | null {
  if (!pathWithin(path, from)) {
    return null;
  }
  const rest = from === "" ? path : path.slice(from.length + 1);
  if (to === "") {
    return rest;
  }
  return rest === "" ? to : `${to}/${rest}`;
}

/** languageLabel is the short uppercase tag shown in the status bar. */
export function languageLabel(language: EditorLanguageId): string {
  switch (language) {
    case "javascript":
      return "JS";
    case "typescript":
      return "TS";
    case "markdown":
      return "MD";
    case "dockerfile":
      return "DOCKERFILE";
    case "plain":
      return "TEXT";
    default:
      return language.toUpperCase();
  }
}
