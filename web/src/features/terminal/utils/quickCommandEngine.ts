import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";
import type {
  PromptPhase,
  PromptState,
} from "@/features/terminal/transport/promptScanner";

/**
 * Quick-command abbreviation engine (JUS-249).
 *
 * This is deliberately a conservative mirror, not a model of the remote
 * shell's line editor. Suggestions are offered only while the shell has
 * reported an editable prompt (OSC 133 `B`) and only when every byte typed
 * since that mark was a single printable character or Backspace. Anything
 * else — an escape sequence (cursor keys, Home/End), Ctrl-C, Enter, a paste
 * chunk, an IME composition, a fullscreen TUI — invalidates the mirror until
 * the next prompt mark. That is what makes `accept` safe: the abbreviation is
 * known to be the last text before the cursor, so deleting N characters
 * cannot remove unrelated input.
 */

/** QuickCommandSuggestion is the one command a typed abbreviation matches. */
export interface QuickCommandSuggestion {
  abbreviation: string;
  commandId: string;
  label: string;
  command: string;
}

/** QuickCommandSuggestionState is published to the tab/UI. */
export interface QuickCommandSuggestionState {
  phase: PromptPhase;
  detected: boolean;
  alternateScreen: boolean;
  composing: boolean;
  suggestion: QuickCommandSuggestion | null;
  /** dismissed is true after Escape until the mirror changes. */
  dismissed: boolean;
  /** suspended is true when the remote line state is unknown. */
  suspended: boolean;
  /** tainted is true when type-ahead may be queued into the next prompt. */
  tainted: boolean;
  /** submitted is true between Enter and the next prompt mark. */
  submitted: boolean;
}

/** SuggestionAcceptance is the erase+insert pair returned by accept(). */
export interface SuggestionAcceptance {
  erase: number;
  text: string;
  abbreviation: string;
}

/** QUICK_COMMAND_BUFFER_MAX bounds the local mirror of the typed line. */
const BUFFER_MAX = 512;

/** isWordChar is the boundary test used before an abbreviation. */
const WORD_CHAR = /[A-Za-z0-9._-]/;

/** QuickCommandEngineOptions wires the engine to the tab and command list. */
export interface QuickCommandEngineOptions {
  getCommands: () => QuickCommand[];
  onChange?: (_state: QuickCommandSuggestionState) => void;
}

export class QuickCommandEngine {
  private readonly options: QuickCommandEngineOptions;
  private prompt: PromptState = {
    phase: "unknown",
    detected: false,
    alternateScreen: false,
  };
  private composing = false;
  private buffer = "";
  private suggestion: QuickCommandSuggestion | null = null;
  private dismissed = false;
  /**
   * suspended means the local mirror no longer knows the remote line: Enter,
   * Esc/vi normal mode, Ctrl-R/Ctrl-S, Tab completion, history recall, cursor
   * movement, paste or IME all set it. Suggestions resume only at the next
   * OSC 133 B mark, so a reset buffer can never pass the word-boundary guard
   * for text this client did not type.
   */
  private suspended = false;
  /**
   * tainted records printable input forwarded while no B mark was current
   * (type-ahead typed while a command ran). That text is already queued in
   * the PTY, so the next B prompt may not start with an empty editable line;
   * keep suggestions off for that prompt and resume at the one after it.
   */
  private tainted = false;
  /**
   * submitted is true after Enter until the next prompt mark. It marks the
   * window in which printable input is type-ahead for the running command,
   * even on a shell that never emits a C mark (bash 3.2).
   */
  private submitted = false;

  constructor(options: QuickCommandEngineOptions) {
    this.options = options;
  }

  /** getState returns the current UI snapshot. */
  getState(): QuickCommandSuggestionState {
    return {
      phase: this.prompt.phase,
      detected: this.prompt.detected,
      alternateScreen: this.prompt.alternateScreen,
      composing: this.composing,
      suggestion: this.suggestion,
      dismissed: this.dismissed,
      suspended: this.suspended,
      tainted: this.tainted,
      submitted: this.submitted,
    };
  }

  /** setPrompt applies a scanner snapshot, clearing stale line state. */
  setPrompt(next: PromptState): void {
    this.prompt = { ...next };
    if (next.phase === "input" || next.phase === "prompt") {
      this.submitted = false;
    } else if (next.alternateScreen) {
      // A fullscreen app owns the input; nothing typed there is shell
      // type-ahead for the next prompt.
      this.submitted = false;
    } else if (next.phase === "running") {
      this.submitted = true;
    }
    // Only a B mark proves a fresh editable line. If printable input was
    // forwarded while no B was current, that text may already be queued in
    // the PTY, so this prompt is tainted: stay off until the next B.
    if (next.phase === "input") {
      if (this.tainted) {
        this.tainted = false;
        this.suspended = true;
      } else {
        this.suspended = false;
      }
    } else {
      this.suspended = true;
    }
    this.clearMirror();
    this.recompute();
    this.emit();
  }

  /** setComposing suspends matching during an IME composition. */
  setComposing(composing: boolean): void {
    if (this.composing === composing) {
      return;
    }
    this.composing = composing;
    // The committed composition changes a line this mirror cannot see.
    this.suspended = true;
    this.clearMirror();
    this.emit();
  }

