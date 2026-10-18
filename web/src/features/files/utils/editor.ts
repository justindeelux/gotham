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
  /**
   * faithful is false when the body mixes CRLF with lone LF, or contains a
   * lone CR: re-serialising such a body with a single `eol` cannot reproduce
   * the original bytes. The store opens those files read-only instead of
   * silently normalising them on the next save.
   */
  faithful: boolean;
}

const BOM = "\uFEFF";

/**
 * splitDoc decodes one text body into editor text plus the line-ending and
 * BOM metadata needed to reproduce the original bytes on save. A body whose
 * terminators are all one kind (LF, or CRLF) round-trips byte-identically;
 * a mixed body (CRLF + lone LF, or any lone CR) is flagged `faithful: false`
 * so it is never silently rewritten.
 */
export function splitDoc(content: string): DocShape {
  const bom = content.startsWith(BOM);
  const body = bom ? content.slice(BOM.length) : content;
  const crlf = (body.match(/\r\n/g) ?? []).length;
  const loneCr = (body.match(/\r(?!\n)/g) ?? []).length;
  const loneLf = (body.match(/(?<!\r)\n/g) ?? []).length;
  const faithful = loneCr === 0 && !(crlf > 0 && loneLf > 0);
  const eol: EditorEol = crlf > 0 ? "\r\n" : "\n";
  const doc = body.replace(/\r\n?|\n/g, "\n");
  return { doc, eol, bom, faithful };
}

/**
 * serializeDoc re-encodes editor text with its original line endings and BOM,
 * so a load followed by an unchanged save writes the same bytes. It takes the
 * round-trippable subset of {@link DocShape} (a mixed-EOL body is never saved).
 * A BOM is only meaningful for UTF-8; a legacy charset drops it, since the
 * server would refuse U+FEFF there.
 */
export function serializeDoc(
  shape: Pick<DocShape, "doc" | "eol" | "bom"> & { encoding?: string },
): string {
  const body = shape.doc.replace(/\n/g, shape.eol);
  const bom = shape.bom && (shape.encoding ?? "utf-8") === "utf-8";
  return bom ? BOM + body : body;
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
  | "scss"
  | "less"
  | "html"
  | "vue"
  | "shell"
  | "dockerfile"
  | "makefile"
  | "toml"
  | "php"
  | "sql"
  | "xml"
  | "rust"
  | "java"
  | "cpp"
  | "lua"
  | "ruby"
  | "nginx"
  | "apache"
  | "ini"
  | "diff"
  | "log"
  | "plain";

/** EditorLanguage is one pickable language with its display names. */
export interface EditorLanguage {
  id: EditorLanguageId;
  /** name is the full name shown in the picker. */
  name: string;
  /** label is the short uppercase tag shown in the status bar. */
  label: string;
}

/** editorLanguages is the status-bar picker order. */
export const editorLanguages: readonly EditorLanguage[] = [
  { id: "plain", name: "Plain text", label: "TEXT" },
  { id: "json", name: "JSON", label: "JSON" },
  { id: "yaml", name: "YAML", label: "YAML" },
  { id: "javascript", name: "JavaScript", label: "JS" },
  { id: "typescript", name: "TypeScript", label: "TS" },
  { id: "html", name: "HTML", label: "HTML" },
  { id: "vue", name: "Vue", label: "VUE" },
  { id: "css", name: "CSS", label: "CSS" },
  { id: "scss", name: "SCSS", label: "SCSS" },
  { id: "less", name: "LESS", label: "LESS" },
  { id: "markdown", name: "Markdown", label: "MD" },
  { id: "python", name: "Python", label: "PYTHON" },
  { id: "go", name: "Go", label: "GO" },
  { id: "php", name: "PHP", label: "PHP" },
  { id: "sql", name: "SQL", label: "SQL" },
  { id: "xml", name: "XML", label: "XML" },
  { id: "rust", name: "Rust", label: "RUST" },
  { id: "java", name: "Java", label: "JAVA" },
  { id: "cpp", name: "C/C++", label: "C/C++" },
  { id: "lua", name: "Lua", label: "LUA" },
  { id: "ruby", name: "Ruby", label: "RUBY" },
  { id: "shell", name: "Shell", label: "SHELL" },
  { id: "dockerfile", name: "Dockerfile", label: "DOCKERFILE" },
  { id: "makefile", name: "Makefile", label: "MAKEFILE" },
  { id: "nginx", name: "Nginx config", label: "NGINX" },
  { id: "apache", name: "Apache config", label: "APACHE" },
  { id: "ini", name: "INI / properties", label: "INI" },
  { id: "toml", name: "TOML", label: "TOML" },
  { id: "diff", name: "Diff", label: "DIFF" },
  { id: "log", name: "Log", label: "LOG" },
];

const byLanguage = new Map(editorLanguages.map((language) => [language.id, language]));

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
  pyi: "python",
  go: "go",
  md: "markdown",
  markdown: "markdown",
  css: "css",
  scss: "scss",
  less: "less",
  html: "html",
  htm: "html",
  xhtml: "html",
  vue: "vue",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  toml: "toml",
  php: "php",
  php3: "php",
  php4: "php",
  php5: "php",
  phps: "php",
  phtml: "php",
  sql: "sql",
  xml: "xml",
  xsl: "xml",
  xslt: "xml",
  rdf: "xml",
  rss: "xml",
  wsdl: "xml",
  atom: "xml",
  mathml: "xml",
  mml: "xml",
  xul: "xml",
  xbl: "xml",
  xaml: "xml",
  svg: "xml",
  rs: "rust",
  java: "java",
  c: "cpp",
  cc: "cpp",
  cpp: "cpp",
  cxx: "cpp",
  h: "cpp",
  hh: "cpp",
  hpp: "cpp",
  hxx: "cpp",
  ino: "cpp",
  lua: "lua",
  rb: "ruby",
  ru: "ruby",
  gemspec: "ruby",
  rake: "ruby",
  nginx: "nginx",
  conf: "apache",
  htaccess: "apache",
  htpasswd: "apache",
  htgroups: "apache",
  ini: "ini",
  cfg: "ini",
  prefs: "ini",
  properties: "ini",
  diff: "diff",
  patch: "diff",
  log: "log",
  mk: "makefile",
  make: "makefile",
  txt: "plain",
};

