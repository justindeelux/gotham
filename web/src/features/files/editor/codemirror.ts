import {
  defaultKeymap,
  history,
  historyKeymap,
} from "@codemirror/commands";
import {
  HighlightStyle,
  StreamLanguage,
  bracketMatching,
  foldGutter,
  foldKeymap,
  indentOnInput,
  indentUnit,
  syntaxHighlighting,
} from "@codemirror/language";
import {
  highlightSelectionMatches,
  selectNextOccurrence,
  selectSelectionMatches,
} from "@codemirror/search";
import { Compartment, EditorState } from "@codemirror/state";
import type { Extension, Range } from "@codemirror/state";
import {
  Decoration,
  EditorView,
  ViewPlugin,
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
import type { DecorationSet, ViewUpdate } from "@codemirror/view";
import { simpleMode } from "@codemirror/legacy-modes/mode/simple-mode";
import { tags } from "@lezer/highlight";

import { bracketExtensions, completionExtensions } from "@/features/files/editor/completion";
import { editorCommandKeymap } from "@/features/files/editor/shortcuts";
import { createSearchController, customSearchExtensions } from "@/features/files/editor/search";
import type { EditorSearchController } from "@/features/files/editor/search";
import type { EditorLineInfo } from "@/features/files/editor/goto";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import type { EditorLanguageId } from "@/features/files/utils/editor";

export interface EditorAccessibility {
  ariaLabel: string;
  placeholder?: string;
  invalid: boolean;
  describedBy?: string;
}

export interface EditorSession {
  getDoc: () => string;
  setDoc: (_doc: string) => void;
  setLanguage: (_language: EditorLanguageId) => void;
  setSettings: (_settings: EditorSettings) => void;
  setReadOnly: (_readOnly: boolean) => void;
  setAccessibility: (_accessibility: EditorAccessibility) => void;
  focus: () => void;
  requestMeasure: () => void;
  search: EditorSearchController;
  lineInfo: () => EditorLineInfo;
  gotoLine: (_line: number, _column: number) => "line" | "column" | null;
  destroy: () => void;
}

export interface EditorSessionOptions {
  parent: HTMLElement;
  doc: string;
  language: EditorLanguageId;
  ariaLabel: string;
  placeholder?: string;
  invalid?: boolean;
  describedBy?: string;
  readOnly: boolean;
  settings: EditorSettings;
  onChange: (_doc: string) => void;
  onCursor?: (_line: number, _column: number) => void;
  onSave: () => void;
  onSaveAll?: () => void;
  onCloseTab?: () => void;
  onToggleWrap?: () => void;
  onNextTab?: () => void;
  onPreviousTab?: () => void;
  onFind: () => void;
  onReplace: () => void;
  onGoToLine: () => void;
  onFindNext?: () => void;
  onFindPrevious?: () => void;
}

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

function stream(mode: Parameters<typeof StreamLanguage.define>[0]): Extension {
  return StreamLanguage.define(mode);
}

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
  apache: async () => stream((await import("@codemirror/legacy-modes/mode/nginx")).nginx),
  ini: async () =>
    stream((await import("@codemirror/legacy-modes/mode/properties")).properties),
  diff: async () => stream((await import("@codemirror/legacy-modes/mode/diff")).diff),
  log: async () => stream(logMode),
  plain: async () => [],
};

const languageExtensions = new Map<EditorLanguageId, Promise<Extension>>();

export function loadLanguage(language: EditorLanguageId): Promise<Extension> {
  let extension = languageExtensions.get(language);
  if (!extension) {
    extension = languageLoaders[language]().catch(() => [] as Extension);
    languageExtensions.set(language, extension);
  }
  return extension;
}

function readOnlyExtensions(readOnly: boolean): Extension {
  return [EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)];
}

function accessibilityExtensions(accessibility: EditorAccessibility): Extension {
  const attributes: Record<string, string> = {
    "aria-label": accessibility.ariaLabel,
  };
  if (accessibility.invalid) {
    attributes["aria-invalid"] = "true";
  }
  if (accessibility.describedBy) {
    attributes["aria-describedby"] = accessibility.describedBy;
  }
  return [
    placeholderExt(accessibility.placeholder ?? "(empty file)"),
    EditorView.contentAttributes.of(attributes),
  ];
}

