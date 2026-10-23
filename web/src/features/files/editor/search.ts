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

export const MAX_SEARCH_MATCHES = 1000;

const SEARCH_TIME_BUDGET_MS = 12;

const MAX_VISIBLE_MATCHES = 4000;

const EMPTY_STATUS: EditorSearchStatus = { total: 0, current: 0, capped: false };

export interface EditorSearchOptions {
  query: string;
  caseSensitive: boolean;
  wholeWord: boolean;
  regexp: boolean;
}

export interface EditorSearchStatus {
  total: number;
  current: number;
  capped: boolean;
}

export interface SearchableEditor {
  search: EditorSearchController;
  lineInfo: () => { line: number; total: number };
  gotoLine: (_line: number, _column: number) => "line" | "column" | null;
  focus: () => void;
}

export interface EditorSearchController {
  setOptions: (_options: EditorSearchOptions, _replacement: string) => EditorSearchStatus;
  status: () => EditorSearchStatus;
  next: () => EditorSearchStatus;
  previous: () => EditorSearchStatus;
  replace: (_replacement: string) => EditorSearchStatus;
  replaceAll: (_replacement: string) => EditorSearchStatus;
  clear: () => void;
}

function toQuery(options: EditorSearchOptions, replacement = ""): SearchQuery {
  return new SearchQuery({
    search: options.query,
    caseSensitive: options.caseSensitive,
    wholeWord: options.wholeWord,
    regexp: options.regexp,
    replace: replacement,
  });
}

function optionsFrom(state: EditorState): EditorSearchOptions {
  const query = getSearchQuery(state);
  return {
    query: query.search,
    caseSensitive: query.caseSensitive,
    wholeWord: query.wholeWord,
    regexp: query.regexp,
  };
}

export function searchStatus(
  text: Text,
  options: EditorSearchOptions,
  anchor: number,
): EditorSearchStatus {
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

export function customSearchExtensions(): Extension {
  return [search(), searchMatchHighlighter];
}

export function createSearchController(views: EditorView[]): EditorSearchController {
  const primary = views[0];
  let replacement = "";

  function apply(options: EditorSearchOptions): void {
    const query = toQuery(options, replacement);
    for (const view of views) {
      if (!getSearchQuery(view.state).eq(query)) {
        view.dispatch({ effects: setSearchQuery.of(query) });
      }
    }
  }

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
