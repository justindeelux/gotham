/**
 * CodeMirror 6 bundle (P18 step 8). This module is imported *only* through a
 * dynamic `import()` in {@link ../composables/useCodeMirror}, so every
 * CodeMirror package lands in its own chunk and never in the entry bundle.
 *
 * Grammars are loaded through per-language dynamic imports: the editor chunk
 * carries only the shell + the loader table, and the first time a file (or the
 * status-bar picker) asks for a language its grammar chunk is fetched.
 * `@codemirror/language-data` is deliberately not a dependency, so the editor
 * never bundles a grammar catalogue it did not name.
 */
import {
  defaultKeymap,
  history,
  historyKeymap,
} from "@codemirror/commands";
import {
  HighlightStyle,
  StreamLanguage,
  bracketMatching,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import { gotoLine, highlightSelectionMatches, openSearchPanel, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState } from "@codemirror/state";
import type { Extension } from "@codemirror/state";
import {
  EditorView,
  drawSelection,
  dropCursor,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  placeholder as placeholderExt,
  rectangularSelection,
} from "@codemirror/view";
import { simpleMode } from "@codemirror/legacy-modes/mode/simple-mode";
import { tags } from "@lezer/highlight";

import type { EditorLanguageId } from "@/features/files/utils/editor";

/** EditorSession is the imperative handle a Vue component keeps per tab. */
export interface EditorSession {
  getDoc: () => string;
  setDoc: (_doc: string) => void;
  setLanguage: (_language: EditorLanguageId) => void;
  setWrap: (_wrap: boolean) => void;
  setReadOnly: (_readOnly: boolean) => void;
  focus: () => void;
  requestMeasure: () => void;
  openSearch: () => void;
  goToLine: () => void;
  destroy: () => void;
}

/** EditorSessionOptions configures one CodeMirror instance. */
export interface EditorSessionOptions {
  parent: HTMLElement;
  doc: string;
  language: EditorLanguageId;
  readOnly: boolean;
  wrap: boolean;
  onChange: (_doc: string) => void;
  onCursor: (_line: number, _column: number) => void;
  onSave: () => void;
  onSaveAll: () => void;
}

/** logMode highlights the level, timestamp and quoted fields of a log line. */
const logMode = simpleMode({
  start: [
    { regex: /\b(?:TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL|CRITICAL)\b/, token: "keyword" },
    {
      regex: /\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\b/,
      token: "atom",
    },
    { regex: /\b\d+(?:\.\d+){1,3}\b/, token: "number" },
    { regex: /"(?:[^"\\]|\\.)*"/, token: "string" },
  ],
});

/** makefileMode highlights assignments and targets in a Makefile. */
const makefileMode = simpleMode({
  start: [
    { regex: /#.*/, token: "comment" },
    { regex: /^\s*[A-Za-z_][\w.-]*\s*[:+?]?=/, token: "def" },
    { regex: /^[A-Za-z0-9_./%$()-]+(?=\s*:(?!=))/, token: "variableName" },
    { regex: /^\t/, token: "meta" },
    { regex: /\$[({][^)}]+[)}]/, token: "variableName" },
    { regex: /"(?:[^"\\]|\\.)*"/, token: "string" },
  ],
});

/** stream wraps one legacy mode as a StreamLanguage extension. */
function stream(mode: Parameters<typeof StreamLanguage.define>[0]): Extension {
  return StreamLanguage.define(mode);
}

/**
 * languageLoaders fetches one grammar on first use. Each dynamic import puts
 * that grammar (and its parser) in its own chunk; Vite shares the common
 * CodeMirror runtime between them.
 */
