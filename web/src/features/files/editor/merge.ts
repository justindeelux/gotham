/**
 * Read-only history diff (JUS-224), built by the same lazy editor chunk as the
 * normal editor so it shares the explicit grammar set and theme and never lands
 * in the entry bundle.
 */
import { MergeView } from "@codemirror/merge";
import { EditorState } from "@codemirror/state";
import type { Extension } from "@codemirror/state";
import { EditorView, lineNumbers } from "@codemirror/view";

import { gothamTheme, loadLanguage } from "@/features/files/editor/codemirror";
import type { EditorLanguageId } from "@/features/files/utils/editor";

/** MergeSession is the imperative handle a component keeps per diff tab. */
export interface MergeSession {
  destroy: () => void;
  /** requestMeasure re-lays out both panes after a hidden host is shown. */
  requestMeasure: () => void;
}

/** MergeSessionOptions configures one side-by-side read-only diff. */
export interface MergeSessionOptions {
  parent: HTMLElement;
  /** base is the stored history revision (left side). */
  base: string;
  /** current is the live file body at open time (right side). */
  current: string;
  language: EditorLanguageId;
}

/** mergeTheme maps added/removed chunks onto the app's surface tokens. */
const mergeTheme = EditorView.theme({
  "&": {
    height: "100%",
    backgroundColor: "var(--surface-warm)",
    fontSize: "var(--text-sm)",
  },
  ".cm-mergeViewEditors": { height: "100%" },
  ".cm-editor": { height: "100%" },
  ".cm-changedLine": { backgroundColor: "color-mix(in oklab, var(--accent) 18%, transparent)" },
  ".cm-changedText": { backgroundColor: "color-mix(in oklab, var(--success) 40%, transparent)" },
  ".cm-deletedChunk": { backgroundColor: "color-mix(in oklab, var(--danger) 16%, transparent)" },
  ".cm-insertedLine": { backgroundColor: "color-mix(in oklab, var(--success) 16%, transparent)" },
  ".cm-changedLineGutter": { backgroundColor: "color-mix(in oklab, var(--accent) 28%, transparent)" },
});

/** readOnlyExtensions make both sides non-editable and non-writable. */
function readOnlyExtensions(): Extension[] {
  return [EditorState.readOnly.of(true), EditorView.editable.of(false)];
}

/**
 * createMergeSession builds one side-by-side diff. Both sides are read-only, so
 * a history tab can never be edited or saved.
 */
export async function createMergeSession(
  options: MergeSessionOptions,
): Promise<MergeSession> {
  const language = await loadLanguage(options.language);
  const side = (doc: string): { doc: string; extensions: Extension[] } => ({
    doc,
    extensions: [lineNumbers(), language, gothamTheme, mergeTheme, ...readOnlyExtensions()],
  });
  const view = new MergeView({
    parent: options.parent,
    a: side(options.base),
    b: side(options.current),
    highlightChanges: true,
    gutter: true,
    collapseUnchanged: { margin: 3, minSize: 4 },
  });
  return {
    destroy: () => view.destroy(),
    requestMeasure: () => {
      view.a.requestMeasure();
      view.b.requestMeasure();
    },
  };
}
