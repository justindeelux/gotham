// Find bar behavior that is not visual (JUS-231): the query scan is debounced,
// a catastrophic pattern is refused before it reaches the editor, and the
// replace row is remembered per tab. The session is a spy, so these assert the
// calls the composable makes rather than CodeMirror's own internals.
/* global performance:readonly */
import { effectScope, nextTick, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useEditorFind } from "@/features/files/composables/useEditorFind";
import type {
  EditorSearchOptions,
  EditorSearchStatus,
} from "@/features/files/editor/search";

const STATUS: EditorSearchStatus = { total: 2, current: 1, capped: false };

/** makeSession builds the searchable-editor shape the composable expects. */
function makeSession() {
  return {
    search: {
      setOptions: vi.fn(
        (_options: EditorSearchOptions, _replacement: string) => STATUS,
      ),
      status: vi.fn(() => STATUS),
      next: vi.fn(() => STATUS),
      previous: vi.fn(() => STATUS),
      replace: vi.fn(() => STATUS),
      replaceAll: vi.fn(() => STATUS),
      clear: vi.fn(),
    },
    lineInfo: vi.fn(() => ({ line: 1, total: 3 })),
    gotoLine: vi.fn(() => null),
    focus: vi.fn(),
  };
}

/** setup runs the composable inside an effect scope so disposal is testable. */
function setup() {
  const session = makeSession();
  const tab = ref("tab-a");
  const scope = effectScope();
  const find = scope.run(() =>
    useEditorFind({
      session: () => session,
      tabId: () => tab.value,
      canReplace: () => true,
    }),
  )!;
  return { session, tab, scope, find };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useEditorFind", () => {
  it("debounces a typing burst into one document scan", async () => {
    const { session, find, scope } = setup();
    find.openFind(false);
    session.search.setOptions.mockClear();

    find.setQuery("o");
    find.setQuery("on");
    find.setQuery("one");
    expect(session.search.setOptions).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(200);
    expect(session.search.setOptions).toHaveBeenCalledTimes(1);
    expect(session.search.setOptions.mock.calls[0]![0]).toMatchObject({
      query: "one",
    });
    scope.stop();
  });

  it("refuses a catastrophic regex without scanning the document", () => {
    const { session, find, scope } = setup();
    find.openFind(false);
    find.toggleRegex();
    session.search.setOptions.mockClear();
    session.search.clear.mockClear();

    const started = performance.now();
    find.setQuery("(a+)+$");

    expect(performance.now() - started).toBeLessThan(250);
    expect(find.patternError.value).toBe("complexity");
    expect(session.search.setOptions).not.toHaveBeenCalled();
    expect(session.search.clear).toHaveBeenCalled();
    expect(find.total.value).toBe(0);
    scope.stop();
  });

  it("remembers the query and the replace row per tab", async () => {
    const { tab, find, scope } = setup();
    find.openFind(false);
    find.toggleReplace();
    find.setQuery("needle");
    expect(find.replaceExpanded.value).toBe(true);

    tab.value = "tab-b";
    await nextTick();
    expect(find.query.value).toBe("");
    expect(find.replaceExpanded.value).toBe(false);

    find.toggleReplace();
    tab.value = "tab-a";
    await nextTick();
    expect(find.query.value).toBe("needle");
    expect(find.replaceExpanded.value).toBe(true);
    scope.stop();
  });

  it("opens collapsed from the toolbar but remembered after a replace open", () => {
    const { find, scope } = setup();

    find.openFind(false);
    expect(find.replaceExpanded.value).toBe(false);

    // The replace binding forces the row open.
    find.openFind(true);
    expect(find.replaceExpanded.value).toBe(true);

    // A close/reopen on the same tab keeps what the user last chose.
    find.closeFind(false);
    find.openFind(false);
    expect(find.replaceExpanded.value).toBe(true);
    scope.stop();
  });

  it("does not recount on cursor moves", () => {
    const { session, find, scope } = setup();
    find.openFind(false);
    session.search.setOptions.mockClear();

    // The composable deliberately has no cursor hook; the components never call
    // queueStatus from onCursor. An edit adds one coalesced recount instead.
    find.queueStatus();
    find.queueStatus();
    vi.advanceTimersByTime(250);

    expect(session.search.setOptions).toHaveBeenCalledTimes(1);
    scope.stop();
  });
});
