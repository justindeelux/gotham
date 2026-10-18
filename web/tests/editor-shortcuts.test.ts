// Shortcut sheet contract (JUS-228): every row the sheet lists must be a real
// binding in the editor's keymap or a declared CodeMirror/browser default, and
// every row must have an EN + VI label. The sheet and keymap share one table,
// so this guards drift.
import { defaultKeymap, historyKeymap } from "@codemirror/commands";
import { foldKeymap } from "@codemirror/language";
import { searchKeymap } from "@codemirror/search";
import { describe, expect, it, vi } from "vitest";

import {
  EDITOR_SHORTCUT_GROUPS,
  NATIVE_SHORTCUT_KEYS,
  editorCommandKeymap,
  editorShortcuts,
  isMacPlatform,
  shortcutKeysFor,
  shortcutLabel,
} from "@/features/files/editor/shortcuts";
import editorEn from "@/features/files/locales/editorEn";
import editorVi from "@/features/files/locales/editorVi";

interface KeyBindingLike {
  key?: string;
  mac?: string;
  shift?: unknown;
}

const builtinBindings: KeyBindingLike[] = [
  ...defaultKeymap,
  ...historyKeymap,
  ...searchKeymap,
  ...foldKeymap,
];

/** customKeymap builds the command bindings with no-op handlers. */
function customKeymap() {
  const noop = vi.fn();
  return editorCommandKeymap({
    save: noop,
    saveAll: noop,
    closeTab: noop,
    nextTab: noop,
    previousTab: noop,
    find: noop,
    replace: noop,
    goToLine: noop,
    toggleWrap: noop,
  });
}

/** isBound reports whether a key string is bound on either platform. */
function isBound(
  key: string,
  custom: ReturnType<typeof editorCommandKeymap>,
): boolean {
  if (custom.some((binding) => binding.key === key || binding.mac === key)) {
    return true;
  }
  return builtinBindings.some((binding) => {
    if (binding.key === key || binding.mac === key) {
      return true;
    }
    if (!key.startsWith("Shift-")) {
      return false;
    }
    const base = key.slice("Shift-".length);
    return Boolean(binding.shift) && (binding.key === base || binding.mac === base);
  });
}

/** messageAt resolves a dotted message key on a locale catalog. */
function messageAt(catalog: unknown, path: string): unknown {
  return path
    .split(".")
    .reduce<unknown>(
      (node, part) =>
        node && typeof node === "object"
          ? (node as Record<string, unknown>)[part]
          : undefined,
      catalog,
    );
}

describe("editor shortcut table", () => {
  it("binds every shortcut the sheet lists", () => {
    const custom = customKeymap();
    for (const shortcut of editorShortcuts) {
      for (const key of shortcutKeysFor(shortcut, "other")) {
        if (shortcut.source === "native") {
          expect(NATIVE_SHORTCUT_KEYS, `${shortcut.id} ${key}`).toContain(key);
          continue;
        }
        expect(isBound(key, custom), `${shortcut.id} is not bound to ${key}`).toBe(true);
      }
      for (const key of shortcutKeysFor(shortcut, "mac")) {
        if (shortcut.source === "native") {
          expect(NATIVE_SHORTCUT_KEYS, `${shortcut.id} ${key}`).toContain(key);
          continue;
        }
        expect(isBound(key, custom), `${shortcut.id} is not bound to ${key}`).toBe(true);
      }
    }
  });

  it("builds one real binding per command key", () => {
    const custom = customKeymap();
    for (const shortcut of editorShortcuts) {
      if (shortcut.source !== "command") {
        continue;
      }
      for (const key of shortcut.keys) {
        expect(custom.some((binding) => binding.key === key)).toBe(true);
      }
    }
  });

  it("has a unique key per binding and no empty group", () => {
    const seen = new Set<string>();
    for (const shortcut of editorShortcuts) {
      for (const key of [...shortcut.keys, ...(shortcut.macKeys ?? [])]) {
        expect(seen.has(key), `duplicate binding ${key}`).toBe(false);
        seen.add(key);
      }
    }
    for (const group of EDITOR_SHORTCUT_GROUPS) {
      expect(editorShortcuts.some((shortcut) => shortcut.group === group)).toBe(true);
    }
  });

  it("labels every row and group in English and Vietnamese", () => {
    for (const catalog of [editorEn, editorVi]) {
      for (const group of EDITOR_SHORTCUT_GROUPS) {
        expect(messageAt(catalog, `shortcuts.groups.${group}`)).toBeTypeOf("string");
      }
      for (const shortcut of editorShortcuts) {
        const key = shortcut.label.replace(/^files\.editor\./, "");
        expect(messageAt(catalog, key), key).toBeTypeOf("string");
      }
    }
  });
});

describe("shortcutLabel", () => {
  it("maps Mod and Alt per platform", () => {
    expect(shortcutLabel("Mod-Shift-s", "mac")).toBe("Cmd+Shift+S");
    expect(shortcutLabel("Mod-Shift-s", "other")).toBe("Ctrl+Shift+S");
    expect(shortcutLabel("Alt-w", "mac")).toBe("Option+W");
    expect(shortcutLabel("Alt-w", "other")).toBe("Alt+W");
    expect(shortcutLabel("Mod-Alt-z", "mac")).toBe("Cmd+Option+Z");
  });

  it("keeps a literal Ctrl and shortens named keys", () => {
    expect(shortcutLabel("Ctrl-Tab", "mac")).toBe("Ctrl+Tab");
    expect(shortcutLabel("Shift-ArrowLeft", "other")).toBe("Shift+←");
    expect(shortcutLabel("PageDown", "other")).toBe("PgDn");
    expect(shortcutLabel("Escape", "mac")).toBe("Esc");
  });

  it("detects the platform from navigator", () => {
    vi.stubGlobal("navigator", { platform: "MacIntel", userAgent: "" });
    expect(isMacPlatform()).toBe(true);
    vi.stubGlobal("navigator", { platform: "Linux x86_64", userAgent: "" });
    expect(isMacPlatform()).toBe(false);
    vi.unstubAllGlobals();
  });
});
