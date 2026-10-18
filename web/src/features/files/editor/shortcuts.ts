/**
 * One binding table for the file editor's keyboard shortcuts. The shortcut
 * sheet renders it, and the CodeMirror keymap is built from the same rows, so
 * a documented key cannot drift from the bound one (JUS-228).
 *
 * Rows carry their provenance:
 * - `command`: this module builds the actual CodeMirror binding.
 * - `default`: a CodeMirror built-in keymap already binds it.
 * - `native`: the browser handles it; the editor deliberately adds nothing.
 *
 * Keys use CodeMirror's syntax (`Mod-s`, `Shift-ArrowLeft`); `Mod` is Cmd on
 * macOS and Ctrl elsewhere, and the sheet labels it per platform.
 */

/** ShortcutGroup is the sheet's grouping, mirroring the aaPanel inventory. */
export type ShortcutGroup = "common" | "cursor" | "selection" | "edit";

/** EDITOR_SHORTCUT_GROUPS is the sheet's display order. */
export const EDITOR_SHORTCUT_GROUPS: readonly ShortcutGroup[] = [
  "common",
  "cursor",
  "selection",
  "edit",
];

/** EditorCommandId names the bindings the editor itself implements. */
export type EditorCommandId =
  | "save"
  | "saveAll"
  | "closeTab"
  | "nextTab"
  | "previousTab"
  | "find"
  | "replace"
  | "goToLine"
  | "toggleWrap";

interface ShortcutBase {
  id: string;
  group: ShortcutGroup;
  /** label is the `files.editor.shortcuts.*` message key. */
  label: string;
  keys: readonly string[];
  /** macKeys overrides keys on macOS when CodeMirror's binding differs. */
  macKeys?: readonly string[];
}

/** CommandShortcut is a row this module turns into a real binding. */
export interface CommandShortcut extends ShortcutBase {
  id: EditorCommandId;
  source: "command";
}

/** BuiltinShortcut is a row covered by CodeMirror or the browser. */
export interface BuiltinShortcut extends ShortcutBase {
  source: "default" | "native";
}

export type EditorShortcut = CommandShortcut | BuiltinShortcut;

/**
 * NATIVE_SHORTCUT_KEYS are the browser-owned keys the sheet lists. They are
 * asserted by the unit test so a typo cannot advertise an unbound key.
 */
export const NATIVE_SHORTCUT_KEYS: readonly string[] = [
  "Mod-x",
  "Mod-c",
  "Mod-v",
  "Escape",
];

/** editorShortcuts is the single source of truth for the sheet and keymap. */
export const editorShortcuts: readonly EditorShortcut[] = [
  // Common
  { id: "save", group: "common", label: "files.editor.shortcuts.save", keys: ["Mod-s"], source: "command" },
  {
    id: "saveAll",
    group: "common",
    label: "files.editor.shortcuts.saveAll",
    keys: ["Mod-Shift-s"],
    source: "command",
  },
  {
    id: "closeTab",
    group: "common",
    label: "files.editor.shortcuts.closeTab",
    keys: ["Alt-w"],
    source: "command",
  },
  {
    id: "nextTab",
    group: "common",
    label: "files.editor.shortcuts.nextTab",
    keys: ["Ctrl-Tab"],
    source: "command",
  },
  {
    id: "previousTab",
    group: "common",
    label: "files.editor.shortcuts.previousTab",
    keys: ["Ctrl-Shift-Tab"],
    source: "command",
  },
  { id: "find", group: "common", label: "files.editor.shortcuts.find", keys: ["Mod-f"], source: "default" },
  {
    id: "replace",
    group: "common",
    label: "files.editor.shortcuts.replace",
    keys: ["Mod-h"],
    source: "command",
  },
  {
    id: "goToLine",
    group: "common",
    label: "files.editor.shortcuts.goToLine",
    keys: ["Mod-l"],
    source: "command",
  },
  {
    id: "toggleWrap",
    group: "common",
    label: "files.editor.shortcuts.toggleWrap",
    keys: ["Mod-Alt-z"],
    source: "command",
  },
  {
    id: "dismiss",
    group: "common",
    label: "files.editor.shortcuts.dismiss",
    keys: ["Escape"],
    source: "native",
  },

  // Cursor
  {
    id: "cursorChar",
    group: "cursor",
    label: "files.editor.shortcuts.cursorChar",
    keys: ["ArrowLeft", "ArrowRight"],
    source: "default",
  },
  {
    id: "cursorLine",
    group: "cursor",
    label: "files.editor.shortcuts.cursorLine",
    keys: ["ArrowUp", "ArrowDown"],
    source: "default",
  },
  {
    id: "cursorPage",
    group: "cursor",
    label: "files.editor.shortcuts.cursorPage",
    keys: ["PageUp", "PageDown"],
    source: "default",
  },
  {
    id: "lineStart",
    group: "cursor",
    label: "files.editor.shortcuts.lineStart",
    keys: ["Home", "End"],
    source: "default",
  },
  {
    id: "docStart",
    group: "cursor",
    label: "files.editor.shortcuts.docStart",
    keys: ["Mod-Home", "Mod-End"],
    source: "default",
  },

  // Selection
  {
    id: "selectAll",
    group: "selection",
    label: "files.editor.shortcuts.selectAll",
    keys: ["Mod-a"],
    source: "default",
  },
  {
    id: "selectChar",
    group: "selection",
    label: "files.editor.shortcuts.selectChar",
    keys: ["Shift-ArrowLeft", "Shift-ArrowRight", "Shift-ArrowUp", "Shift-ArrowDown"],
    source: "default",
  },
  {
    id: "selectLine",
    group: "selection",
    label: "files.editor.shortcuts.selectLine",
    keys: ["Alt-l"],
    macKeys: ["Ctrl-l"],
    source: "default",
  },
  {
    id: "selectNext",
    group: "selection",
    label: "files.editor.shortcuts.selectNext",
    keys: ["Mod-d"],
    source: "default",
  },

  // Edit
  { id: "undo", group: "edit", label: "files.editor.shortcuts.undo", keys: ["Mod-z"], source: "default" },
  {
    id: "redo",
    group: "edit",
    label: "files.editor.shortcuts.redo",
    keys: ["Mod-y"],
    macKeys: ["Mod-Shift-z"],
    source: "default",
  },
  { id: "cut", group: "edit", label: "files.editor.shortcuts.cut", keys: ["Mod-x"], source: "native" },
  { id: "copy", group: "edit", label: "files.editor.shortcuts.copy", keys: ["Mod-c"], source: "native" },
  { id: "paste", group: "edit", label: "files.editor.shortcuts.paste", keys: ["Mod-v"], source: "native" },
  {
    id: "deleteLine",
    group: "edit",
    label: "files.editor.shortcuts.deleteLine",
    keys: ["Shift-Mod-k"],
    source: "default",
  },
  {
    id: "toggleComment",
    group: "edit",
    label: "files.editor.shortcuts.toggleComment",
    keys: ["Mod-/"],
    source: "default",
  },
  {
    id: "moveLine",
    group: "edit",
    label: "files.editor.shortcuts.moveLine",
    keys: ["Alt-ArrowUp", "Alt-ArrowDown"],
    source: "default",
  },
];

