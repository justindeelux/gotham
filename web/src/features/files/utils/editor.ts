import { clampWindowOffset } from "@/shared/window/geometry";

export type EditorEol = "\n" | "\r\n";

export interface DocShape {
  doc: string;
  eol: EditorEol;
  bom: boolean;
  faithful: boolean;
}

const BOM = "\uFEFF";

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

export function serializeDoc(
  shape: Pick<DocShape, "doc" | "eol" | "bom"> & { encoding?: string },
): string {
  const body = shape.doc.replace(/\n/g, shape.eol);
  const bom = shape.bom && (shape.encoding ?? "utf-8") === "utf-8";
  return bom ? BOM + body : body;
}

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

export interface EditorLanguage {
  id: EditorLanguageId;
  name: string;
  label: string;
}

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

const filenameRules: ReadonlyArray<{
  test: (_name: string) => boolean;
  language: EditorLanguageId;
}> = [
  {
    test: (name) => name.startsWith("dockerfile.") || name.endsWith(".dockerfile"),
    language: "dockerfile",
  },
  {
    test: (name) => name.startsWith("makefile.") || name.endsWith(".makefile"),
    language: "makefile",
  },
  {
    test: (name) => name.startsWith(".env.") || name.endsWith(".env"),
    language: "ini",
  },
  {
    test: (name) => name.endsWith(".nginx.conf"),
    language: "nginx",
  },
];

function extensionOf(name: string): string {
  const base = name.slice(name.lastIndexOf("/") + 1);
  const dot = base.lastIndexOf(".");
  return dot > 0 ? base.slice(dot + 1).toLowerCase() : "";
}

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

export function tabTitle(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? path : path.slice(slash + 1);
}

export function pathWithin(path: string, dir: string): boolean {
  if (dir === "") {
    return true;
  }
  return path === dir || path.startsWith(`${dir}/`);
}

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

export function clampEditorOffset(
  x: number,
  y: number,
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
): { x: number; y: number } {
  return clampWindowOffset(
    x,
    y,
    width,
    height,
    viewportWidth,
    viewportHeight,
  );
}

export type TabCloseScope = "self" | "others" | "all" | "right";

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

export function languageLabel(language: EditorLanguageId): string {
  return byLanguage.get(language)?.label ?? language.toUpperCase();
}

export function languageName(language: EditorLanguageId): string {
  return byLanguage.get(language)?.name ?? language;
}

export type EditorEncoding = "ascii" | "utf-8" | "gbk" | "gb2312" | "big5";

export const editorEncodings: readonly EditorEncoding[] = [
  "ascii",
  "utf-8",
  "gbk",
  "gb2312",
  "big5",
];

const editorEncodingNames: Record<EditorEncoding, string> = {
  ascii: "ASCII",
  "utf-8": "UTF-8",
  gbk: "GBK",
  gb2312: "GB2312",
  big5: "BIG5",
};

export function isEditorEncoding(value: unknown): value is EditorEncoding {
  return (
    typeof value === "string" &&
    (editorEncodings as readonly string[]).includes(value)
  );
}

export function normalizeEditorEncoding(value: unknown): EditorEncoding {
  const normalized = typeof value === "string" ? value.trim().toLowerCase() : value;
  return isEditorEncoding(normalized) ? normalized : "utf-8";
}

export function encodingLabel(encoding: EditorEncoding): string {
  return editorEncodingNames[encoding];
}

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
