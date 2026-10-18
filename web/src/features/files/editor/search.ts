/**
 * Custom find/replace over CodeMirror's search state (JUS-231). The search UI
 * is Vue + Naive UI; this module is the only place that touches
 * `@codemirror/search`, so the commands, the match count and the highlight
 * decoration cannot drift from the state the queries are stored in.
 *
 * The default panel is deliberately never opened: the match highlighter below
 * is ours, because CodeMirror's only draws while its own panel is open.
 */
import {
  SearchQuery,
  findNext,
  findPrevious,
  getSearchQuery,
  replaceAll,
  replaceNext,
  search,
  setSearchQuery,
} from "@codemirror/search";
import type { EditorState, Extension, Range, Text } from "@codemirror/state";
import { Decoration, EditorView, ViewPlugin } from "@codemirror/view";
import type { DecorationSet, ViewUpdate } from "@codemirror/view";

import { validateSearchPattern } from "@/features/files/editor/searchPattern";

export { isValidRegex } from "@/features/files/editor/searchPattern";

/** MAX_SEARCH_MATCHES caps the counter; the bar shows "1000+" instead. */
export const MAX_SEARCH_MATCHES = 1000;

/** SEARCH_TIME_BUDGET_MS bounds one counter pass on a huge document. */
const SEARCH_TIME_BUDGET_MS = 12;

/** MAX_VISIBLE_MATCHES bounds the decorations one viewport can carry. */
const MAX_VISIBLE_MATCHES = 4000;

const EMPTY_STATUS: EditorSearchStatus = { total: 0, current: 0, capped: false };

/** EditorSearchOptions is the find bar's query and toggles. */
export interface EditorSearchOptions {
  query: string;
  caseSensitive: boolean;
  wholeWord: boolean;
  regexp: boolean;
}

/** EditorSearchStatus is the match count after a search action. `current` is
 *  1-based, or 0 when the cursor sits before no match / there are none.
 *  `capped` means the scan stopped at the match cap or the time budget. */
export interface EditorSearchStatus {
  total: number;
  current: number;
  capped: boolean;
}

/** SearchableEditor is the surface both the file editor and the diff expose. */
export interface SearchableEditor {
  search: EditorSearchController;
  lineInfo: () => { line: number; total: number };
  gotoLine: (_line: number, _column: number) => "line" | "column" | null;
  focus: () => void;
}

/** EditorSearchController drives one view's search state. */
export interface EditorSearchController {
  /** setOptions installs the query (and replacement) in every view. */
  setOptions: (_options: EditorSearchOptions, _replacement: string) => EditorSearchStatus;
  status: () => EditorSearchStatus;
  next: () => EditorSearchStatus;
  previous: () => EditorSearchStatus;
  replace: (_replacement: string) => EditorSearchStatus;
  replaceAll: (_replacement: string) => EditorSearchStatus;
  clear: () => void;
}

/** toQuery builds the CodeMirror query for one option set. */
function toQuery(options: EditorSearchOptions, replacement = ""): SearchQuery {
  return new SearchQuery({
    search: options.query,
    caseSensitive: options.caseSensitive,
    wholeWord: options.wholeWord,
    regexp: options.regexp,
    replace: replacement,
  });
}

/** optionsFrom reads the query back out of the editor state. */
function optionsFrom(state: EditorState): EditorSearchOptions {
  const query = getSearchQuery(state);
  return {
    query: query.search,
    caseSensitive: query.caseSensitive,
    wholeWord: query.wholeWord,
    regexp: query.regexp,
  };
}

/**
 * searchStatus counts the matches in `text` and locates the one at or after
 * `anchor`, so the bar can say "3 of 12" without owning a second copy of the
 * matching rules. A catastrophic pattern is rejected before it runs, the count
 * stops at {@link MAX_SEARCH_MATCHES}, and a time budget bounds the pass, so a
 * huge document degrades to "1000+" instead of blocking the editor.
 */
export function searchStatus(
  text: Text,
  options: EditorSearchOptions,
  anchor: number,
): EditorSearchStatus {
  // `validateSearchPattern` also catches the nested-quantifier shapes that can
  // backtrack exponentially; never hand one to RegExp.exec.
  if (options.regexp && validateSearchPattern(options.query) !== null) {
    return EMPTY_STATUS;
  }
  const query = toQuery(options);
  if (!query.valid) {
    return EMPTY_STATUS;
  }
  const deadline = performance.now() + SEARCH_TIME_BUDGET_MS;
  let total = 0;
  let inside = 0;
  let after = 0;
  let capped = false;
  const cursor = query.getCursor(text);
  for (let step = cursor.next(); !step.done; step = cursor.next()) {
    const { from, to } = step.value;
    total += 1;
    if (inside === 0 && from <= anchor && anchor <= to) {
      inside = total;
    }
    if (after === 0 && from >= anchor) {
      after = total;
    }
    if (total >= MAX_SEARCH_MATCHES) {
      capped = true;
      break;
    }
    if ((total & 31) === 0 && performance.now() > deadline) {
      capped = true;
      break;
    }
  }
  return { total, current: inside || after || (total > 0 ? 1 : 0), capped };
}

const matchMark = Decoration.mark({ class: "cm-searchMatch" });
const selectedMatchMark = Decoration.mark({ class: "cm-searchMatch cm-searchMatch-selected" });

