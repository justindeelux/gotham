import { z } from "zod";

/**
 * Terminal appearance (JUS-250): the named xterm theme chosen by one user in
 * this browser. A single field keeps the preference small and forward-safe;
 * the ANSI palette itself lives in design tokens (`--term-*` in tokens.css),
 * never in this schema.
 */
export const TERMINAL_APPEARANCE_VERSION = 1 as const;

/** TERMINAL_THEMES are the named themes offered by the appearance control. */
export const TERMINAL_THEMES = ["gotham-dark", "gotham-light"] as const;
export type TerminalThemeName = (typeof TERMINAL_THEMES)[number];

/** DEFAULT_TERMINAL_THEME keeps the original dark renderer as the factory set. */
export const DEFAULT_TERMINAL_THEME: TerminalThemeName = "gotham-dark";

/** terminalAppearanceSchema validates the preference; a bad value resets it. */
export const terminalAppearanceSchema = z.object({
  theme: z.enum(TERMINAL_THEMES).catch(DEFAULT_TERMINAL_THEME),
});
export type TerminalAppearance = z.infer<typeof terminalAppearanceSchema>;

/** DEFAULT_TERMINAL_APPEARANCE is the factory preference set. */
export const DEFAULT_TERMINAL_APPEARANCE: TerminalAppearance = {
  theme: DEFAULT_TERMINAL_THEME,
};

/** terminalAppearanceSnapshotSchema is the versioned shape written to storage. */
export const terminalAppearanceSnapshotSchema = terminalAppearanceSchema.extend({
  version: z.literal(TERMINAL_APPEARANCE_VERSION),
});

/** isTerminalThemeName narrows an untrusted string to a named theme. */
export function isTerminalThemeName(value: unknown): value is TerminalThemeName {
  return (
    typeof value === "string" &&
    (TERMINAL_THEMES as readonly string[]).includes(value)
  );
}

/**
 * parseTerminalAppearance resolves untrusted stored data into a complete
 * preference. A wrong version or a non-object payload is treated as unreadable
 * (defaults); a bad theme value resets only that field.
 */
export function parseTerminalAppearance(data: unknown): TerminalAppearance {
  if (typeof data !== "object" || data === null) {
    return DEFAULT_TERMINAL_APPEARANCE;
  }
  if (
    (data as { version?: unknown }).version !== TERMINAL_APPEARANCE_VERSION
  ) {
    return DEFAULT_TERMINAL_APPEARANCE;
  }
  const parsed = terminalAppearanceSchema.safeParse(data);
  return parsed.success ? parsed.data : DEFAULT_TERMINAL_APPEARANCE;
}