/** EditorKeyBinding is the CodeMirror-compatible shape built from the table. */
export interface EditorKeyBinding {
  key: string;
  mac?: string;
  preventDefault: boolean;
  run: () => boolean;
}

/**
 * editorCommandKeymap turns every `command` row into its CodeMirror binding.
 * Bindings are returned in table order so a row that overrides a default keymap
 * entry (go-to-line on `Mod-l`) wins by being installed first.
 */
export function editorCommandKeymap(
  handlers: Record<EditorCommandId, () => void>,
): EditorKeyBinding[] {
  return editorShortcuts.flatMap((shortcut) => {
    if (shortcut.source !== "command") {
      return [];
    }
    const run = handlers[shortcut.id];
    return shortcut.keys.map((key, index) => ({
      key,
      mac: shortcut.macKeys?.[index],
      preventDefault: true,
      run: () => {
        run();
        return true;
      },
    }));
  });
}

/** ShortcutPlatform is the label flavour: macOS says Cmd/Option. */
export type ShortcutPlatform = "mac" | "other";

/** isMacPlatform reports whether the editor runs on a Mac-like platform. */
export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") {
    return false;
  }
  const platform = navigator.platform ?? "";
  const userAgent = navigator.userAgent ?? "";
  return /Mac|iPhone|iPad|iPod/.test(platform) || /Mac OS X/.test(userAgent);
}

/** shortcutKeysFor returns the keys to show/bind on one platform. */
export function shortcutKeysFor(
  shortcut: EditorShortcut,
  platform: ShortcutPlatform,
): readonly string[] {
  return platform === "mac" && shortcut.macKeys ? shortcut.macKeys : shortcut.keys;
}

const KEY_NAMES: Record<string, string> = {
  ArrowLeft: "←",
  ArrowRight: "→",
  ArrowUp: "↑",
  ArrowDown: "↓",
  PageUp: "PgUp",
  PageDown: "PgDn",
  Escape: "Esc",
};

/** keyName shortens a key token for display. */
function keyName(token: string): string {
  const named = KEY_NAMES[token];
  if (named) {
    return named;
  }
  return token.length === 1 ? token.toUpperCase() : token;
}

/** modifierName renders one modifier token for the platform. */
function modifierName(token: string, platform: ShortcutPlatform): string {
  if (token === "Mod") {
    return platform === "mac" ? "Cmd" : "Ctrl";
  }
  if (token === "Alt") {
    return platform === "mac" ? "Option" : "Alt";
  }
  if (token === "Meta") {
    return platform === "mac" ? "Cmd" : "Meta";
  }
  return token;
}

/** shortcutLabel renders a CodeMirror key string for the sheet. */
export function shortcutLabel(key: string, platform: ShortcutPlatform): string {
  const parts = key.split("-");
  const last = parts.pop() ?? key;
  return [...parts.map((part) => modifierName(part, platform)), keyName(last)].join("+");
}