/** matchDecorations marks every match in the visible ranges, not the whole doc. */
function matchDecorations(view: EditorView): DecorationSet {
  const query = getSearchQuery(view.state);
  if (
    !query.valid ||
    (query.regexp && validateSearchPattern(query.search) !== null)
  ) {
    return Decoration.none;
  }
  const selection = view.state.selection.ranges;
  const ranges: Range<Decoration>[] = [];
  outer: for (const { from, to } of view.visibleRanges) {
    const cursor = query.getCursor(view.state, from, to);
    for (let step = cursor.next(); !step.done; step = cursor.next()) {
      const match = step.value;
      // Mark decorations cannot be empty; a zero-width regex match (/^/) still
      // counts in the total, it just has nothing to paint.
      if (match.from === match.to) {
        continue;
      }
      const selected = selection.some(
        (range) => range.from === match.from && range.to === match.to,
      );
      ranges.push(
        (selected ? selectedMatchMark : matchMark).range(match.from, match.to),
      );
      if (ranges.length >= MAX_VISIBLE_MATCHES) {
        break outer;
      }
    }
  }
  return Decoration.set(ranges, true);
}

/** searchMatchHighlighter repaints the matches, reusing the editor's tokens. */
const searchMatchHighlighter = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = matchDecorations(view);
    }

    update(update: ViewUpdate): void {
      const queryChanged = update.transactions.some((transaction) =>
        transaction.effects.some((effect) => effect.is(setSearchQuery)),
      );
      if (
        queryChanged ||
        update.docChanged ||
        update.selectionSet ||
        update.viewportChanged
      ) {
        this.decorations = matchDecorations(update.view);
      }
    }
  },
  { decorations: (value) => value.decorations },
);

/** customSearchExtensions installs the search state plus our highlighter. */
export function customSearchExtensions(): Extension {
  return [search(), searchMatchHighlighter];
}

/**
 * createSearchController wraps one or more views (the diff has two panes but
 * navigates the current side). `views[0]` owns the selection, the match count
 * and the replacement; every view gets the query so both panes highlight.
 */
export function createSearchController(views: EditorView[]): EditorSearchController {
  const primary = views[0];
  let replacement = "";

  /** apply syncs the current query into every pane. */
  function apply(options: EditorSearchOptions): void {
    const query = toQuery(options, replacement);
    for (const view of views) {
      if (!getSearchQuery(view.state).eq(query)) {
        view.dispatch({ effects: setSearchQuery.of(query) });
      }
    }
  }

  /** usable reports whether a command may run: valid and not catastrophic. */
  function usable(view: EditorView | undefined): view is EditorView {
    if (!view) {
      return false;
    }
    const query = getSearchQuery(view.state);
    return (
      query.valid &&
      (!query.regexp || validateSearchPattern(query.search) === null)
    );
  }

  /** selectionIsMatch reports whether the selection is exactly one match, so a
   *  replace must not first walk to the next one. */
  function selectionIsMatch(): boolean {
    if (!primary) {
      return false;
    }
    const selection = primary.state.selection.main;
    if (selection.empty) {
      return false;
    }
    const cursor = getSearchQuery(primary.state).getCursor(
      primary.state,
      selection.from,
      selection.to,
    );
    const first = cursor.next();
    return (
      !first.done &&
      first.value.from === selection.from &&
      first.value.to === selection.to
    );
  }

  /** statusOf counts matches in the primary pane. */
  function statusOf(): EditorSearchStatus {
    if (!primary) {
      return EMPTY_STATUS;
    }
    return searchStatus(
      primary.state.doc,
      optionsFrom(primary.state),
      primary.state.selection.main.from,
    );
  }

  return {
    setOptions(options, nextReplacement) {
      replacement = nextReplacement;
      if (
        options.regexp &&
        validateSearchPattern(options.query) !== null
      ) {
        return EMPTY_STATUS;
      }
      apply(options);
      return statusOf();
    },
    status: statusOf,
    next() {
      // A command on an invalid query would make CodeMirror fall back to
      // opening its default panel; never give it that chance.
      if (usable(primary)) {
        findNext(primary);
      }
      return statusOf();
    },
    previous() {
      if (usable(primary)) {
        findPrevious(primary);
      }
      return statusOf();
    },
    replace(nextReplacement) {
      if (!usable(primary)) {
        return statusOf();
      }
      replacement = nextReplacement;
      const query = toQuery(optionsFrom(primary.state), replacement);
      primary.dispatch({ effects: setSearchQuery.of(query) });
      if (!selectionIsMatch()) {
        // Replace walks from the cursor when it is not already on a match.
        findNext(primary);
      }
      replaceNext(primary);
      return statusOf();
    },
    replaceAll(nextReplacement) {
      if (!usable(primary)) {
        return statusOf();
      }
      replacement = nextReplacement;
      const query = toQuery(optionsFrom(primary.state), replacement);
      primary.dispatch({ effects: setSearchQuery.of(query) });
      replaceAll(primary);
      return statusOf();
    },
    clear() {
      replacement = "";
      const empty = new SearchQuery({ search: "" });
      for (const view of views) {
        if (getSearchQuery(view.state).search !== "") {
          view.dispatch({ effects: setSearchQuery.of(empty) });
        }
      }
    },
  };
}
