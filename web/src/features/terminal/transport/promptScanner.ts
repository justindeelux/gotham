
export type PromptPhase = "unknown" | "prompt" | "input" | "running";

export interface PromptState {
  phase: PromptPhase;
  detected: boolean;
  alternateScreen: boolean;
}

export const UNKNOWN_PROMPT_STATE: PromptState = {
  phase: "unknown",
  detected: false,
  alternateScreen: false,
};

const OSC_MAX_BYTES = 512;
const CSI_MAX_BYTES = 64;

const ESC = 0x1b;
const BEL = 0x07;
const OSC_INTRODUCER = 0x5d;
const CSI_INTRODUCER = 0x5b;
const ST_FINAL = 0x5c;

export class PromptScanner {
  private pending: number[] = [];
  private state: PromptState = { ...UNKNOWN_PROMPT_STATE };
  private readonly onChange?: (_state: PromptState) => void;

  constructor(onChange?: (_state: PromptState) => void) {
    this.onChange = onChange;
  }

  getState(): PromptState {
    return { ...this.state };
  }

  reset(): void {
    this.pending = [];
    this.state = { ...UNKNOWN_PROMPT_STATE };
  }

  push(bytes: Uint8Array): void {
    for (const byte of bytes) {
      this.feed(byte);
    }
  }

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
      if (this.pending.length >= 3 && byte >= 0x40 && byte <= 0x7e) {
        this.completeCsi();
        return;
      }
      if (this.pending.length > CSI_MAX_BYTES) {
        this.pending = [];
      }
      return;
    }
    this.pending = [];
  }

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

  private completeCsi(): void {
    const body = ascii(this.pending, 2);
    this.pending = [];
    const match = /^\?([0-9;]+)([hl])$/.exec(body);
    if (!match) {
      return;
    }
    const modes = match[1]!
      .split(";")
      .map((value) => Number.parseInt(value, 10));
    if (!modes.some((mode) => mode === 47 || mode === 1047 || mode === 1049)) {
      return;
    }
    const enter = match[2] === "h";
    this.setState({
      phase: "unknown",
      detected: this.state.detected,
      alternateScreen: enter,
    });
  }

  private setPhase(phase: PromptPhase, detected: boolean): void {
    this.setState({
      phase,
      detected,
      alternateScreen: this.state.alternateScreen,
    });
  }

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

function ascii(bytes: number[], start: number): string {
  let result = "";
  for (let index = start; index < bytes.length; index += 1) {
    result += String.fromCharCode(bytes[index]!);
  }
  return result;
}
