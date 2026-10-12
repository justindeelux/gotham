/**
 * CodeMirror 6 bundle (P18 step 8). This module is imported *only* through a
 * dynamic `import()` in {@link ../composables/useCodeMirror}, so every
 * CodeMirror package lands in its own chunk and never in the entry bundle.
 *
 * The language set is explicit (json, yaml, js/ts, python, go, markdown, css,
 * html, shell, dockerfile, toml); `@codemirror/language-data` is deliberately
 * not a dependency, so the editor never loads a grammar the ticket did not
 * list.
 */
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { css } from "@codemirror/lang-css";
import { go } from "@codemirror/lang-go";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { yaml } from "@codemirror/lang-yaml";
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
import { dockerFile } from "@codemirror/legacy-modes/mode/dockerfile";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { tags } from "@lezer/highlight";

import type { EditorLanguageId } from "@/features/files/utils/editor";

/** EditorSession is the imperative handle a Vue component keeps per tab. */
export interface EditorSession {
  getDoc: () => string;
  setDoc: (_doc: string) => void;
  setLanguage: (_language: EditorLanguageId) => void;
  setWrap: (_wrap: boolean) => void;
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
}

/** languageExtension resolves the explicit language set to a CM extension. */
function languageExtension(id: EditorLanguageId): Extension {
  switch (id) {
    case "json":
      return json();
    case "yaml":
      return yaml();
    case "javascript":
      return javascript();
    case "typescript":
      return javascript({ typescript: true });
    case "python":
      return python();
    case "go":
      return go();
    case "markdown":
      return markdown();
    case "css":
      return css();
    case "html":
      return html();
    case "shell":
      return StreamLanguage.define(shell);
    case "dockerfile":
      return StreamLanguage.define(dockerFile);
    case "toml":
      return StreamLanguage.define(toml);
    default:
      return [];
  }
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
 * createEditorSession builds one editor. The wrap and language facets live in
 * Compartments so the toolbar can reconfigure them without recreating the view,
 * which would lose the undo history.
 */
export function createEditorSession(options: EditorSessionOptions): EditorSession {
  const languageCompartment = new Compartment();
  const wrapCompartment = new Compartment();

  const saveKeymap = keymap.of([
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
        languageCompartment.of(languageExtension(options.language)),
        placeholderExt("(empty file)"),
        EditorState.readOnly.of(options.readOnly),
        EditorView.editable.of(!options.readOnly),
        keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
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
      view.dispatch({
        effects: languageCompartment.reconfigure(languageExtension(language)),
      });
    },
    setWrap(wrap: boolean): void {
      view.dispatch({
        effects: wrapCompartment.reconfigure(wrap ? EditorView.lineWrapping : []),
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