  /**
   * handleInput observes one xterm onData chunk and returns the bytes to
   * forward. It never swallows a byte: Esc, Ctrl-Space, Tab and Enter belong
   * to the shell, so they are always delivered. The mirror only advances on a
   * single printable character or Backspace while the line state is known.
   */
  handleInput(data: string): string | null {
    if (this.suspended) {
      // The remote line is unknown; forward untouched and wait for a B mark.
      // Ctrl-C/Ctrl-U cancel the line, so queued type-ahead no longer taints
      // the next prompt.
      if (data === "\x03" || data === "\x15") {
        this.tainted = false;
        this.submitted = false;
        return data;
      }
      // Printable input sent while a command is submitted (type-ahead) may
      // already be queued into the next prompt; taint that prompt.
      if (
        (this.submitted || this.prompt.phase === "running") &&
        !this.prompt.alternateScreen &&
        this.mayEnterLine(data)
      ) {
        this.tainted = true;
      }
      return data;
    }
    if (data === "\x1b" && this.suggestion !== null) {
      // Esc may be vi normal mode or a readline meta prefix; never swallow it.
      // Dismiss the visible suggestion and stop tracking until the next B.
      this.suspend();
      return data;
    }
    if (data.length === 1) {
      const code = data.charCodeAt(0);
      if (code === 0x7f || code === 0x08) {
        this.buffer = this.buffer.slice(0, -1);
        this.dismissed = false;
        this.recompute();
        this.emit();
        return data;
      }
      if (code >= 0x20 && code <= 0x7e) {
        this.buffer = `${this.buffer}${data}`.slice(-BUFFER_MAX);
        this.dismissed = false;
        this.recompute();
        this.emit();
        return data;
      }
    }
    // Cursor movement, Tab completion, history recall, Ctrl-R/Ctrl-S, paste,
    // IME, Enter or any other control: the remote line is no longer something
    // this mirror can vouch for.
    if (data.includes("\r") || data.includes("\n")) {
      this.submitted = true;
    }
    this.suspend();
    return data;
  }

  /**
   * accept returns the erase+insert pair for a visible suggestion without
   * changing state. The runtime plans the safe insertion first and only then
   * calls clearTracking(), so a refused command leaves the line untouched.
   */
  accept(commandId?: string): SuggestionAcceptance | null {
    const suggestion = this.suggestion;
    if (!suggestion || this.suspended) {
      return null;
    }
    if (commandId !== undefined && commandId !== suggestion.commandId) {
      return null;
    }
    if (!endsWithAbbreviation(this.buffer, suggestion.abbreviation)) {
      return null;
    }
    return {
      erase: suggestion.abbreviation.length,
      text: suggestion.command,
      abbreviation: suggestion.abbreviation,
    };
  }

  /**
   * clearTracking forgets the typed line after a successful replacement or a
   * manual insertion. Suggestions resume at the next prompt mark.
   */
  clearTracking(): void {
    this.suspended = true;
    this.clearMirror();
    this.dismissed = true;
    this.recompute();
    this.emit();
  }

  /** dismiss hides the current suggestion and stops tracking until B. */
  dismiss(): void {
    if (this.suggestion === null && this.dismissed && this.suspended) {
      return;
    }
    this.suspend();
  }

  /** reset clears everything for a new session. */
  reset(): void {
    this.prompt = { phase: "unknown", detected: false, alternateScreen: false };
    this.composing = false;
    this.suspended = true;
    this.tainted = false;
    this.submitted = false;
    this.clearMirror();
    this.emit();
  }

  /** suspend forgets the line and waits for the next B mark. */
  private suspend(): void {
    this.suspended = true;
    this.clearMirror();
    this.emit();
  }

  /**
   * mayEnterLine reports whether a chunk forwarded while suspended can leave
   * printable text in the shell's input queue. Enter, Ctrl-C, Ctrl-U, cursor
   * keys and other controls do not; printable text and paste do.
   */
  private mayEnterLine(data: string): boolean {
    for (const char of data) {
      const code = char.charCodeAt(0);
      if (code >= 0x20 && code <= 0x7e) {
        return true;
      }
    }
    return false;
  }

  /** clearMirror drops the local line model and the visible suggestion. */
  private clearMirror(): void {
    this.buffer = "";
    this.suggestion = null;
    this.dismissed = false;
  }

  /**
   * recompute finds exactly one command whose abbreviation ends the mirrored
   * line at a word boundary. Ambiguity (stored duplicate) disables the
   * suggestion instead of guessing.
   */
  private recompute(): void {
    if (
      this.prompt.phase !== "input" ||
      this.prompt.alternateScreen ||
      this.composing ||
      this.dismissed ||
      this.suspended ||
      this.buffer === ""
    ) {
      this.suggestion = null;
      return;
    }
    const matches = this.options
      .getCommands()
      .filter(
        (command) =>
          command.abbreviation !== undefined &&
          command.abbreviation !== "" &&
          endsWithAbbreviation(this.buffer, command.abbreviation),
      );
    if (matches.length !== 1) {
      this.suggestion = null;
      return;
    }
    const command = matches[0]!;
    this.suggestion = {
      abbreviation: command.abbreviation!,
      commandId: command.id,
      label: command.label,
      command: command.command,
    };
  }

  /** emit publishes one snapshot. */
  private emit(): void {
    this.options.onChange?.(this.getState());
  }
}

/**
 * endsWithAbbreviation checks a case-insensitive suffix at a word boundary.
 * The boundary keeps `xgs` from matching `gs`, while `sudo gs` still matches.
 */
export function endsWithAbbreviation(line: string, abbreviation: string): boolean {
  if (abbreviation === "" || line.length < abbreviation.length) {
    return false;
  }
  const start = line.length - abbreviation.length;
  if (line.slice(start).toLowerCase() !== abbreviation.toLowerCase()) {
    return false;
  }
  if (start === 0) {
    return true;
  }
  return !WORD_CHAR.test(line.charAt(start - 1));
}