const gothamHighlight = HighlightStyle.define([
  { tag: tags.keyword, color: "#c792ea" },
  { tag: [tags.string, tags.special(tags.string)], color: "#c3e88d" },
  { tag: [tags.number, tags.bool, tags.null], color: "#f78c6c" },
  { tag: [tags.comment, tags.lineComment, tags.blockComment], color: "#8f95a3", fontStyle: "italic" },
  { tag: [tags.propertyName, tags.attributeName], color: "#82aaff" },
  { tag: [tags.typeName, tags.className], color: "#ffcb6b" },
  { tag: [tags.function(tags.variableName), tags.labelName], color: "#82aaff" },
  { tag: [tags.definition(tags.variableName), tags.variableName], color: "#dbdee1" },
  { tag: [tags.tagName, tags.heading, tags.strong], color: "#f2f3f5" },
  { tag: [tags.link, tags.url], color: "#89ddff", textDecoration: "underline" },
  { tag: tags.invalid, color: "#f23f43" },
]);

const gothamLightHighlight = HighlightStyle.define([
  { tag: tags.keyword, color: "var(--editor-light-keyword)" },
  { tag: [tags.string, tags.special(tags.string)], color: "var(--editor-light-string)" },
  { tag: [tags.number, tags.bool, tags.null], color: "var(--editor-light-number)" },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    color: "var(--editor-light-comment)",
    fontStyle: "italic",
  },
  { tag: [tags.propertyName, tags.attributeName], color: "var(--editor-light-property)" },
  { tag: [tags.typeName, tags.className], color: "var(--editor-light-type)" },
  { tag: [tags.function(tags.variableName), tags.labelName], color: "var(--editor-light-property)" },
  { tag: [tags.definition(tags.variableName), tags.variableName], color: "var(--editor-light-variable)" },
  { tag: [tags.tagName, tags.heading, tags.strong], color: "var(--editor-light-tag)" },
  { tag: [tags.link, tags.url], color: "var(--editor-light-link)", textDecoration: "underline" },
  { tag: tags.invalid, color: "var(--editor-light-invalid)" },
]);

function whitespaceDecorations(
  view: EditorView,
  tabSize: number,
  showInvisibles: boolean,
  indentGuides: boolean,
): DecorationSet {
  const ranges: Range<Decoration>[] = [];
  const spaceMark = Decoration.mark({ class: "cm-invisible-space" });
  const tabMark = Decoration.mark({ class: "cm-invisible-tab" });
  const guideMark = Decoration.mark({ class: "cm-indent-guide" });
  for (const { from, to } of view.visibleRanges) {
    let pos = from;
    while (pos <= to) {
      const line = view.state.doc.lineAt(pos);
      const lead = /^[ \t]+/.exec(line.text)?.[0] ?? "";
      let column = 0;
      for (let index = 0; index < lead.length; index += 1) {
        const char = lead[index];
        const at = line.from + index;
        if (showInvisibles) {
          ranges.push((char === "\t" ? tabMark : spaceMark).range(at, at + 1));
        }
        if (indentGuides && column > 0 && column % tabSize === 0) {
          ranges.push(guideMark.range(at, at + 1));
        }
        column += char === "\t" ? tabSize - (column % tabSize) : 1;
      }
      if (line.to >= to) {
        break;
      }
      pos = line.to + 1;
    }
  }
  return Decoration.set(ranges, true);
}

function whitespacePlugin(settings: EditorSettings): Extension {
  if (!settings.showInvisibles && !settings.indentGuides) {
    return [];
  }
  const { tabSize, showInvisibles, indentGuides } = settings;
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet;

      constructor(view: EditorView) {
        this.decorations = whitespaceDecorations(view, tabSize, showInvisibles, indentGuides);
      }

      update(update: ViewUpdate): void {
        if (update.docChanged || update.viewportChanged) {
          this.decorations = whitespaceDecorations(
            update.view,
            tabSize,
            showInvisibles,
            indentGuides,
          );
        }
      }
    },
    { decorations: (value) => value.decorations },
  );
}

