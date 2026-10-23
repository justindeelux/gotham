
export type InsertionFailure = "empty" | "control" | "multiline-no-paste";

export interface InsertionPlan {
  ok: boolean;
  sequence: string;
  failure?: InsertionFailure;
}

const BRACKETED_PASTE_START = "\x1b[200~";
const BRACKETED_PASTE_END = "\x1b[201~";

const CONTROL_CHARS = /[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/; // eslint-disable-line no-control-regex -- deliberate C0/C1 range

export function prepareInsertion(
  raw: string,
  bracketedPaste: boolean,
): InsertionPlan {
  const text = raw.replace(/\r\n?/g, "\n");
  if (text.trim() === "") {
    return { ok: false, sequence: "", failure: "empty" };
  }
  if (CONTROL_CHARS.test(text) || /\p{Bidi_Control}/u.test(text)) {
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

export function shellBasename(shell: string): string {
  const trimmed = shell.trim();
  if (trimmed === "") {
    return "";
  }
  const parts = trimmed.split("/");
  return parts[parts.length - 1] ?? "";
}

export function shellIntegrationSnippet(shell: string): string {
  const name = shellBasename(shell);
  if (name === "bash") {
    return [
      "# ~/.bashrc — optional Gotham prompt marks (bash >= 4.4).",
      "# Additive and idempotent: safe to source more than once.",
      "if ! declare -F __gotham_prompt_command >/dev/null 2>&1; then",
      "  __gotham_prompt_command() {",
      "    local st=$?",
      "    printf '\\033]133;D;%s\\a' \"$st\"",
      "    printf '\\033]133;A\\a'",
      "    case \"$PS1\" in",
      "      *'133;B'*) ;;",
      "      *) PS1=\"$PS1\"'\\[\\033]133;B\\a\\]' ;;",
      "    esac",
      "    return $st",
      "  }",
      "fi",
      "case \"$PROMPT_COMMAND\" in",
      "  *__gotham_prompt_command*) ;;",
      "  *) PROMPT_COMMAND=\"__gotham_prompt_command${PROMPT_COMMAND:+; $PROMPT_COMMAND}\" ;;",
      "esac",
      "case \"$PS0\" in",
      "  *'133;C'*) ;;",
      "  *) PS0=\"\\[\\033]133;C\\a\\]${PS0:-}\" ;;",
      "esac",
    ].join("\n");
  }
  if (name === "zsh") {
    return [
      "# ~/.zshrc — optional Gotham prompt marks (zsh >= 5.3).",
      "# Additive and safe to source more than once.",
      "__gotham_precmd() {",
      "  local st=$?",
      "  print -n $'\\e]133;D;'\"$st\"$'\\a\\e]133;A\\a'",
      "  if [[ $PROMPT != *$'\\e]133;B\\a'* ]]; then",
      "    PROMPT=\"${PROMPT}\"$'%{\\e]133;B\\a%}'",
      "  fi",
      "}",
      "__gotham_preexec() {",
      "  print -n $'\\e]133;C\\a'",
      "}",
      "__gotham_line_init() {",
      "  print -n $'\\e]133;B\\a'",
      "}",
      "autoload -Uz add-zsh-hook",
      "add-zsh-hook precmd __gotham_precmd",
      "add-zsh-hook preexec __gotham_preexec",
      "autoload -Uz add-zle-hook-widget",
      "add-zle-hook-widget line-init __gotham_line_init",
    ].join("\n");
  }
  return "";
}

export function isSupportedSuggestionShell(shell: string): boolean {
  const name = shellBasename(shell);
  return name === "bash" || name === "zsh";
}
