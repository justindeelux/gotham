import { z } from "zod";

export const TERMINAL_APPEARANCE_VERSION = 1 as const;

export const TERMINAL_THEMES = ["gotham-dark", "gotham-light"] as const;
export type TerminalThemeName = (typeof TERMINAL_THEMES)[number];

export const DEFAULT_TERMINAL_THEME: TerminalThemeName = "gotham-dark";

export const terminalAppearanceSchema = z.object({
  theme: z.enum(TERMINAL_THEMES).catch(DEFAULT_TERMINAL_THEME),
});
export type TerminalAppearance = z.infer<typeof terminalAppearanceSchema>;

export const DEFAULT_TERMINAL_APPEARANCE: TerminalAppearance = {
  theme: DEFAULT_TERMINAL_THEME,
};

export const terminalAppearanceSnapshotSchema = terminalAppearanceSchema.extend({
  version: z.literal(TERMINAL_APPEARANCE_VERSION),
});

export function isTerminalThemeName(value: unknown): value is TerminalThemeName {
  return (
    typeof value === "string" &&
    (TERMINAL_THEMES as readonly string[]).includes(value)
  );
}

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
