// JUS-232 fix round 1 (finding 4): logout and account changes must reset the
// editor tabs and the window geometry, and a new account must load its own
// persisted geometry instead of inheriting the previous one.
/* global window:readonly */
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import { useAuthStore } from "../src/features/auth/stores/auth";
import { useEditorStore } from "../src/features/files/stores/editor";
import type { EditorTab } from "../src/features/files/stores/editor";
import { useEditorWindowStore } from "../src/features/files/stores/editorWindow";
import { setSession } from "../src/shared/api/token";
import type { User } from "../src/shared/api/token";

/** account builds one minimal signed-in user. */
function account(id: string): User {
  return {
    id,
    email: `${id}@gotham.dev`,
    created_at: "2026-01-01T00:00:00Z",
  };
}

/** makeTab builds one minimal open tab. */
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

/** signIn installs one account's session (which the auth listener observes). */
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
    // The auth listener (registered on first store use) drives the reloads.
    useAuthStore();
    const windows = useEditorWindowStore();
    windows.setSize(900, 600);

    signIn("u-2");
    // u-2 never saved geometry: factory defaults, never u-1's size.
    expect(windows.width).toBe(1331);
    expect(windows.minimized).toBe(false);

    windows.setSize(700, 500);
    signIn("u-1");
    // u-1's own saved size is restored (900), not the default and not u-2's.
    expect(windows.width).toBe(900);
  });
});
