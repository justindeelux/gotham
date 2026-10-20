import type { ITheme } from "@xterm/xterm";

import {
  DEFAULT_TERMINAL_THEME,
  type TerminalThemeName,
} from "@/features/terminal/schemas/terminalAppearance";

/**
 * xterm theme mapping (JUS-250). Every color is read from a design token so
 * the named themes stay in the token contract instead of a parallel palette
 * file. A missing token is omitted from the ITheme rather than replaced with
 * a literal, so xterm's own fallback applies (server-render/test contexts have
 * no stylesheet loaded).
 */

/** TokenReader resolves one CSS custom property (undefined when absent). */
export type TokenReader = (_name: string) => string | undefined;

/** ANSI_TOKEN_FIELDS maps xterm's palette keys onto the token suffix. */
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

/** SCHEME_TOKEN_FIELDS maps the rest of the theme onto token suffixes. */
const SCHEME_TOKEN_FIELDS = [
  ["background", "bg"],
  ["foreground", "fg"],
  ["cursor", "cursor"],
  ["cursorAccent", "cursor-accent"],
  ["selectionBackground", "selection"],
  ["selectionForeground", "selection-fg"],
] as const satisfies ReadonlyArray<readonly [keyof ITheme, string]>;

/** terminalThemeTokenPrefix is the token group for one named theme. */
export function terminalThemeTokenPrefix(theme: TerminalThemeName): string {
  return theme === "gotham-light" ? "--term-light-" : "--term-dark-";
}

/**
 * terminalTheme builds xterm's ITheme for one named theme from the tokens.
 * The chrome around xterm keeps using the shell tokens, so only the renderer
 * changes.
 */
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

/**
 * readDesignToken reads one resolved custom property from the document root.
 * It is the default reader; tests inject a map so no stylesheet is needed.
 */
export function readDesignToken(name: string): string | undefined {
  if (typeof document === "undefined") {
    return undefined;
  }
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  return value === "" ? undefined : value;
}
