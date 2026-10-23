
export type ShortcutGroup = "common" | "cursor" | "selection" | "edit";

export const EDITOR_SHORTCUT_GROUPS: readonly ShortcutGroup[] = [
  "common",
  "cursor",
  "selection",
  "edit",
];

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
  label: string;
  keys: readonly string[];
  macKeys?: readonly string[];
}

export interface CommandShortcut extends ShortcutBase {
  id: EditorCommandId;
  source: "command";
}

export interface BuiltinShortcut extends ShortcutBase {
  source: "default" | "native";
}

export type EditorShortcut = CommandShortcut | BuiltinShortcut;

export const NATIVE_SHORTCUT_KEYS: readonly string[] = [
  "Mod-x",
  "Mod-c",
  "Mod-v",
  "Escape",
];

export const editorShortcuts: readonly EditorShortcut[] = [
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
    keys: ["Mod-Alt-PageDown"],
    source: "command",
  },
  {
    id: "previousTab",
    group: "common",
    label: "files.editor.shortcuts.previousTab",
    keys: ["Mod-Alt-PageUp"],
    source: "command",
  },
  { id: "find", group: "common", label: "files.editor.shortcuts.find", keys: ["Mod-f"], source: "command" },
  {
    id: "replace",
    group: "common",
    label: "files.editor.shortcuts.replace",
    keys: ["Mod-h"],
    macKeys: ["Mod-Alt-f"],
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

export interface EditorKeyBinding {
  key: string;
  mac?: string;
  preventDefault: boolean;
  run: () => boolean;
}

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

export type ShortcutPlatform = "mac" | "other";

export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") {
    return false;
  }
  const platform = navigator.platform ?? "";
  const userAgent = navigator.userAgent ?? "";
  return /Mac|iPhone|iPad|iPod/.test(platform) || /Mac OS X/.test(userAgent);
}

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

function keyName(token: string): string {
  const named = KEY_NAMES[token];
  if (named) {
    return named;
  }
  return token.length === 1 ? token.toUpperCase() : token;
}

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

export function shortcutLabel(key: string, platform: ShortcutPlatform): string {
  const parts = key.split("-");
  const last = parts.pop() ?? key;
  return [...parts.map((part) => modifierName(part, platform)), keyName(last)].join("+");
}
