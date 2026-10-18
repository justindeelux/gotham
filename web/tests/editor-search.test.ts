// Custom find/replace over CodeMirror's search state (JUS-231): the match
// count, the query options, replacement undo and the go-to-line parser. The
// controller is exercised on a real EditorView so the search state, the
// highlight decoration and the history integration are the real ones.
/* global document:readonly */
import { history, undo } from "@codemirror/commands";
import { EditorState } from "@codemirror/state";
import type { Text } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { getSearchQuery } from "@codemirror/search";
import { afterEach, describe, expect, it } from "vitest";

import { parseGotoLine } from "@/features/files/editor/goto";
import {
  createSearchController,
  customSearchExtensions,
  isValidRegex,
  searchStatus,
} from "@/features/files/editor/search";
import type { EditorSearchOptions } from "@/features/files/editor/search";

/** options builds one query spec with every toggle off. */
function options(query: string, extra: Partial<EditorSearchOptions> = {}): EditorSearchOptions {
  return {
    query,
    caseSensitive: false,
    wholeWord: false,
    regexp: false,
    ...extra,
  };
}

/** text builds a CodeMirror Text from one document string. */
function text(doc: string): Text {
  return EditorState.create({ doc }).doc;
}

/** makeView builds a real editable view with undo history and the search UI. */
function makeView(doc: string): EditorView {
  const host = document.createElement("div");
  document.body.append(host);
  return new EditorView({
    parent: host,
    state: EditorState.create({
      doc,
      extensions: [history(), customSearchExtensions()],
    }),
  });
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("isValidRegex", () => {
  it("accepts plain text and rejects an unclosed group", () => {
    expect(isValidRegex("plain")).toBe(true);
    expect(isValidRegex("a.*b")).toBe(true);
    expect(isValidRegex("(")).toBe(false);
  });
});

describe("searchStatus", () => {
  it("counts every match and names the one at the cursor", () => {
    const doc = text("one two\none");
    expect(searchStatus(doc, options("one"), 0)).toEqual({ total: 2, current: 1 });
    expect(searchStatus(doc, options("one"), 4)).toEqual({ total: 2, current: 2 });
  });

  it("honours case, whole word and regexp", () => {
    expect(searchStatus(text("One one"), options("one"), 0).total).toBe(2);
    expect(
      searchStatus(text("One one"), options("one", { caseSensitive: true }), 0).total,
    ).toBe(1);
    expect(
      searchStatus(text("One one"), options("One", { caseSensitive: true }), 0).total,
    ).toBe(1);
    expect(
      searchStatus(text("one someone one"), options("one"), 0).total,
    ).toBe(3);
    expect(
      searchStatus(text("one someone one"), options("one", { wholeWord: true }), 0)
        .total,
    ).toBe(2);
    expect(searchStatus(text("one two one"), options("o.e", { regexp: true }), 0).total).toBe(
      2,
    );
  });

  it("reports no results for an invalid regex or an empty query", () => {
    const doc = text("one");
    expect(searchStatus(doc, options("(", { regexp: true }), 0)).toEqual({
      total: 0,
      current: 0,
    });
    expect(searchStatus(doc, options(""), 0)).toEqual({ total: 0, current: 0 });
  });
});

describe("search controller", () => {
  it("installs the query in the search state and highlights the matches", () => {
    const view = makeView("one two one");
    const search = createSearchController([view]);

    expect(search.setOptions(options("one"), "")).toEqual({ total: 2, current: 1 });
    expect(getSearchQuery(view.state).search).toBe("one");
    expect(view.dom.querySelectorAll(".cm-searchMatch")).toHaveLength(2);

    search.clear();
    expect(view.dom.querySelectorAll(".cm-searchMatch")).toHaveLength(0);
    expect(view.dom.querySelector(".cm-panels")).toBeNull();
    view.destroy();
  });

  it("walks the matches forward and backward, wrapping at the ends", () => {
    const view = makeView("one two one");
    const search = createSearchController([view]);
    search.setOptions(options("one"), "");

    expect(search.next()).toEqual({ total: 2, current: 1 });
    expect(search.next()).toEqual({ total: 2, current: 2 });
    expect(search.next()).toEqual({ total: 2, current: 1 });
    expect(search.previous()).toEqual({ total: 2, current: 2 });
    view.destroy();
  });

  it("replaces one match and undoes it in a single step", () => {
    const view = makeView("one two one");
    const search = createSearchController([view]);
    search.setOptions(options("one"), "1");

    search.replace("1");
    expect(view.state.doc.toString()).toBe("1 two one");

    undo(view);
    expect(view.state.doc.toString()).toBe("one two one");
    view.destroy();
  });

  it("replaces every match as one undoable transaction", () => {
    const view = makeView("one two one");
    const search = createSearchController([view]);
    search.setOptions(options("one"), "X");

    expect(search.replaceAll("X")).toEqual({ total: 0, current: 0 });
    expect(view.state.doc.toString()).toBe("X two X");

    undo(view);
    expect(view.state.doc.toString()).toBe("one two one");
    view.destroy();
  });

  it("does not throw or edit on an invalid regex", () => {
    const view = makeView("one (two");
    const search = createSearchController([view]);
    expect(search.setOptions(options("(", { regexp: true }), "X")).toEqual({
      total: 0,
      current: 0,
    });
    expect(search.next()).toEqual({ total: 0, current: 0 });
    expect(search.replaceAll("X")).toEqual({ total: 0, current: 0 });
    expect(view.state.doc.toString()).toBe("one (two");
    // A fallback to CodeMirror's default panel is the one regression this
    // surface must never have.
    expect(view.dom.querySelector(".cm-panels")).toBeNull();
    view.destroy();
  });
});

describe("parseGotoLine", () => {
  it("accepts a line or line:column", () => {
    expect(parseGotoLine("12", 20)).toEqual({ target: { line: 12, column: 1 } });
    expect(parseGotoLine(" 4:7 ", 20)).toEqual({ target: { line: 4, column: 7 } });
    expect(parseGotoLine("20", 20)).toEqual({ target: { line: 20, column: 1 } });
  });

  it("rejects zero, out-of-range, non-numeric and a zero column", () => {
    expect(parseGotoLine("0", 20)).toEqual({ error: "line" });
    expect(parseGotoLine("21", 20)).toEqual({ error: "line" });
    expect(parseGotoLine("-2", 20)).toEqual({ error: "format" });
    expect(parseGotoLine("abc", 20)).toEqual({ error: "format" });
    expect(parseGotoLine("", 20)).toEqual({ error: "format" });
    expect(parseGotoLine("4:0", 20)).toEqual({ error: "column" });
  });
});
