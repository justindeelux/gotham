/**
 * Find/replace + go-to-line state for one editor surface (JUS-231). The query
 * and its toggles are kept per tab here, while the actual matches live in
 * CodeMirror's search state, so a tab switch restores what the user typed and
 * the highlights always come from the live document.
 */
import { computed, ref, watch } from "vue";

import type { GotoLineError } from "@/features/files/editor/goto";
import { isValidRegex } from "@/features/files/editor/searchPattern";
import type {
  EditorSearchOptions,
  EditorSearchStatus,
  SearchableEditor,
} from "@/features/files/editor/search";

export interface UseEditorFindOptions {
  /** session returns the active tab's searchable editor, or null while loading. */
  session: () => SearchableEditor | null;
  /** tabId keys the stored query; a change saves the old tab and loads the new. */
  tabId: () => string;
  /** canReplace is false on read-only tabs and for a read-only role. */
  canReplace: () => boolean;
}

interface SavedFindState extends EditorSearchOptions {
  replacement: string;
}

/** useEditorFind owns the find bar and go-to-line popover state. */
export function useEditorFind(options: UseEditorFindOptions) {
  const findOpen = ref(false);
  const replaceExpanded = ref(false);
  const focusReplace = ref(false);
  const gotoOpen = ref(false);
  const query = ref("");
  const replacement = ref("");
  const caseSensitive = ref(false);
  const wholeWord = ref(false);
  const regexp = ref(false);
  const total = ref(0);
  const current = ref(0);
  const gotoCurrent = ref(1);
  const gotoTotal = ref(1);
  const saved = new Map<string, SavedFindState>();

  /** canReplace follows the active tab's read-only state. */
  const canReplace = computed<boolean>(() => options.canReplace());

  /** invalidRegex turns on the inline error without throwing at the editor. */
  const invalidRegex = computed<boolean>(
    () => regexp.value && query.value.length > 0 && !isValidRegex(query.value),
  );

  /** currentOptions snapshots the form for the search controller. */
  function currentOptions(): EditorSearchOptions {
    return {
      query: query.value,
      caseSensitive: caseSensitive.value,
      wholeWord: wholeWord.value,
      regexp: regexp.value,
    };
  }

  /** publish stores one controller result in the reactive bar state. */
  function publish(status: EditorSearchStatus): void {
    total.value = status.total;
    current.value = status.current;
  }

  /** applySearch installs the query on the active session. */
  function applySearch(): void {
    const editor = options.session();
    if (!editor) {
      return;
    }
    publish(editor.search.setOptions(currentOptions(), replacement.value));
  }

  /** loadState restores one tab's query and toggles. */
  function loadState(tabId: string): void {
    const stored = saved.get(tabId);
    query.value = stored?.query ?? "";
    replacement.value = stored?.replacement ?? "";
    caseSensitive.value = stored?.caseSensitive ?? false;
    wholeWord.value = stored?.wholeWord ?? false;
    regexp.value = stored?.regexp ?? false;
  }

  watch(options.tabId, (next, previous) => {
    if (previous) {
      saved.set(previous, { ...currentOptions(), replacement: replacement.value });
    }
    loadState(next);
    if (findOpen.value) {
      applySearch();
    }
    if (gotoOpen.value) {
      refreshGotoInfo();
    }
  });

  /** openFind opens the bar, optionally with the replace row expanded. */
  function openFind(expand: boolean): void {
    if (gotoOpen.value) {
      closeGotoLine(false);
    }
    replaceExpanded.value = expand;
    focusReplace.value = expand;
    findOpen.value = true;
    applySearch();
  }

  /** closeFind closes the bar, clears the highlights and refocuses the editor. */
  function closeFind(focus = true): void {
    if (!findOpen.value) {
      return;
    }
    findOpen.value = false;
    replaceExpanded.value = false;
    options.session()?.search.clear();
    total.value = 0;
    current.value = 0;
    if (focus) {
      options.session()?.focus();
    }
  }

  /** toggleReplace shows or hides the replacement row. */
  function toggleReplace(): void {
    replaceExpanded.value = !replaceExpanded.value;
  }

  /** setQuery applies a typed query immediately. */
  function setQuery(value: string): void {
    query.value = value;
    applySearch();
  }

  /** setReplacement only stores the text; matching does not depend on it. */
  function setReplacement(value: string): void {
    replacement.value = value;
  }

  /** toggleOption flips one search toggle and reruns the query. */
  function toggleOption(option: "caseSensitive" | "wholeWord" | "regexp"): void {
    if (option === "caseSensitive") {
      caseSensitive.value = !caseSensitive.value;
    } else if (option === "wholeWord") {
      wholeWord.value = !wholeWord.value;
    } else {
      regexp.value = !regexp.value;
    }
    applySearch();
  }

  /** next selects the next match and refreshes the counter. */
  function next(): void {
    const editor = options.session();
    if (editor && !invalidRegex.value) {
      publish(editor.search.next());
    }
  }

  /** previous selects the previous match and refreshes the counter. */
  function previous(): void {
    const editor = options.session();
    if (editor && !invalidRegex.value) {
      publish(editor.search.previous());
    }
  }

  /** replaceSwap replaces the current match (one undo step). */
  function replaceMatch(): void {
    const editor = options.session();
    if (editor && canReplace.value && !invalidRegex.value) {
      publish(editor.search.replace(replacement.value));
    }
  }

  /** replaceEveryMatch replaces every match in one undoable transaction. */
  function replaceEveryMatch(): void {
    const editor = options.session();
    if (editor && canReplace.value && !invalidRegex.value) {
      publish(editor.search.replaceAll(replacement.value));
    }
  }

  /** refreshGotoInfo reads the current line and the document line count. */
  function refreshGotoInfo(): void {
    const info = options.session()?.lineInfo();
    gotoCurrent.value = info?.line ?? 1;
    gotoTotal.value = info?.total ?? 1;
  }

  /** openGotoLine shows the popover with the live line numbers. */
  function openGotoLine(): void {
    if (findOpen.value) {
      closeFind(false);
    }
    refreshGotoInfo();
    gotoOpen.value = true;
  }

  /** closeGotoLine hides the popover and returns focus to the editor. */
  function closeGotoLine(focus = true): void {
    if (!gotoOpen.value) {
      return;
    }
    gotoOpen.value = false;
    if (focus) {
      options.session()?.focus();
    }
  }

  /** jumpToLine rejects an out-of-range line/column inline, else jumps. */
  function jumpToLine(line: number, column: number): GotoLineError | null {
    const editor = options.session();
    if (!editor) {
      return "line";
    }
    const failure = editor.gotoLine(line, column);
    if (failure) {
      return failure;
    }
    gotoOpen.value = false;
    editor.focus();
    return null;
  }

  /** refresh re-applies the query after the active session is (re)built. */
  function refresh(): void {
    if (findOpen.value) {
      applySearch();
    }
    if (gotoOpen.value) {
      refreshGotoInfo();
    }
  }

  /** syncStatus updates the counter after an edit or a cursor move. */
  function syncStatus(): void {
    if (findOpen.value) {
      applySearch();
    }
  }

  return {
    findOpen,
    replaceExpanded,
    focusReplace,
    gotoOpen,
    query,
    replacement,
    caseSensitive,
    wholeWord,
    regexp,
    total,
    current,
    gotoCurrent,
    gotoTotal,
    canReplace,
    invalidRegex,
    openFind,
    closeFind,
    toggleReplace,
    setQuery,
    setReplacement,
    toggleCase: () => toggleOption("caseSensitive"),
    toggleWord: () => toggleOption("wholeWord"),
    toggleRegex: () => toggleOption("regexp"),
    next,
    previous,
    replaceMatch,
    replaceEveryMatch,
    openGotoLine,
    closeGotoLine,
    jumpToLine,
    refresh,
    syncStatus,
  };
}
