/* global window:readonly */
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import { useAuthStore } from "../src/features/auth/stores/auth";
import { useEditorStore } from "../src/features/files/stores/editor";
import type { EditorTab } from "../src/features/files/stores/editor";
import { useEditorWindowStore } from "../src/features/files/stores/editorWindow";
import { setSession } from "../src/shared/api/token";
import type { User } from "../src/shared/api/token";

function account(id: string): User {
  return {
    id,
    email: `${id}@gotham.dev`,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function makeTab(id: string): EditorTab {
  return {
    id,
    kind: "file",
    root: "volumes",
    path: `${id}.md`,
    name: `${id}.md`,
    doc: "changed",
    original: "start",
    etag: '"1"',
    eol: "\n",
    bom: false,
    encoding: "utf-8",
    size: 7,
    mode: 0o100644,
    truncated: false,
    language: "markdown",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

function signIn(id: string): void {
  setSession({ user: account(id), accessToken: "a", refreshToken: "r" });
}

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
});

describe("auth session changes reset the editor (JUS-232)", () => {
  it("clears tabs and window geometry on logout", () => {
    signIn("u-1");
    const auth = useAuthStore();
    const editor = useEditorStore();
    const windows = useEditorWindowStore();
    editor.tabs = [makeTab("t1")];
    editor.activeId = "t1";
    windows.setSize(900, 600);
    windows.setMinimized(true);

    auth.clearSession();

    expect(editor.tabs).toHaveLength(0);
    expect(editor.hasDirty).toBe(false);
    expect(windows.width).toBe(1331);
    expect(windows.minimized).toBe(false);
  });

  it("loads the new account's geometry on an in-SPA account change", () => {
    signIn("u-1");
    useAuthStore();
    const windows = useEditorWindowStore();
    windows.setSize(900, 600);

    signIn("u-2");
    expect(windows.width).toBe(1331);
    expect(windows.minimized).toBe(false);

    windows.setSize(700, 500);
    signIn("u-1");
    expect(windows.width).toBe(900);
  });
});
