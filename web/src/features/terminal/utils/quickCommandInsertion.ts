/**
 * Safe insertion of saved command text into a PTY (JUS-249).
 *
 * A saved command is inserted, never executed: this module NEVER appends
 * Enter/CR. Multiline text (and tabs) is only sent inside bracketed paste when
 * the application has enabled it (readline/zle do by default in supported
 * shells), so the shell inserts it into the editing buffer instead of running
 * each line. Any other C0/C1 control byte is refused rather than stripped,
 * because silently changing a command could run something different from what
 * the user reviewed.
 */

/** InsertionFailure explains why nothing was inserted. */
export type InsertionFailure = "empty" | "control" | "multiline-no-paste";

/** InsertionPlan is the exact bytes to send, or a refusal. */
export interface InsertionPlan {
  ok: boolean;
  /** sequence is the raw input to send; empty when ok is false. */
  sequence: string;
  /** failure is set when ok is false. */
  failure?: InsertionFailure;
}

/** BRACKETED_PASTE_START/END are the xterm bracketed-paste guards. */
const BRACKETED_PASTE_START = "\x1b[200~";
const BRACKETED_PASTE_END = "\x1b[201~";

/**
 * CONTROL_CHARS matches every C0/C1 control except tab (0x09) and newline
 * (0x0a), which are handled separately. ESC is included, so a saved command
 * can never smuggle a terminal escape sequence.
 */
const CONTROL_CHARS = /[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/; // eslint-disable-line no-control-regex -- deliberate C0/C1 range

/**
 * prepareInsertion turns reviewed command text into a safe input sequence.
 * `bracketedPaste` must come from the live terminal's mode (readline enables
 * it; a plain TUI may not), never assumed.
 */
export function prepareInsertion(
  raw: string,
  bracketedPaste: boolean,
): InsertionPlan {
  const text = raw.replace(/\r\n?/g, "\n");
  if (text.trim() === "") {
    return { ok: false, sequence: "", failure: "empty" };
  }
  if (CONTROL_CHARS.test(text)) {
    return { ok: false, sequence: "", failure: "control" };
  }
  const needsPaste = text.includes("\n") || text.includes("\t");
  if (needsPaste) {
    if (!bracketedPaste) {
      return { ok: false, sequence: "", failure: "multiline-no-paste" };
    }
    return {
      ok: true,
      sequence: `${BRACKETED_PASTE_START}${text}${BRACKETED_PASTE_END}`,
    };
  }
  return { ok: true, sequence: text };
}

/**
 * prepareAcceptance builds the bytes for accepting a suggestion: delete the
 * typed abbreviation (cursor is at the end by construction) then insert the
 * command. It computes the insertion plan first so a refused command leaves
 * the typed abbreviation untouched.
 */
export function prepareAcceptance(
  erase: number,
  command: string,
  bracketedPaste: boolean,
): InsertionPlan {
  const plan = prepareInsertion(command, bracketedPaste);
  if (!plan.ok) {
    return plan;
  }
  const deleteCount = Math.max(0, Math.trunc(erase));
  return { ok: true, sequence: `${"\x7f".repeat(deleteCount)}${plan.sequence}` };
}

/** shellBasename reduces an absolute shell path to its program name. */
export function shellBasename(shell: string): string {
  const trimmed = shell.trim();
  if (trimmed === "") {
    return "";
  }
  const parts = trimmed.split("/");
  return parts[parts.length - 1] ?? "";
}

/**
 * shellIntegrationSnippet returns the optional, additive rc snippet for a
 * supported shell, or "" when this build has no tested integration. The
 * snippet is only ever shown/copied to the user; Gotham never injects it into
 * the shell, and it never writes to a PTY.
 */
export function shellIntegrationSnippet(shell: string): string {
  const name = shellBasename(shell);
  if (name === "bash") {
    return [
      "# ~/.bashrc — optional Gotham prompt marks (bash >= 4.4).",
      "# Additive: keeps your existing prompt and PROMPT_COMMAND.",
      "__gotham_prompt_command() {",
      "  local status=$?",
      "  printf '\\033]133;D;%s\\a' \"$status\"",
      "  printf '\\033]133;A\\a'",
      "  return $status",
      "}",
      "PROMPT_COMMAND=\"__gotham_prompt_command${PROMPT_COMMAND:+; $PROMPT_COMMAND}\"",
      "PS1=\"${PS1}\\[\\033]133;B\\a\\]\"",
      "PS0=\"\\[\\033]133;C\\a\\]${PS0:-}\"",
    ].join("\n");
  }
  if (name === "zsh") {
    return [
      "# ~/.zshrc — optional Gotham prompt marks (zsh >= 5.3).",
      "# Additive: keeps your theme and existing hooks.",
      "__gotham_precmd() {",
      "  local status=$?",
      "  print -n $'\\e]133;D;'\"$status\"$'\\a\\e]133;A\\a'",
      "  if [[ $PROMPT != *$'\\e]133;B\\a'* ]]; then",
      "    PROMPT=\"${PROMPT}\"$'%{\\e]133;B\\a%}'",
      "  fi",
      "}",
      "__gotham_preexec() {",
      "  print -n $'\\e]133;C\\a'",
      "}",
      "autoload -Uz add-zsh-hook",
      "add-zsh-hook precmd __gotham_precmd",
      "add-zsh-hook preexec __gotham_preexec",
    ].join("\n");
  }
  return "";
}

/** isSupportedSuggestionShell reports whether a tested snippet exists. */
export function isSupportedSuggestionShell(shell: string): boolean {
  const name = shellBasename(shell);
  return name === "bash" || name === "zsh";
}
