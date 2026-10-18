// Editor settings (JUS-219): the stored schema must accept good data and
// degrade one bad field at a time; the storage helpers must survive blocked or
// corrupt localStorage; and a live CodeMirror session must reconfigure through
// its settings compartment without being recreated.
/* global document:readonly */
import { indentUnit } from "@codemirror/language";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { afterEach, describe, expect, it } from "vitest";

import {
  createEditorSession,
  editorSettingsExtensions,
} from "@/features/files/editor/codemirror";
import {
  DEFAULT_EDITOR_SETTINGS,
  EDITOR_SETTINGS_VERSION,
  parseEditorSettings,
} from "@/features/files/schemas/editorSettings";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import {
  editorSettingsStorageKey,
  readEditorSettings,
  writeEditorSettings,
} from "@/features/files/utils/editorSettings";

/** settingsState builds a state with just the reconfigurable extensions. */
function settingsState(settings: EditorSettings): EditorState {
  return EditorState.create({ extensions: [editorSettingsExtensions(settings)] });
}

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

describe("parseEditorSettings", () => {
  it("fills a missing payload with defaults", () => {
    expect(parseEditorSettings({ version: EDITOR_SETTINGS_VERSION })).toEqual(
      DEFAULT_EDITOR_SETTINGS,
    );
  });

  it("resets only the invalid fields", () => {
    const parsed = parseEditorSettings({
      version: EDITOR_SETTINGS_VERSION,
      ...DEFAULT_EDITOR_SETTINGS,
      theme: "neon",
      fontSize: 999,
      lineNumbers: "yes",
      tabSize: 3,
      wrap: true,
    });
    expect(parsed.theme).toBe(DEFAULT_EDITOR_SETTINGS.theme);
    expect(parsed.fontSize).toBe(DEFAULT_EDITOR_SETTINGS.fontSize);
    expect(parsed.lineNumbers).toBe(DEFAULT_EDITOR_SETTINGS.lineNumbers);
    expect(parsed.tabSize).toBe(DEFAULT_EDITOR_SETTINGS.tabSize);
    expect(parsed.wrap).toBe(true);
  });

  it("strips unknown keys", () => {
    const parsed = parseEditorSettings({
      version: EDITOR_SETTINGS_VERSION,
      ...DEFAULT_EDITOR_SETTINGS,
      futureOption: "ignored",
    });
    expect(parsed).toEqual(DEFAULT_EDITOR_SETTINGS);
  });

  it("falls back wholesale on a wrong version or a non-object", () => {
    expect(
      parseEditorSettings({
        ...DEFAULT_EDITOR_SETTINGS,
        version: EDITOR_SETTINGS_VERSION + 1,
        theme: "light",
      }),
    ).toEqual(DEFAULT_EDITOR_SETTINGS);
    expect(parseEditorSettings("nope")).toEqual(DEFAULT_EDITOR_SETTINGS);
    expect(parseEditorSettings(null)).toEqual(DEFAULT_EDITOR_SETTINGS);
  });
});

describe("editor settings storage", () => {
  it("round-trips per user and never leaks across users", () => {
    const storage = memoryStorage();
    const custom: EditorSettings = {
      ...DEFAULT_EDITOR_SETTINGS,
      theme: "light",
      tabSize: 2,
      showInvisibles: true,
    };
    expect(writeEditorSettings(storage, "user-a", custom)).toBe(true);
    expect(readEditorSettings(storage, "user-a")).toEqual(custom);
    expect(readEditorSettings(storage, "user-b")).toEqual(DEFAULT_EDITOR_SETTINGS);
  });

  it("reads corrupt or oversized payloads as defaults", () => {
    const storage = memoryStorage();
    const key = editorSettingsStorageKey("user-a");
    storage.setItem(key, "{not json");
    expect(readEditorSettings(storage, "user-a")).toEqual(DEFAULT_EDITOR_SETTINGS);

    storage.setItem(key, JSON.stringify({ version: EDITOR_SETTINGS_VERSION, pad: "x".repeat(5000) }));
    expect(readEditorSettings(storage, "user-a")).toEqual(DEFAULT_EDITOR_SETTINGS);
  });

  it("skips storage when no user is signed in", () => {
    const storage = memoryStorage();
    expect(editorSettingsStorageKey("")).toBe("");
    expect(writeEditorSettings(storage, "", DEFAULT_EDITOR_SETTINGS)).toBe(false);
    expect(readEditorSettings(storage, "")).toEqual(DEFAULT_EDITOR_SETTINGS);
  });
});

describe("editor settings extensions", () => {
  it("applies tab size and indentation unit", () => {
    expect(settingsState({ ...DEFAULT_EDITOR_SETTINGS, tabSize: 2 }).facet(EditorState.tabSize)).toBe(2);
    expect(
      settingsState({ ...DEFAULT_EDITOR_SETTINGS, tabSize: 2 }).facet(indentUnit),
    ).toBe("  ");
    expect(
      settingsState({
        ...DEFAULT_EDITOR_SETTINGS,
        tabSize: 8,
        insertSpaces: false,
      }).facet(indentUnit),
    ).toBe("\t");
  });

  it("flips the dark facet with the theme", () => {
    expect(settingsState(DEFAULT_EDITOR_SETTINGS).facet(EditorView.darkTheme)).toBe(true);
    expect(
      settingsState({ ...DEFAULT_EDITOR_SETTINGS, theme: "light" }).facet(
        EditorView.darkTheme,
      ),
    ).toBe(false);
  });
});

describe("live session reconfiguration", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("reconfigures gutters and wrap without recreating the view", async () => {
    const host = document.createElement("div");
    document.body.append(host);
    const session = await createEditorSession({
      parent: host,
      doc: "  const value = 1;",
      language: "plain",
      readOnly: false,
      settings: DEFAULT_EDITOR_SETTINGS,
      onChange: () => undefined,
      onCursor: () => undefined,
      onSave: () => undefined,
    });

    expect(host.querySelector(".cm-lineNumbers")).not.toBeNull();
    expect(host.querySelector(".cm-foldGutter")).not.toBeNull();
    expect(host.querySelector(".cm-content")?.classList.contains("cm-lineWrapping")).toBe(false);

    session.setSettings({
      ...DEFAULT_EDITOR_SETTINGS,
      lineNumbers: false,
      foldGutter: false,
      wrap: true,
      indentGuides: false,
    });

    expect(host.querySelector(".cm-lineNumbers")).toBeNull();
    expect(host.querySelector(".cm-foldGutter")).toBeNull();
    expect(host.querySelector(".cm-content")?.classList.contains("cm-lineWrapping")).toBe(true);
    session.destroy();
  });
});
