/**
 * Regex validation for the custom find bar (JUS-231). Kept free of CodeMirror
 * imports so the eager files-page chunk can validate a pattern without pulling
 * the lazy editor chunk in.
 */

/** isValidRegex reports whether the regular-expression option accepts text. */
export function isValidRegex(text: string): boolean {
  try {
    new RegExp(text);
    return true;
  } catch {
    return false;
  }
}
