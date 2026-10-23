
export type SearchPatternError = "syntax" | "complexity";

const QUANTIFIER = /^\{(\d*)(?:,(\d*))?\}/;

function quantifierAt(
  pattern: string,
  at: number,
): { unbounded: boolean; length: number } | null {
  const char = pattern[at];
  if (char === "+" || char === "*") {
    return { unbounded: true, length: 1 };
  }
  if (char !== "{") {
    return null;
  }
  const match = QUANTIFIER.exec(pattern.slice(at));
  if (!match) {
    return null;
  }
  return { unbounded: match[2] === "", length: match[0].length };
}

export function hasNestedQuantifier(pattern: string): boolean {
  const groups: boolean[] = [false];
  let escaped = false;
  let inClass = false;
  for (let index = 0; index < pattern.length; index += 1) {
    const char = pattern[index]!;
    const top = groups.length - 1;
    if (escaped) {
      escaped = false;
      groups[top] = false;
      continue;
    }
    if (char === "\\") {
      escaped = true;
      groups[top] = false;
      continue;
    }
    if (inClass) {
      if (char === "]") {
        inClass = false;
      }
      continue;
    }
    if (char === "[") {
      inClass = true;
      groups[top] = false;
      continue;
    }
    if (char === "(") {
      groups.push(false);
      continue;
    }
    if (char === ")") {
      if (groups.length === 1) {
        groups[0] = false;
        continue;
      }
      const inner = groups.pop() ?? false;
      const quantifier = quantifierAt(pattern, index + 1);
      if (inner && quantifier?.unbounded) {
        return true;
      }
      groups[groups.length - 1] = inner || quantifier !== null;
      if (quantifier) {
        index += quantifier.length;
      }
      continue;
    }
    if (char === "|") {
      groups[top] = false;
      continue;
    }
    const quantifier = quantifierAt(pattern, index);
    if (quantifier) {
      if (quantifier.unbounded) {
        groups[top] = true;
      }
      index += quantifier.length - 1;
      continue;
    }
    groups[top] = false;
  }
  return false;
}

export function validateSearchPattern(text: string): SearchPatternError | null {
  try {
    new RegExp(text);
  } catch {
    return "syntax";
  }
  return hasNestedQuantifier(text) ? "complexity" : null;
}

export function isValidRegex(text: string): boolean {
  try {
    new RegExp(text);
    return true;
  } catch {
    return false;
  }
}
