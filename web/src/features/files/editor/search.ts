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

export { isValidRegex } from "@/features/files/editor/searchPattern";

/** EditorSearchOptions is the find bar's query and toggles. */
export interface EditorSearchOptions {
  query: string;
  caseSensitive: boolean;
  wholeWord: boolean;
  regexp: boolean;
}

/** EditorSearchStatus is the match count after a search action. `current` is
 *  1-based, or 0 when the cursor sits before no match / there are none. */
export interface EditorSearchStatus {
  total: number;
  current: number;
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
 * matching rules. An invalid regex counts as no results.
 */
export function searchStatus(
  text: Text,
  options: EditorSearchOptions,
  anchor: number,
): EditorSearchStatus {
  const query = toQuery(options);
  if (!query.valid) {
    return { total: 0, current: 0 };
  }
  let total = 0;
  let inside = 0;
  let after = 0;
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
  }
  return { total, current: inside || after || (total > 0 ? 1 : 0) };
}

const matchMark = Decoration.mark({ class: "cm-searchMatch" });
const selectedMatchMark = Decoration.mark({ class: "cm-searchMatch cm-searchMatch-selected" });

/** matchDecorations marks every match in the visible ranges, not the whole doc. */
function matchDecorations(view: EditorView): DecorationSet {
  const query = getSearchQuery(view.state);
  if (!query.valid) {
    return Decoration.none;
  }
  const selection = view.state.selection.ranges;
  const ranges: Range<Decoration>[] = [];
  for (const { from, to } of view.visibleRanges) {
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

  /** statusOf counts matches in the primary pane. */
  function statusOf(): EditorSearchStatus {
    if (!primary) {
      return { total: 0, current: 0 };
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
      apply(options);
      return statusOf();
    },
    status: statusOf,
    next() {
      // A command on an invalid query would make CodeMirror fall back to
      // opening its default panel; never give it that chance.
      if (primary && getSearchQuery(primary.state).valid) {
        findNext(primary);
      }
      return statusOf();
    },
    previous() {
      if (primary && getSearchQuery(primary.state).valid) {
        findPrevious(primary);
      }
      return statusOf();
    },
    replace(nextReplacement) {
      if (!primary || !getSearchQuery(primary.state).valid) {
        return statusOf();
      }
      replacement = nextReplacement;
      const query = toQuery(optionsFrom(primary.state), replacement);
      primary.dispatch({ effects: setSearchQuery.of(query) });
      // The first call only selects the match when the cursor is not already
      // on one; the second replaces that freshly selected match.
      const before = primary.state.doc;
      replaceNext(primary);
      if (primary.state.doc === before) {
        replaceNext(primary);
      }
      return statusOf();
    },
    replaceAll(nextReplacement) {
      if (!primary || !getSearchQuery(primary.state).valid) {
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
