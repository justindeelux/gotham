import { MergeView } from "@codemirror/merge";
import { Compartment, EditorState } from "@codemirror/state";
import type { Extension } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";

import { editorSettingsExtensions, loadLanguage } from "@/features/files/editor/codemirror";
import { createSearchController, customSearchExtensions } from "@/features/files/editor/search";
import type { EditorSearchController } from "@/features/files/editor/search";
import type { EditorLineInfo } from "@/features/files/editor/goto";
import { editorCommandKeymap } from "@/features/files/editor/shortcuts";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import type { EditorLanguageId } from "@/features/files/utils/editor";

export interface MergeSession {
  setSettings: (_settings: EditorSettings) => void;
  destroy: () => void;
  requestMeasure: () => void;
  search: EditorSearchController;
  lineInfo: () => EditorLineInfo;
  gotoLine: (_line: number, _column: number) => "line" | "column" | null;
  focus: () => void;
}

export interface MergeSessionOptions {
  parent: HTMLElement;
  base: string;
  current: string;
  language: EditorLanguageId;
  settings: EditorSettings;
  onFind: () => void;
  onReplace: () => void;
  onGoToLine: () => void;
}

const mergeTheme = EditorView.theme({
  "&": { height: "100%" },
  ".cm-mergeViewEditors": { height: "100%" },
  ".cm-editor": { height: "100%" },
  ".cm-changedLine": { backgroundColor: "color-mix(in oklab, var(--accent) 18%, transparent)" },
  ".cm-changedText": { backgroundColor: "color-mix(in oklab, var(--success) 40%, transparent)" },
  ".cm-deletedChunk": { backgroundColor: "color-mix(in oklab, var(--danger) 16%, transparent)" },
  ".cm-insertedLine": { backgroundColor: "color-mix(in oklab, var(--success) 16%, transparent)" },
  ".cm-changedLineGutter": {
    backgroundColor: "color-mix(in oklab, var(--accent) 28%, transparent)",
  },
});

function readOnlyExtensions(): Extension[] {
  return [EditorState.readOnly.of(true), EditorView.editable.of(false)];
}

function diffSettings(settings: EditorSettings): EditorSettings {
  return settings.autocomplete ? { ...settings, autocomplete: false } : settings;
}

export async function createMergeSession(
  options: MergeSessionOptions,
): Promise<MergeSession> {
  const language = await loadLanguage(options.language);
  const settingsA = new Compartment();
  const settingsB = new Compartment();
  const noop = (): void => {};
  const commandKeymap = keymap.of(
    editorCommandKeymap({
      save: noop,
      saveAll: noop,
      closeTab: noop,
      nextTab: noop,
      previousTab: noop,
      find: options.onFind,
      replace: options.onReplace,
      goToLine: options.onGoToLine,
      toggleWrap: noop,
    }),
  );
  const side = (doc: string, settings: Compartment) => ({
    doc,
    extensions: [
      settings.of(
        editorSettingsExtensions(diffSettings(options.settings), options.language),
      ),
      language,
      mergeTheme,
      customSearchExtensions(),
      commandKeymap,
      ...readOnlyExtensions(),
    ],
  });
  const view = new MergeView({
    parent: options.parent,
    a: side(options.base, settingsA),
    b: side(options.current, settingsB),
    highlightChanges: true,
    gutter: true,
    collapseUnchanged: { margin: 3, minSize: 4 },
  });
  return {
    setSettings(settings: EditorSettings): void {
      const extension = editorSettingsExtensions(
        diffSettings(settings),
        options.language,
      );
      view.a.dispatch({ effects: settingsA.reconfigure(extension) });
      view.b.dispatch({ effects: settingsB.reconfigure(extension) });
    },
    search: createSearchController([view.b, view.a]),
    lineInfo(): EditorLineInfo {
      const head = view.b.state.selection.main.head;
      return {
        line: view.b.state.doc.lineAt(head).number,
        total: view.b.state.doc.lines,
      };
    },
    gotoLine(line: number, column: number): "line" | "column" | null {
      const total = view.b.state.doc.lines;
      if (!Number.isSafeInteger(line) || line < 1 || line > total) {
        return "line";
      }
      const target = view.b.state.doc.line(line);
      if (!Number.isSafeInteger(column) || column < 1 || column > target.length + 1) {
        return "column";
      }
      view.b.dispatch({
        selection: { anchor: target.from + column - 1 },
        scrollIntoView: true,
      });
      return null;
    },
    focus: () => view.b.focus(),
    destroy: () => view.destroy(),
    requestMeasure: () => {
      view.a.requestMeasure();
      view.b.requestMeasure();
    },
  };
}
