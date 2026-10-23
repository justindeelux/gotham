import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";
import type {
  PromptPhase,
  PromptState,
} from "@/features/terminal/transport/promptScanner";

export interface QuickCommandSuggestion {
  abbreviation: string;
  commandId: string;
  label: string;
  command: string;
}

export interface QuickCommandSuggestionState {
  phase: PromptPhase;
  detected: boolean;
  alternateScreen: boolean;
  composing: boolean;
  suggestion: QuickCommandSuggestion | null;
  dismissed: boolean;
  suspended: boolean;
  tainted: boolean;
  submitted: boolean;
}

export interface SuggestionAcceptance {
  erase: number;
  text: string;
  abbreviation: string;
}

const BUFFER_MAX = 512;

const WORD_CHAR = /[A-Za-z0-9._-]/;

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
  private suspended = false;
  private tainted = false;
  private submitted = false;

  constructor(options: QuickCommandEngineOptions) {
    this.options = options;
  }

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

  setPrompt(next: PromptState): void {
    this.prompt = { ...next };
    if (next.phase === "input" || next.phase === "prompt") {
      this.submitted = false;
    } else if (next.alternateScreen) {
      this.submitted = false;
    } else if (next.phase === "running") {
      this.submitted = true;
    }
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

  setComposing(composing: boolean): void {
    if (this.composing === composing) {
      return;
    }
    this.composing = composing;
    this.suspended = true;
    this.clearMirror();
    this.emit();
  }

  handleInput(data: string): string | null {
    if (this.suspended) {
      if (data === "\x03" || data === "\x15") {
        this.tainted = false;
        this.submitted = false;
        return data;
      }
      if (data.includes("\r") || data.includes("\n")) {
        this.submitted = true;
      }
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
    if (data.includes("\r") || data.includes("\n")) {
      this.submitted = true;
    }
    this.suspend();
    return data;
  }

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

  clearTracking(): void {
    this.suspended = true;
    this.clearMirror();
    this.dismissed = true;
    this.recompute();
    this.emit();
  }

  dismiss(): void {
    if (this.suggestion === null && this.dismissed && this.suspended) {
      return;
    }
    this.suspend();
  }

  reset(): void {
    this.prompt = { phase: "unknown", detected: false, alternateScreen: false };
    this.composing = false;
    this.suspended = true;
    this.tainted = false;
    this.submitted = false;
    this.clearMirror();
    this.emit();
  }

  private suspend(): void {
    this.suspended = true;
    this.clearMirror();
    this.emit();
  }

  private mayEnterLine(data: string): boolean {
    for (const char of data) {
      const code = char.charCodeAt(0);
      if (code >= 0x20 && code <= 0x7e) {
        return true;
      }
    }
    return false;
  }

  private clearMirror(): void {
    this.buffer = "";
    this.suggestion = null;
    this.dismissed = false;
  }

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

  private emit(): void {
    this.options.onChange?.(this.getState());
  }
}

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