/**
 * byName matches the whole basename first, so a dotless file (Dockerfile,
 * Makefile, Gemfile) and a dotted special name (nginx.conf, .htaccess) never
 * depend on extension extraction.
 */
const byName: Record<string, EditorLanguageId> = {
  ".htaccess": "apache",
  ".htpasswd": "apache",
  ".htgroups": "apache",
  ".env": "ini",
  ".editorconfig": "ini",
  dockerfile: "dockerfile",
  makefile: "makefile",
  gnumakefile: "makefile",
  "nginx.conf": "nginx",
  "httpd.conf": "apache",
  "apache2.conf": "apache",
  gemfile: "ruby",
  rakefile: "ruby",
  guardfile: "ruby",
  "docker-compose.yml": "yaml",
  "docker-compose.yaml": "yaml",
  "compose.yml": "yaml",
  "compose.yaml": "yaml",
};

/** filenameRules cover a basename family whose extension is not enough. */
const filenameRules: ReadonlyArray<{
  test: (_name: string) => boolean;
  language: EditorLanguageId;
}> = [
  // "Dockerfile", "Dockerfile.prod", "app.Dockerfile".
  {
    test: (name) => name.startsWith("dockerfile.") || name.endsWith(".dockerfile"),
    language: "dockerfile",
  },
  // "Makefile.am", "Makefile.in", "app.Makefile".
  {
    test: (name) => name.startsWith("makefile.") || name.endsWith(".makefile"),
    language: "makefile",
  },
  // ".env", ".env.local", "dev.env".
  {
    test: (name) => name.startsWith(".env.") || name.endsWith(".env"),
    language: "ini",
  },
  {
    test: (name) => name.endsWith(".nginx.conf"),
    language: "nginx",
  },
];

/** extensionOf returns the lowercased extension after the last dot. */
function extensionOf(name: string): string {
  const base = name.slice(name.lastIndexOf("/") + 1);
  const dot = base.lastIndexOf(".");
  // A leading dot (".env") is a hidden file, not an extension.
  return dot > 0 ? base.slice(dot + 1).toLowerCase() : "";
}

/**
 * detectLanguage maps a file path onto the explicit editor language set. The
 * basename is resolved first (exact special names, then filename families),
 * then the extension, so "Dockerfile" and "nginx.conf" do not fall back to
 * plain text.
 */