const languageLoaders: Record<EditorLanguageId, () => Promise<Extension>> = {
  json: async () => (await import("@codemirror/lang-json")).json(),
  yaml: async () => (await import("@codemirror/lang-yaml")).yaml(),
  javascript: async () => (await import("@codemirror/lang-javascript")).javascript(),
  typescript: async () =>
    (await import("@codemirror/lang-javascript")).javascript({ typescript: true }),
  python: async () => (await import("@codemirror/lang-python")).python(),
  go: async () => (await import("@codemirror/lang-go")).go(),
  markdown: async () => (await import("@codemirror/lang-markdown")).markdown(),
  css: async () => (await import("@codemirror/lang-css")).css(),
  scss: async () => stream((await import("@codemirror/legacy-modes/mode/sass")).sass),
  // shortcut: the legacy sass mode also covers the CSS-superset parts of LESS;
  // swap in a dedicated LESS parser if strict LESS nesting matters.
  less: async () => stream((await import("@codemirror/legacy-modes/mode/sass")).sass),
  html: async () => (await import("@codemirror/lang-html")).html(),
  vue: async () => (await import("@codemirror/lang-vue")).vue(),
  shell: async () => stream((await import("@codemirror/legacy-modes/mode/shell")).shell),
  dockerfile: async () =>
    stream((await import("@codemirror/legacy-modes/mode/dockerfile")).dockerFile),
  makefile: async () => stream(makefileMode),
  toml: async () => stream((await import("@codemirror/legacy-modes/mode/toml")).toml),
  php: async () => (await import("@codemirror/lang-php")).php(),
  sql: async () => stream((await import("@codemirror/legacy-modes/mode/sql")).standardSQL),
  xml: async () => stream((await import("@codemirror/legacy-modes/mode/xml")).xml),
  rust: async () => stream((await import("@codemirror/legacy-modes/mode/rust")).rust),
  java: async () => stream((await import("@codemirror/legacy-modes/mode/clike")).java),
  cpp: async () => stream((await import("@codemirror/legacy-modes/mode/clike")).cpp),
  lua: async () => stream((await import("@codemirror/legacy-modes/mode/lua")).lua),
  ruby: async () => stream((await import("@codemirror/legacy-modes/mode/ruby")).ruby),
  nginx: async () => stream((await import("@codemirror/legacy-modes/mode/nginx")).nginx),
  // Apache directives are close enough to nginx's `directive value;` shape.
  apache: async () => stream((await import("@codemirror/legacy-modes/mode/nginx")).nginx),
  ini: async () =>
    stream((await import("@codemirror/legacy-modes/mode/properties")).properties),
  diff: async () => stream((await import("@codemirror/legacy-modes/mode/diff")).diff),
  log: async () => stream(logMode),
  plain: async () => [],
};

/** languageExtensions caches the loaded extension per language (one load each). */
const languageExtensions = new Map<EditorLanguageId, Promise<Extension>>();

/** loadLanguage resolves one language, falling back to no grammar on failure. */
function loadLanguage(language: EditorLanguageId): Promise<Extension> {
  let extension = languageExtensions.get(language);
  if (!extension) {
    extension = languageLoaders[language]().catch(() => [] as Extension);
    languageExtensions.set(language, extension);
  }
  return extension;
}

/** readOnlyExtensions bundles the two facets that gate typing, so a live
 *  session can be flipped read-only when the caller's role/root changes. */
function readOnlyExtensions(readOnly: boolean): Extension {
  return [EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)];
}

/** gothamHighlight maps syntax tags onto the app's dark palette. */
const gothamHighlight = HighlightStyle.define([
  { tag: tags.keyword, color: "#c792ea" },
  { tag: [tags.string, tags.special(tags.string)], color: "#c3e88d" },
  { tag: [tags.number, tags.bool, tags.null], color: "#f78c6c" },
  { tag: [tags.comment, tags.lineComment, tags.blockComment], color: "#7a7f8a", fontStyle: "italic" },
  { tag: [tags.propertyName, tags.attributeName], color: "#82aaff" },
  { tag: [tags.typeName, tags.className], color: "#ffcb6b" },
  { tag: [tags.function(tags.variableName), tags.labelName], color: "#82aaff" },
  { tag: [tags.definition(tags.variableName), tags.variableName], color: "#dbdee1" },
  { tag: [tags.tagName, tags.heading, tags.strong], color: "#f2f3f5" },
  { tag: [tags.link, tags.url], color: "#89ddff", textDecoration: "underline" },
  { tag: tags.invalid, color: "#f23f43" },
]);