function editorTheme(settings: EditorSettings): Extension {
  const dark = settings.theme === "dark";
  const background = dark ? "var(--surface-warm)" : "var(--editor-light-bg)";
  const foreground = dark ? "var(--fg)" : "var(--editor-light-fg)";
  return [
    EditorView.theme(
      {
        "&": {
          height: "100%",
          backgroundColor: background,
          color: foreground,
          fontSize: `${settings.fontSize}px`,
        },
        ".cm-scroller": {
          fontFamily:
            settings.fontFamily === "sans" ? "var(--font-body)" : "var(--font-mono)",
          lineHeight: "1.6",
          overflow: "auto",
        },
        ".cm-content": { caretColor: "var(--accent)" },
        ".cm-placeholder": { whiteSpace: "pre-wrap" },
        ".cm-gutters": {
          backgroundColor: background,
          color: dark ? "var(--muted)" : "var(--meta)",
          border: "none",
          borderRight: "1px solid var(--border)",
        },
        ".cm-activeLine": {
          backgroundColor: dark
            ? "color-mix(in oklab, var(--fg) 6%, transparent)"
            : "color-mix(in oklab, var(--editor-light-fg) 6%, transparent)",
        },
        ".cm-activeLineGutter": { backgroundColor: "transparent", color: foreground },
        ".cm-invisible-space": {
          backgroundImage:
            "radial-gradient(circle, var(--meta) 1px, transparent 1px)",
          backgroundSize: "0.5em 1em",
        },
        ".cm-invisible-tab": {
          backgroundImage:
            "linear-gradient(90deg, var(--meta) 1px, transparent 1px)",
          backgroundSize: "0.5em 100%",
          opacity: "0.7",
        },
        ".cm-indent-guide": { boxShadow: "inset 1px 0 0 var(--border-soft)" },
        "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": {
          backgroundColor: "color-mix(in oklab, var(--accent) 40%, transparent)",
        },
        ".cm-cursor, .cm-dropCursor": { borderLeftColor: foreground },
        ".cm-searchMatch": { backgroundColor: "color-mix(in oklab, var(--warn) 45%, transparent)" },
        ".cm-searchMatch.cm-searchMatch-selected": {
          backgroundColor: "color-mix(in oklab, var(--warn) 70%, transparent)",
        },
        ".cm-panels": {
          backgroundColor: dark ? "var(--surface)" : "var(--editor-light-bg)",
          color: dark ? "var(--fg)" : "var(--editor-light-fg)",
        },
        ".cm-tooltip": {
          backgroundColor: dark ? "var(--surface)" : "var(--editor-light-bg)",
          border: "1px solid var(--border)",
          color: dark ? "var(--fg)" : "var(--editor-light-fg)",
        },
      },
      { dark },
    ),
    syntaxHighlighting(dark ? gothamHighlight : gothamLightHighlight),
  ];
}

export function editorSettingsExtensions(
  settings: EditorSettings,
  language: EditorLanguageId = "plain",
): Extension {
  return [
    editorTheme(settings),
    EditorState.tabSize.of(settings.tabSize),
    indentUnit.of(settings.insertSpaces ? " ".repeat(settings.tabSize) : "\t"),
    settings.lineNumbers ? lineNumbers() : [],
    settings.foldGutter ? [foldGutter(), keymap.of(foldKeymap)] : [],
    settings.highlightActiveLine
      ? [highlightActiveLine(), highlightActiveLineGutter()]
      : [],
    whitespacePlugin(settings),
    settings.wrap ? EditorView.lineWrapping : [],
    completionExtensions(settings, language),
    bracketExtensions(),
  ];
}