export function detectLanguage(path: string): EditorLanguageId {
  const lower = path.slice(path.lastIndexOf("/") + 1).toLowerCase();
  const named = byName[lower];
  if (named) {
    return named;
  }
  const family = filenameRules.find((rule) => rule.test(lower));
  if (family) {
    return family.language;
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

/**
 * clampEditorOffset keeps the floating editor window fully inside the viewport
 * after a title-bar drag: the window may never hang off an edge, so its title
 * bar (and the controls in it) can always be grabbed again. A window larger
 * than the viewport (a small screen) pins to the top-left corner instead of
 * producing a negative maximum.
 */
export function clampEditorOffset(
  x: number,
  y: number,
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
): { x: number; y: number } {
  return {
    x: Math.min(Math.max(0, x), Math.max(0, viewportWidth - width)),
    y: Math.min(Math.max(0, y), Math.max(0, viewportHeight - height)),
  };
}

/** TabCloseScope is the tab-strip command a context-menu entry runs. */
export type TabCloseScope = "self" | "others" | "all" | "right";

/**
 * tabsToClose returns the ids a close command targets, in strip order. "right"
 * is every tab after the target; an unknown target closes nothing.
 */
export function tabsToClose<T extends { id: string }>(
  tabs: T[],
  scope: TabCloseScope,
  targetId: string,
): string[] {
  const index = tabs.findIndex((tab) => tab.id === targetId);
  if (index === -1) {
    return [];
  }
  switch (scope) {
    case "self":
      return [tabs[index]!.id];
    case "others":
      return tabs.filter((_, position) => position !== index).map((tab) => tab.id);
    case "all":
      return tabs.map((tab) => tab.id);
    case "right":
      return tabs.slice(index + 1).map((tab) => tab.id);
  }
}

/**
 * scrollTabIntoView returns the scrollLeft that brings `tab` fully inside the
 * horizontal tab strip, keeping `pad` px of breathing room at either edge. It
 * takes viewport rects, so it works with the strip's own scroll offset already
 * applied; a tab that is already visible leaves the offset untouched.
 */
export function scrollTabIntoView(
  strip: { left: number; right: number; scrollLeft: number },
  tab: { left: number; right: number },
  pad = 8,
): number {
  const next =
    tab.left < strip.left + pad
      ? strip.scrollLeft - (strip.left + pad - tab.left)
      : tab.right > strip.right - pad
        ? strip.scrollLeft + (tab.right - strip.right + pad)
        : strip.scrollLeft;
  return Math.max(0, next);
}

/** languageLabel is the short uppercase tag shown in the status bar. */
export function languageLabel(language: EditorLanguageId): string {
  return byLanguage.get(language)?.label ?? language.toUpperCase();
}

/** languageName is the full display name shown in the language picker. */
export function languageName(language: EditorLanguageId): string {
  return byLanguage.get(language)?.name ?? language;
}

/**
 * EditorEncoding is the save charset of one open document. UTF-8 is the only
 * one a browser produces natively; the legacy charsets are transcoded by the
 * control plane (internal/files/encoding.go).
 */
export type EditorEncoding = "ascii" | "utf-8" | "gbk" | "gb2312" | "big5";

/** editorEncodings is the encoding picker order (reference panel parity). */
export const editorEncodings: readonly EditorEncoding[] = [
  "ascii",
  "utf-8",
  "gbk",
  "gb2312",
  "big5",
];

/** editorEncodingNames maps each charset to its display label in the UI. */
const editorEncodingNames: Record<EditorEncoding, string> = {
  ascii: "ASCII",
  "utf-8": "UTF-8",
  gbk: "GBK",
  gb2312: "GB2312",
  big5: "BIG5",
};

/** isEditorEncoding reports a charset name this editor knows how to save. */
export function isEditorEncoding(value: unknown): value is EditorEncoding {
  return (
    typeof value === "string" &&
    (editorEncodings as readonly string[]).includes(value)
  );
}

/**
 * normalizeEditorEncoding maps a server-reported charset onto a known one,
 * falling back to UTF-8 for a server that predates encoding detection.
 */
export function normalizeEditorEncoding(value: unknown): EditorEncoding {
  const normalized = typeof value === "string" ? value.trim().toLowerCase() : value;
  return isEditorEncoding(normalized) ? normalized : "utf-8";
}

/** encodingLabel is the status-bar and picker label of one charset. */
export function encodingLabel(encoding: EditorEncoding): string {
  return editorEncodingNames[encoding];
}

/**
 * unrepresentableChars returns the characters of `text` the target charset
 * cannot hold. The check is deliberately conservative: every legacy encoding
 * here is BMP-only, so a non-BMP code point (an emoji) is always refused, and
 * ASCII refuses every non-ASCII rune. A BMP character outside a CJK charset is
 * not detectable in the browser; the control plane refuses it with a clear
 * error, so the text is still never silently dropped.
 */
export function unrepresentableChars(
  text: string,
  encoding: EditorEncoding,
): string[] {
  if (encoding === "utf-8") {
    return [];
  }
  const found = new Set<string>();
  for (const character of text) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint > 0xffff || (encoding === "ascii" && codePoint > 0x7f)) {
      found.add(character);
    }
  }
  return [...found];
}