/** gothamTheme keeps the editor on the app's dark surface tokens. */
const gothamTheme = EditorView.theme(
  {
    "&": {
      height: "100%",
      backgroundColor: "var(--surface-warm)",
      color: "var(--fg)",
      fontSize: "var(--text-sm)",
    },
    ".cm-scroller": {
      fontFamily: "var(--font-mono)",
      lineHeight: "1.6",
      overflow: "auto",
    },
    ".cm-content": { caretColor: "var(--accent)" },
    ".cm-gutters": {
      backgroundColor: "var(--surface-warm)",
      color: "var(--meta)",
      border: "none",
      borderRight: "1px solid var(--border)",
    },
    ".cm-activeLine": { backgroundColor: "color-mix(in oklab, var(--fg) 6%, transparent)" },
    ".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--fg-2)" },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": {
      backgroundColor: "color-mix(in oklab, var(--accent) 40%, transparent)",
    },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--fg-2)" },
    ".cm-searchMatch": { backgroundColor: "color-mix(in oklab, var(--warn) 45%, transparent)" },
    ".cm-searchMatch.cm-searchMatch-selected": {
      backgroundColor: "color-mix(in oklab, var(--warn) 70%, transparent)",
    },
    ".cm-panels": { backgroundColor: "var(--surface)", color: "var(--fg)" },
    ".cm-tooltip": {
      backgroundColor: "var(--surface)",
      border: "1px solid var(--border)",
      color: "var(--fg)",
    },
  },
  { dark: true },
);

/**
 * createEditorSession builds one editor. The wrap, language and read-only
 * facets live in Compartments so the toolbar (or a role change) can
 * reconfigure them without recreating the view, which would lose the undo
 * history. It awaits the first grammar so the first paint is already
 * highlighted; later switches load in the background.
 */
export async function createEditorSession(
  options: EditorSessionOptions,
): Promise<EditorSession> {
  const languageCompartment = new Compartment();
  const wrapCompartment = new Compartment();
  const readOnlyCompartment = new Compartment();
  const initialLanguage = await loadLanguage(options.language);

  const saveKeymap = keymap.of([
    {
      key: "Mod-Shift-s",
      preventDefault: true,
      run: () => {
        options.onSaveAll();
        return true;
      },
    },
    {
      key: "Mod-s",
      preventDefault: true,
      run: () => {
        options.onSave();
        return true;
      },
    },
  ]);

  const view = new EditorView({
    parent: options.parent,
    state: EditorState.create({
      doc: options.doc,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightSpecialChars(),
        history(),
        drawSelection(),
        dropCursor(),
        EditorState.allowMultipleSelections.of(true),
        indentOnInput(),
        bracketMatching(),
        rectangularSelection(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        syntaxHighlighting(gothamHighlight),
        gothamTheme,
        wrapCompartment.of(options.wrap ? EditorView.lineWrapping : []),
        languageCompartment.of(initialLanguage),
        placeholderExt("(empty file)"),
        readOnlyCompartment.of(readOnlyExtensions(options.readOnly)),
        // indentWithTab is deliberately omitted: it binds Tab to indentation,
        // which traps keyboard focus in the editor (WCAG 2.1.2). Tab now moves
        // focus out of the editor as usual.
        keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap]),
        saveKeymap,
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            options.onChange(update.state.doc.toString());
          }
          if (update.docChanged || update.selectionSet) {
            const head = update.state.selection.main.head;
            const line = update.state.doc.lineAt(head);
            options.onCursor(line.number, head - line.from + 1);
          }
        }),
      ],
    }),
  });

  // languageToken makes the last requested grammar win if several loads race.
  let languageToken = 0;

  return {
    getDoc(): string {
      return view.state.doc.toString();
    },
    setDoc(doc: string): void {
      if (doc === view.state.doc.toString()) {
        return;
      }
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: doc },
      });
    },
    setLanguage(language: EditorLanguageId): void {
      // A rapid second switch wins over the first grammar still loading; the
      // compartment reconfigure keeps the existing state and undo history.
      const token = ++languageToken;
      void loadLanguage(language).then((extension) => {
        if (token === languageToken) {
          view.dispatch({
            effects: languageCompartment.reconfigure(extension),
          });
        }
      });
    },
    setWrap(wrap: boolean): void {
      view.dispatch({
        effects: wrapCompartment.reconfigure(wrap ? EditorView.lineWrapping : []),
      });
    },
    setReadOnly(readOnly: boolean): void {
      view.dispatch({
        effects: readOnlyCompartment.reconfigure(readOnlyExtensions(readOnly)),
      });
    },
    focus(): void {
      view.focus();
    },
    requestMeasure(): void {
      view.requestMeasure();
    },
    openSearch(): void {
      openSearchPanel(view);
    },
    goToLine(): void {
      gotoLine(view);
    },
    destroy(): void {
      view.destroy();
    },
  };
}
