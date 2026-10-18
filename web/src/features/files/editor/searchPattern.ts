/**
 * Pattern validation for the custom find bar (JUS-231). Kept free of CodeMirror
 * imports so the eager files-page chunk can validate a pattern without pulling
 * the lazy editor chunk in.
 *
 * `validateSearchPattern` does more than syntax: a regex whose catastrophic
 * backtracking can freeze the main thread (a group ending in an unbounded
 * quantifier, itself quantified — `(a+)+$`, `(.*)*`) is rejected as too
 * complex before it ever reaches the editor. JavaScript cannot interrupt a
 * running `RegExp.exec`, so refusing the shape is the only guard that works.
 */

/** SearchPatternError is the inline reason a pattern is not used. */
export type SearchPatternError = "syntax" | "complexity";

/** QUANTIFIER matches a brace quantifier at the start of `text`. */
const QUANTIFIER = /^\{(\d*)(?:,(\d*))?\}/;

/**
 * quantifierAt reports whether a quantifier starts at `at`, and whether it has
 * no upper bound. `{n}` and `{n,m}` are bounded; `{n,}` and `{,}` are not.
 */
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

/**
 * hasNestedQuantifier detects the catastrophic shape: a group whose last token
 * is an unbounded quantifier and that is itself unbounded-quantified.
 * `(a+)+`, `(.*)*` and `((a+))+` match; `(\d+\.)+` does not, because the group
 * ends with a literal separator, so its backtracking is bounded in practice.
 *
 * shortcut: this is a narrow heuristic, not a full safe-regex analysis; a novel
 * catastrophic shape can still reach RegExp.exec. Replace it with a worker that
 * runs the count behind a hard timeout if a user reports a freeze.
 */
export function hasNestedQuantifier(pattern: string): boolean {
  // One entry per open group; true when its last token is an unbounded repeat.
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
        // Unbalanced close: invalid syntax is rejected before this runs.
        groups[0] = false;
        continue;
      }
      const inner = groups.pop() ?? false;
      const quantifier = quantifierAt(pattern, index + 1);
      if (inner && quantifier?.unbounded) {
        return true;
      }
      // The group is one atom for the parent; it ends in a repeat if either the
      // group body did or the group itself carries a quantifier.
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

/** validateSearchPattern returns the inline error, or null when usable. */
export function validateSearchPattern(text: string): SearchPatternError | null {
  try {
    new RegExp(text);
  } catch {
    return "syntax";
  }
  return hasNestedQuantifier(text) ? "complexity" : null;
}

/** isValidRegex reports whether the pattern is syntactically valid. */
export function isValidRegex(text: string): boolean {
  try {
    new RegExp(text);
    return true;
  } catch {
    return false;
  }
}
