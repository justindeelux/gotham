// JUS-245: the floating terminal window persists its geometry per user and
// clamps it into the viewport, using the same shared math as the editor.
/* global window:readonly */
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  DEFAULT_TERMINAL_WINDOW,
  TERMINAL_WINDOW_VERSION,
  parseTerminalWindow,
} from "@/features/terminal/schemas/terminal";
import { useTerminalWindowStore } from "@/features/terminal/stores/terminalWindow";
import {
  TERMINAL_WINDOW_MAX_BYTES,
  readTerminalWindow,
  terminalWindowStorageKey,
  writeTerminalWindow,
} from "@/features/terminal/utils/terminalWindow";

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

/** seedSession signs in one user so the store picks up their key. */
function seedSession(userId: string): void {
  window.localStorage.setItem(
    "gotham.auth.session",
    JSON.stringify({ user: { id: userId }, accessToken: "a", refreshToken: "r" }),
  );
}

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
  vi.restoreAllMocks();
});

describe("parseTerminalWindow", () => {
  it("fills a missing payload with defaults", () => {
    expect(parseTerminalWindow({ version: TERMINAL_WINDOW_VERSION })).toEqual(
      DEFAULT_TERMINAL_WINDOW,
    );
  });

  it("resets only invalid fields", () => {
    const parsed = parseTerminalWindow({
      version: TERMINAL_WINDOW_VERSION,
      width: 200,
      height: "tall",
      x: -5,
      y: 40,
      minimized: "yes",
    });
    expect(parsed.width).toBe(DEFAULT_TERMINAL_WINDOW.width);
    expect(parsed.height).toBe(DEFAULT_TERMINAL_WINDOW.height);
    expect(parsed.x).toBe(DEFAULT_TERMINAL_WINDOW.x);
    expect(parsed.y).toBe(40);
    expect(parsed.minimized).toBe(false);
  });

  it("falls back wholesale on a wrong version or a non-object", () => {
    expect(parseTerminalWindow({ ...DEFAULT_TERMINAL_WINDOW, version: 9 })).toEqual(
      DEFAULT_TERMINAL_WINDOW,
    );
    expect(parseTerminalWindow(null)).toEqual(DEFAULT_TERMINAL_WINDOW);
    expect(parseTerminalWindow("geometry")).toEqual(DEFAULT_TERMINAL_WINDOW);
  });
});

describe("terminal window storage", () => {
  it("scopes the key per user and round-trips geometry", () => {
    const storage = memoryStorage();
    expect(terminalWindowStorageKey("")).toBe("");
    const state = { width: 900, height: 500, x: 10, y: 20, minimized: true };
    expect(writeTerminalWindow(storage, "u-1", state)).toBe(true);
    expect(readTerminalWindow(storage, "u-1")).toEqual(state);
    expect(readTerminalWindow(storage, "u-2")).toEqual(DEFAULT_TERMINAL_WINDOW);
  });

  it("ignores an oversized or corrupt payload", () => {
    const storage = memoryStorage();
    storage.setItem(terminalWindowStorageKey("u-1"), "x".repeat(TERMINAL_WINDOW_MAX_BYTES));
    expect(readTerminalWindow(storage, "u-1")).toEqual(DEFAULT_TERMINAL_WINDOW);
    storage.setItem(terminalWindowStorageKey("u-1"), "{not json");
    expect(readTerminalWindow(storage, "u-1")).toEqual(DEFAULT_TERMINAL_WINDOW);
  });
});

describe("terminal window store geometry", () => {
  it("fits the window inside 1280/1366 and a 390px phone", () => {
    seedSession("u-1");
    const store = useTerminalWindowStore();
    store.setSize(2000, 2000, 1280, 800);
    expect(store.width).toBe(1232);
    expect(store.height).toBe(696);

    store.setSize(2000, 2000, 1366, 800);
    expect(store.width).toBe(1318);

    store.setSize(2000, 2000, 390, 844);
    expect(store.width).toBe(342);
    // A phone still fits the chrome bound rather than shrinking to nothing.
    expect(store.height).toBe(740);
  });

  it("drops a dragged offset below the narrow breakpoint, display-only", () => {
    seedSession("u-1");
    const store = useTerminalWindowStore();
    store.setSize(700, 400, 1440, 900);
    store.setPosition(200, 120, 700, 400);
    const wide = store.displayGeometry(1440, 900);
    expect(wide.x).toBe(200);
    expect(wide.y).toBe(120);

    const narrow = store.displayGeometry(390, 844);
    expect(narrow.x).toBeNull();
    expect(narrow.y).toBeNull();
    // Display fitting never rewrites the saved position.
    expect(store.x).toBe(200);
  });

  it("rebinds per account and resets without persisting on logout", () => {
    window.localStorage.clear();
    seedSession("u-1");
    const store = useTerminalWindowStore();
    store.setSize(700, 400, 1440, 900);
    const first = { width: store.width, height: store.height };

    store.reloadForUser("u-2");
    expect(store.width).toBe(DEFAULT_TERMINAL_WINDOW.width);
    store.setSize(800, 500, 1440, 900);
    const second = { width: store.width, height: store.height };

    store.reloadForUser("u-1");
    expect({ width: store.width, height: store.height }).toEqual(first);
    store.reloadForUser("u-2");
    expect({ width: store.width, height: store.height }).toEqual(second);

    store.reset();
    expect(store.width).toBe(DEFAULT_TERMINAL_WINDOW.width);
    store.reloadForUser("u-2");
    expect({ width: store.width, height: store.height }).toEqual(second);
  });
});
