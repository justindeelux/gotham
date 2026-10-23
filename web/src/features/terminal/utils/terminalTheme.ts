import type { ITheme } from "@xterm/xterm";

import {
  DEFAULT_TERMINAL_THEME,
  type TerminalThemeName,
} from "@/features/terminal/schemas/terminalAppearance";

export type TokenReader = (_name: string) => string | undefined;

const ANSI_TOKEN_FIELDS = [
  ["black", "ansi-black"],
  ["red", "ansi-red"],
  ["green", "ansi-green"],
  ["yellow", "ansi-yellow"],
  ["blue", "ansi-blue"],
  ["magenta", "ansi-magenta"],
  ["cyan", "ansi-cyan"],
  ["white", "ansi-white"],
  ["brightBlack", "ansi-bright-black"],
  ["brightRed", "ansi-bright-red"],
  ["brightGreen", "ansi-bright-green"],
  ["brightYellow", "ansi-bright-yellow"],
  ["brightBlue", "ansi-bright-blue"],
  ["brightMagenta", "ansi-bright-magenta"],
  ["brightCyan", "ansi-bright-cyan"],
  ["brightWhite", "ansi-bright-white"],
] as const satisfies ReadonlyArray<readonly [keyof ITheme, string]>;

const SCHEME_TOKEN_FIELDS = [
  ["background", "bg"],
  ["foreground", "fg"],
  ["cursor", "cursor"],
  ["cursorAccent", "cursor-accent"],
  ["selectionBackground", "selection"],
  ["selectionForeground", "selection-fg"],
] as const satisfies ReadonlyArray<readonly [keyof ITheme, string]>;

export function terminalThemeTokenPrefix(theme: TerminalThemeName): string {
  return theme === "gotham-light" ? "--term-light-" : "--term-dark-";
}

export function terminalTheme(
  theme: TerminalThemeName = DEFAULT_TERMINAL_THEME,
  readToken: TokenReader = readDesignToken,
): ITheme {
  const prefix = terminalThemeTokenPrefix(theme);
  const resolved: Record<string, string> = {};
  for (const [field, suffix] of [
    ...SCHEME_TOKEN_FIELDS,
    ...ANSI_TOKEN_FIELDS,
  ]) {
    const value = readToken(`${prefix}${suffix}`);
    if (value !== undefined && value !== "") {
      resolved[field] = value;
    }
  }
  return resolved as ITheme;
}

export function readDesignToken(name: string): string | undefined {
  if (typeof document === "undefined") {
    return undefined;
  }
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  return value === "" ? undefined : value;
}
