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
   * awaitingPrompt is set by Enter and cleared by the next B mark. It keeps a
   * shell without a C mark (older bash) from suggesting while a submitted
   * command is still running.
   */
  private awaitingPrompt = false;

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
    };
  }

  /** setPrompt applies a scanner snapshot, clearing stale line state. */
  setPrompt(next: PromptState): void {
    this.prompt = { ...next };
    if (next.phase === "input") {
      // A B mark always starts a fresh editable line: even a redraw makes the
      // old mirror untrustworthy, and it is the only safe resume point.
      this.awaitingPrompt = false;
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
    this.clearMirror();
    this.emit();
  }

  /**
   * handleInput observes one xterm onData chunk and returns the bytes to
   * forward, or null to swallow them (Escape dismissing a suggestion). The
   * mirror only advances on a single printable character or Backspace.
   */
  handleInput(data: string): string | null {
    if (data === "\x1b" && this.suggestion !== null) {
      this.dismissed = true;
      this.suggestion = null;
      this.emit();
      return null;
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
    // Cursor movement, paste, IME, Enter, Ctrl-C or any other control: the
    // remote line is no longer something this mirror can vouch for.
    if (data.includes("\r") || data.includes("\n")) {
      this.awaitingPrompt = true;
    }
    this.clearMirror();
    this.emit();
    return data;
  }

  /**
   * accept returns the erase+insert pair for a visible suggestion without
   * changing state. The runtime plans the safe insertion first and only then
   * calls clearTracking(), so a refused command leaves the line untouched.
   */
  accept(commandId?: string): SuggestionAcceptance | null {
    const suggestion = this.suggestion;
    if (!suggestion) {
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
    this.clearMirror();
    this.dismissed = true;
    this.recompute();
    this.emit();
  }

  /** dismiss hides the current suggestion (Escape / UI close). */
  dismiss(): void {
    if (this.suggestion === null && this.dismissed) {
      return;
    }
    this.dismissed = true;
    this.suggestion = null;
    this.emit();
  }

  /** reset clears everything for a new session. */
  reset(): void {
    this.prompt = { phase: "unknown", detected: false, alternateScreen: false };
    this.composing = false;
    this.awaitingPrompt = false;
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
      this.awaitingPrompt ||
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
