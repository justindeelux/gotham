/**
 * Read-only history diff (JUS-224), built by the same lazy editor chunk as the
 * normal editor so it shares the explicit grammar set and settings theme and
 * never lands in the entry bundle.
 */
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

/** MergeSession is the imperative handle a component keeps per diff tab. */
export interface MergeSession {
  setSettings: (_settings: EditorSettings) => void;
  destroy: () => void;
  /** requestMeasure re-lays out both panes after a hidden host is shown. */
  requestMeasure: () => void;
  /** search drives the find bar over the current (right) pane. */
  search: EditorSearchController;
  lineInfo: () => EditorLineInfo;
  gotoLine: (_line: number, _column: number) => "line" | "column" | null;
  focus: () => void;
}

/** MergeSessionOptions configures one side-by-side read-only diff. */
export interface MergeSessionOptions {
  parent: HTMLElement;
  /** base is the stored history revision (left side). */
  base: string;
  /** current is the live file body at open time (right side). */
  current: string;
  language: EditorLanguageId;
  settings: EditorSettings;
  /** The custom find/replace and go-to-line surfaces (JUS-231), so the shared
   *  shortcuts work with focus inside a diff pane. */
  onFind: () => void;
  onReplace: () => void;
  onGoToLine: () => void;
}

/** mergeTheme maps added/removed chunks onto the app's surface tokens. */
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

/** readOnlyExtensions make both sides non-editable and non-writable. */
function readOnlyExtensions(): Extension[] {
  return [EditorState.readOnly.of(true), EditorView.editable.of(false)];
}

/**
 * diffSettings drops the completion popup from the read-only panes: it could
 * never insert text there. Bracket closing is always-on (see
 * `editorSettingsExtensions`) and harmless on a non-editable document.
 */
function diffSettings(settings: EditorSettings): EditorSettings {
  return settings.autocomplete ? { ...settings, autocomplete: false } : settings;
}

/**
 * createMergeSession builds one side-by-side diff. Both sides are read-only, so
 * a history tab can never be edited or saved. Each side keeps the shared editor
 * settings in its own compartment, so a preference change reconfigures both
 * panes without rebuilding the view.
 */
export async function createMergeSession(
  options: MergeSessionOptions,
): Promise<MergeSession> {
  const language = await loadLanguage(options.language);
  const settingsA = new Compartment();
  const settingsB = new Compartment();
  // The read-only panes can still find, jump and open the replace surface
  // (disabled there); the file commands are no-ops.
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
