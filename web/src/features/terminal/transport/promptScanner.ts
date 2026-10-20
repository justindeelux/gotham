/**
 * Incremental PTY prompt/screen scanner (JUS-249).
 *
 * Suggestion replacement needs a trustworthy "the shell is editing a fresh
 * command line" signal. xterm is only an emulator, so this module never parses
 * prompt text and never infers state from arbitrary keystrokes: it observes
 * the OSC 133/633 shell-integration marks that a supported shell prints, plus
 * the alternate-screen switches a fullscreen TUI uses. PTY bytes are passed
 * through to xterm untouched; output can split a sequence across frames, so
 * the scanner keeps a bounded partial-sequence buffer.
 *
 * OSC 133 semantics:
 *   A  prompt starts (no editable line yet)
 *   B  prompt ends / editable command line starts
 *   C  command was submitted and is running
 *   D  command finished (optional exit code)
 * OSC 633 is VS Code's equivalent and is accepted with the same meanings.
 */

/** PromptPhase is the shell-integration state for one session. */
export type PromptPhase = "unknown" | "prompt" | "input" | "running";

/** PromptState is a snapshot the runtime publishes to the suggestion engine. */
export interface PromptState {
  phase: PromptPhase;
  /** detected is true once this session has emitted any prompt mark. */
  detected: boolean;
  /** alternateScreen is true while a fullscreen TUI owns the screen. */
  alternateScreen: boolean;
}

/** UNKNOWN_PROMPT_STATE is the initial state before any mark arrives. */
export const UNKNOWN_PROMPT_STATE: PromptState = {
  phase: "unknown",
  detected: false,
  alternateScreen: false,
};

/** OSC_MAX_BYTES bounds a partial OSC sequence; a longer one is dropped. */
const OSC_MAX_BYTES = 512;
/** CSI_MAX_BYTES bounds a partial CSI sequence. */
const CSI_MAX_BYTES = 64;

const ESC = 0x1b;
const BEL = 0x07;
const OSC_INTRODUCER = 0x5d; // ]
const CSI_INTRODUCER = 0x5b; // [
const ST_FINAL = 0x5c; // \ (the second byte of ST)

/** PromptScanner observes one PTY byte stream and emits prompt changes. */
export class PromptScanner {
  private pending: number[] = [];
  private state: PromptState = { ...UNKNOWN_PROMPT_STATE };
  private readonly onChange?: (_state: PromptState) => void;

  /**
   * onChange is called only when the observable state changes, so a chatty
   * shell emitting A/B for every prompt does not re-render Vue needlessly.
   */
  constructor(onChange?: (_state: PromptState) => void) {
    this.onChange = onChange;
  }

  /** getState returns the current snapshot (never a mutable reference). */
  getState(): PromptState {
    return { ...this.state };
  }

  /** reset forgets all state for a brand-new session. */
  reset(): void {
    this.pending = [];
    this.state = { ...UNKNOWN_PROMPT_STATE };
  }

  /** push scans one output chunk; the bytes are not consumed or modified. */
  push(bytes: Uint8Array): void {
    for (const byte of bytes) {
      this.feed(byte);
    }
  }

  /** feed advances the escape-sequence state machine by one byte. */
  private feed(byte: number): void {
    if (this.pending.length === 0) {
      if (byte === ESC) {
        this.pending.push(byte);
      }
      return;
    }
    this.pending.push(byte);
    const introducer = this.pending[1];
    if (introducer === OSC_INTRODUCER) {
      if (byte === BEL) {
        this.completeOsc();
        return;
      }
      if (byte === ST_FINAL && this.pending[this.pending.length - 2] === ESC) {
        this.completeOsc();
        return;
      }
      if (this.pending.length > OSC_MAX_BYTES) {
        this.pending = [];
      }
      return;
    }
    if (introducer === CSI_INTRODUCER) {
      // A CSI sequence ends with a final byte in 0x40-0x7e; the introducer
      // case itself is handled on the next byte.
      if (this.pending.length >= 3 && byte >= 0x40 && byte <= 0x7e) {
        this.completeCsi();
        return;
      }
      if (this.pending.length > CSI_MAX_BYTES) {
        this.pending = [];
      }
      return;
    }
    // A lone ESC followed by an ordinary byte is not a sequence we track
    // (Alt+key echo, for example). Drop it without buffering more output.
    this.pending = [];
  }

  /** completeOsc parses one finished OSC body and resets the buffer. */
  private completeOsc(): void {
    // eslint-disable-next-line no-control-regex -- stripping the ST terminator is intentional.
    const body = ascii(this.pending, 2).replace(/\x1b\\$/, "");
    this.pending = [];
    const [code, kind] = body.split(";");
    if ((code !== "133" && code !== "633") || !kind) {
      return;
    }
    const mark = kind.charAt(0).toUpperCase();
    switch (mark) {
      case "A":
        this.setPhase("prompt", true);
        return;
      case "B":
        this.setPhase("input", true);
        return;
      case "C":
        this.setPhase("running", true);
        return;
      case "D":
        this.setPhase("running", true);
        return;
      default:
        return;
    }
  }

  /** completeCsi recognises the alternate-screen switches a TUI uses. */
  private completeCsi(): void {
    const body = ascii(this.pending, 2);
    this.pending = [];
    const match = /^\?(47|1047|1049)([hl])$/.exec(body);
    if (!match) {
      return;
    }
    const enter = match[2] === "h";
    this.setState({
      phase: "unknown",
      detected: this.state.detected,
      alternateScreen: enter,
    });
  }

  /** setPhase updates the prompt phase and detection flag. */
  private setPhase(phase: PromptPhase, detected: boolean): void {
    this.setState({
      phase,
      detected,
      // An OSC mark means the alternate-screen flag is stale at worst; a
      // shell prompt after a TUI exits clears it below.
      alternateScreen: phase === "input" ? false : this.state.alternateScreen,
    });
  }

  /** setState emits only a real change. */
  private setState(next: PromptState): void {
    if (
      next.phase === this.state.phase &&
      next.detected === this.state.detected &&
      next.alternateScreen === this.state.alternateScreen
    ) {
      return;
    }
    this.state = next;
    this.onChange?.({ ...next });
  }
}

/** ascii decodes bytes from `start` as Latin-1 (escape sequences are ASCII). */
function ascii(bytes: number[], start: number): string {
  let result = "";
  for (let index = start; index < bytes.length; index += 1) {
    result += String.fromCharCode(bytes[index]!);
  }
  return result;
}
