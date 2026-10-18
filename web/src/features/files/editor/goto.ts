/**
 * Go-to-line parsing for the custom popover (JUS-231). The line range is
 * checked here against the document's line count; the column upper bound
 * depends on the target line, so the editor checks it when it jumps.
 */

/** GotoLineError names the rejected dimension, for an inline message. */
export type GotoLineError = "format" | "line" | "column";

/** EditorLineInfo is the cursor line and the document's line count. */
export interface EditorLineInfo {
  line: number;
  total: number;
}

/** GotoLineTarget is a validated 1-based position. */
export interface GotoLineTarget {
  line: number;
  column: number;
}

/** GotoLineParse is either a validated target or the rejected dimension. */
export type GotoLineParse = { target: GotoLineTarget } | { error: GotoLineError };

/**
 * parseGotoLine accepts `line` or `line:column`. Zero, a line past the end and
 * anything non-numeric is rejected instead of clamped, so a typo never jumps
 * somewhere the user did not ask for.
 */
export function parseGotoLine(input: string, totalLines: number): GotoLineParse {
  const match = /^\s*(\d+)(?::(\d+))?\s*$/.exec(input);
  if (!match) {
    return { error: "format" };
  }
  const line = Number(match[1]);
  const column = match[2] === undefined ? 1 : Number(match[2]);
  if (!Number.isSafeInteger(line) || line < 1 || line > totalLines) {
    return { error: "line" };
  }
  if (!Number.isSafeInteger(column) || column < 1) {
    return { error: "column" };
  }
  return { target: { line, column } };
}
