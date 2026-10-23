
export type GotoLineError = "format" | "line" | "column";

export interface EditorLineInfo {
  line: number;
  total: number;
}

export interface GotoLineTarget {
  line: number;
  column: number;
}

export type GotoLineParse = { target: GotoLineTarget } | { error: GotoLineError };

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
