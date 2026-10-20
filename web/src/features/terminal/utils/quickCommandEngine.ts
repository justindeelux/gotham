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
    };
  }

  /** setPrompt applies a scanner snapshot, clearing stale line state. */
  setPrompt(next: PromptState): void {
    this.prompt = { ...next };
    // Only a B mark proves a fresh editable line. Everything else (prompt
    // drawing, command running, alternate screen) is unknown to this mirror
    // and disables suggestions until B arrives again.
    this.suspended = next.phase !== "input";
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
    this.clearMirror();
    this.emit();
  }

  /** suspend forgets the line and waits for the next B mark. */
  private suspend(): void {
    this.suspended = true;
    this.clearMirror();
    this.emit();
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