export async function createEditorSession(
  options: EditorSessionOptions,
): Promise<EditorSession> {
  const languageCompartment = new Compartment();
  const settingsCompartment = new Compartment();
  const readOnlyCompartment = new Compartment();
  const accessibilityCompartment = new Compartment();
  let currentLanguage = options.language;
  let currentSettings = options.settings;
  let currentAccessibility: EditorAccessibility = {
    ariaLabel: options.ariaLabel,
    placeholder: options.placeholder,
    invalid: options.invalid ?? false,
    describedBy: options.describedBy,
  };
  const initialLanguage = await loadLanguage(options.language);

  const noop = (): void => {};
  const commandKeymap = keymap.of([
    ...editorCommandKeymap({
      save: options.onSave,
      saveAll: options.onSaveAll ?? noop,
      closeTab: options.onCloseTab ?? noop,
      nextTab: options.onNextTab ?? noop,
      previousTab: options.onPreviousTab ?? noop,
      find: options.onFind ?? noop,
      replace: options.onReplace ?? noop,
      goToLine: options.onGoToLine ?? noop,
      toggleWrap: options.onToggleWrap ?? noop,
    }),
    ...defaultKeymap,
    ...historyKeymap,
  ]);

  let view: EditorView;
  view = new EditorView({
    parent: options.parent,
    state: EditorState.create({
      doc: options.doc,
      extensions: [
        highlightSpecialChars(),
        history(),
        drawSelection(),
        dropCursor(),
        EditorState.allowMultipleSelections.of(true),
        indentOnInput(),
        bracketMatching(),
        rectangularSelection(),
        highlightSelectionMatches(),
        customSearchExtensions(),
        settingsCompartment.of(
          editorSettingsExtensions(options.settings, options.language),
        ),
        languageCompartment.of(initialLanguage),
        readOnlyCompartment.of(readOnlyExtensions(options.readOnly)),
        accessibilityCompartment.of(accessibilityExtensions(currentAccessibility)),
        commandKeymap,
        keymap.of([
          { key: "Mod-d", run: selectNextOccurrence, preventDefault: true },
          {
            key: "Mod-g",
            run: () => {
              options.onFindNext?.();
              return true;
            },
            preventDefault: true,
          },
          {
            key: "Mod-Shift-g",
            run: () => {
              options.onFindPrevious?.();
              return true;
            },
            preventDefault: true,
          },
          {
            key: "Mod-Alt-g",
            run: () => {
              options.onGoToLine();
              return true;
            },
            preventDefault: true,
          },
          { key: "Mod-Shift-l", run: selectSelectionMatches, preventDefault: true },
        ]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            options.onChange(update.state.doc.toString());
          }
          if (update.docChanged || update.selectionSet) {
            const head = update.state.selection.main.head;
            const line = update.state.doc.lineAt(head);
            options.onCursor?.(line.number, head - line.from + 1);
          }
        }),
      ],
    }),
  });

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
      currentLanguage = language;
      const token = ++languageToken;
      view.dispatch({
        effects: settingsCompartment.reconfigure(
          editorSettingsExtensions(currentSettings, language),
        ),
      });
      void loadLanguage(language).then((extension) => {
        if (token === languageToken) {
          view.dispatch({
            effects: languageCompartment.reconfigure(extension),
          });
        }
      });
    },
    setSettings(settings: EditorSettings): void {
      currentSettings = settings;
      view.dispatch({
        effects: settingsCompartment.reconfigure(
          editorSettingsExtensions(settings, currentLanguage),
        ),
      });
    },
    setReadOnly(readOnly: boolean): void {
      view.dispatch({
        effects: readOnlyCompartment.reconfigure(readOnlyExtensions(readOnly)),
      });
    },
    setAccessibility(accessibility: EditorAccessibility): void {
      currentAccessibility = accessibility;
      view.dispatch({
        effects: accessibilityCompartment.reconfigure(
          accessibilityExtensions(accessibility),
        ),
      });
    },
    focus(): void {
      view.focus();
    },
    requestMeasure(): void {
      view.requestMeasure();
    },
    search: createSearchController([view]),
    lineInfo(): EditorLineInfo {
      const head = view.state.selection.main.head;
      return {
        line: view.state.doc.lineAt(head).number,
        total: view.state.doc.lines,
      };
    },
    gotoLine(line: number, column: number): "line" | "column" | null {
      const total = view.state.doc.lines;
      if (!Number.isSafeInteger(line) || line < 1 || line > total) {
        return "line";
      }
      const target = view.state.doc.line(line);
      if (!Number.isSafeInteger(column) || column < 1 || column > target.length + 1) {
        return "column";
      }
      view.dispatch({
        selection: { anchor: target.from + column - 1 },
        scrollIntoView: true,
      });
      return null;
    },
    destroy(): void {
      view.destroy();
    },
  };
}
