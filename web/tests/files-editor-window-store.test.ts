// JUS-232: the floating editor window persists its size, position and
// minimized flag per user, clamps them back into the viewport on restore, and
// exposes the open-tab count behind the rail badge.
/* global window:readonly */
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createPinia, setActivePinia } from "pinia";

import {
  DEFAULT_EDITOR_WINDOW,
  EDITOR_WINDOW_VERSION,
  parseEditorWindow,
} from "@/features/files/schemas/editorWindow";
import { useEditorWindowStore } from "@/features/files/stores/editorWindow";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import {
  clampEditorSize,
  editorWindowStorageKey,
  readEditorWindow,
  writeEditorWindow,
} from "@/features/files/utils/editorWindow";

/** memoryStorage is a Map-backed localStorage stand-in. */
function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
  };
}

/** seedSession signs in one user so user-scoped stores pick up their key. */
function seedSession(userId: string): void {
  window.localStorage.setItem(
    "gotham.auth.session",
    JSON.stringify({ user: { id: userId }, accessToken: "a", refreshToken: "r" }),
  );
}

/** makeTab builds one minimal open tab for the badge count. */
function makeTab(id: string): EditorTab {
  return {
    id,
    kind: "file",
    root: "volumes",
    path: `${id}.md`,
    name: `${id}.md`,
    doc: "hello",
    original: "hello",
    etag: '"1"',
    eol: "\n",
    bom: false,
    encoding: "utf-8",
    size: 5,
    mode: 0o100644,
    truncated: false,
    language: "markdown",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
  vi.restoreAllMocks();
});

describe("parseEditorWindow", () => {
  it("fills a missing payload with defaults", () => {
    expect(parseEditorWindow({ version: EDITOR_WINDOW_VERSION })).toEqual(
      DEFAULT_EDITOR_WINDOW,
    );
  });

  it("resets only the invalid fields", () => {
    const parsed = parseEditorWindow({
      version: EDITOR_WINDOW_VERSION,
      width: 200,
      height: "tall",
      x: -5,
      y: 40,
      minimized: "yes",
    });
    expect(parsed.width).toBe(DEFAULT_EDITOR_WINDOW.width);
    expect(parsed.height).toBe(DEFAULT_EDITOR_WINDOW.height);
    expect(parsed.x).toBe(DEFAULT_EDITOR_WINDOW.x);
    expect(parsed.y).toBe(40);
    expect(parsed.minimized).toBe(false);
  });

  it("falls back wholesale on a wrong version or a non-object", () => {
    expect(
      parseEditorWindow({ ...DEFAULT_EDITOR_WINDOW, version: 999 }),
    ).toEqual(DEFAULT_EDITOR_WINDOW);
    expect(parseEditorWindow(null)).toEqual(DEFAULT_EDITOR_WINDOW);
    expect(parseEditorWindow("geometry")).toEqual(DEFAULT_EDITOR_WINDOW);
  });
});

describe("editor window storage", () => {
  it("scopes the key per user and round-trips geometry", () => {
    const storage = memoryStorage();
    expect(editorWindowStorageKey("")).toBe("");
    const state = { width: 900, height: 600, x: 10, y: 20, minimized: true };
    expect(writeEditorWindow(storage, "u-1", state)).toBe(true);
    expect(readEditorWindow(storage, "u-1")).toEqual(state);
    // Another user never inherits the geometry.
    expect(readEditorWindow(storage, "u-2")).toEqual(DEFAULT_EDITOR_WINDOW);
  });

  it("survives corrupt or blocked storage", () => {
    const storage = memoryStorage();
    storage.setItem(editorWindowStorageKey("u-1"), "{not-json");
    expect(readEditorWindow(storage, "u-1")).toEqual(DEFAULT_EDITOR_WINDOW);
    const blocked = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    };
    expect(readEditorWindow(blocked, "u-1")).toEqual(DEFAULT_EDITOR_WINDOW);
    expect(writeEditorWindow(blocked, "u-1", DEFAULT_EDITOR_WINDOW)).toBe(false);
  });
});

describe("clampEditorSize", () => {
  it("floors at the minimum and caps at the viewport", () => {
    expect(clampEditorSize(100, 100, 1440, 900, 480, 320)).toEqual({
      width: 480,
      height: 320,
    });
    expect(clampEditorSize(2000, 1500, 1440, 900, 480, 320)).toEqual({
      width: 1440,
      height: 900,
    });
    expect(clampEditorSize(900, 600, 1440, 900, 480, 320)).toEqual({
      width: 900,
      height: 600,
    });
  });
});

describe("useEditorWindowStore", () => {
  it("persists size, position and minimized across store instances", () => {
    seedSession("u-1");
    const first = useEditorWindowStore();
    first.setSize(900, 600);
    first.setPosition(30, 40);
    first.setMinimized(true);
    // A fresh store instance (a reload) restores the persisted geometry.
    setActivePinia(createPinia());
    const second = useEditorWindowStore();
    expect(second.width).toBe(900);
    expect(second.height).toBe(600);
    expect(second.x).toBe(30);
    expect(second.y).toBe(40);
    expect(second.minimized).toBe(true);
  });

  it("clamps restored geometry back into a smaller viewport", () => {
    seedSession("u-1");
    const store = useEditorWindowStore();
    store.setSize(1331, 936);
    store.setPosition(1200, 800);
    store.clampToViewport(800, 600);
    expect(store.width).toBeLessThanOrEqual(800);
    expect(store.height).toBeLessThanOrEqual(600);
    expect(store.x).not.toBeNull();
    expect(store.y).not.toBeNull();
    expect(store.x! + store.width).toBeLessThanOrEqual(800);
    expect(store.y! + store.height).toBeLessThanOrEqual(600);
  });

  it("resets a dragged position on a narrow viewport", () => {
    seedSession("u-1");
    const store = useEditorWindowStore();
    store.setPosition(30, 40);
    store.clampToViewport(600, 800);
    expect(store.x).toBeNull();
    expect(store.y).toBeNull();
  });

  it("counts open editor tabs for the rail badge", () => {
    seedSession("u-1");
    const editor = useEditorStore();
    const windows = useEditorWindowStore();
    expect(windows.openCount).toBe(0);
    editor.tabs = [makeTab("tab-1"), makeTab("tab-2")];
    expect(windows.openCount).toBe(2);
    editor.closeAll();
    expect(windows.openCount).toBe(0);
  });
});
