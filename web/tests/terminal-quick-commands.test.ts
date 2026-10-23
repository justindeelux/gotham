/* global HTMLElement:readonly, TextEncoder:readonly, TextDecoder:readonly, window:readonly */
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import {
  QUICK_COMMANDS_MAX,
  QUICK_COMMAND_LABEL_MAX,
  QUICK_COMMAND_TEXT_MAX,
  newQuickCommandId,
  parseQuickCommands,
  quickCommandDraftSchema,
} from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";
import { useQuickCommandsStore } from "@/features/terminal/stores/quickCommands";
import {
  CLIENT_INPUT,
  encodeInput,
} from "@/features/terminal/transport/frames";
import { PromptScanner } from "@/features/terminal/transport/promptScanner";
import {
  TerminalRuntime,
} from "@/features/terminal/transport/terminalRuntime";
import type {
  TerminalModules,
  TerminalSocket,
  TerminalStatePatch,
} from "@/features/terminal/transport/terminalRuntime";
import {
  QuickCommandEngine,
  endsWithAbbreviation,
} from "@/features/terminal/utils/quickCommandEngine";
import {
  prepareAcceptance,
  prepareInsertion,
  shellIntegrationSnippet,
} from "@/features/terminal/utils/quickCommandInsertion";
import {
  quickCommandsStorageKey,
  readQuickCommands,
  writeQuickCommands,
} from "@/features/terminal/utils/quickCommandStorage";
import { setSession } from "@/shared/api/token";
import type { User } from "@/shared/api/token";

function command(
  id: string,
  label: string,
  text: string,
  abbreviation?: string,
): QuickCommand {
  return {
    id,
    label,
    command: text,
    ...(abbreviation !== undefined ? { abbreviation } : {}),
  };
}

function memoryStorage(): {
  getItem: (_key: string) => string | null;
  setItem: (_key: string, _value: string) => void;
  map: Map<string, string>;
} {
  const map = new Map<string, string>();
  return {
    map,
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
  };
}

function account(id: string): User {
  return {
    id,
    email: `${id}@gotham.dev`,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function bytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

function concat(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}

function osc(mark: string, code = "133"): Uint8Array {
  return bytes(`\x1b]${code};${mark}\x07`);
}

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
});

describe("quick command schemas", () => {
  it("accepts bounded fields and normalises an empty abbreviation", () => {
    const parsed = quickCommandDraftSchema([]).safeParse({
      label: "  Deploy API  ",
      command: "  docker compose up -d  ",
      abbreviation: "  ",
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data.label).toBe("Deploy API");
      expect(parsed.data.command).toBe("docker compose up -d");
      expect(parsed.data.abbreviation).toBeUndefined();
    }
  });

  it("rejects empty and over-long labels/commands", () => {
    expect(
      quickCommandDraftSchema([]).safeParse({ label: "  ", command: "echo ok" })
        .success,
    ).toBe(false);
    expect(
      quickCommandDraftSchema([]).safeParse({ label: "x", command: "  " })
        .success,
    ).toBe(false);
    expect(
      quickCommandDraftSchema([]).safeParse({
        label: "x".repeat(QUICK_COMMAND_LABEL_MAX + 1),
        command: "echo ok",
      }).success,
    ).toBe(false);
    expect(
      quickCommandDraftSchema([]).safeParse({
        label: "x",
        command: "x".repeat(QUICK_COMMAND_TEXT_MAX + 1),
      }).success,
    ).toBe(false);
  });

  it("rejects duplicate abbreviations case-insensitively", () => {
    const existing = [command("a", "One", "echo one", "GS")];
    const duplicate = quickCommandDraftSchema(existing).safeParse({
      label: "Two",
      command: "echo two",
      abbreviation: "gs",
    });
    expect(duplicate.success).toBe(false);
    const self = quickCommandDraftSchema(existing, "a").safeParse({
      label: "One",
      command: "echo one",
      abbreviation: "gs",
    });
    expect(self.success).toBe(true);
  });

  it("rejects shell-breaking abbreviations", () => {
    for (const abbreviation of ["a b", "a;b", "$x", "a\nb", "a/b", "_x"]) {
      expect(
        quickCommandDraftSchema([]).safeParse({
          label: "x",
          command: "echo ok",
          abbreviation,
        }).success,
      ).toBe(false);
    }
  });

  it("allows newline/tab but rejects other control and bidi characters", () => {
    expect(
      quickCommandDraftSchema([]).safeParse({
        label: "multi",
        command: "echo one\necho two\tend",
      }).success,
    ).toBe(true);
    for (const text of ["echo \x1b[31mred", "echo \x07", "echo \x00"]) {
      expect(
        quickCommandDraftSchema([]).safeParse({ label: "x", command: text })
          .success,
      ).toBe(false);
    }
    expect(
      quickCommandDraftSchema([]).safeParse({
        label: "x",
        command: "echo \u202egnp.exe",
      }).success,
    ).toBe(false);
  });

  it("parses stored data defensively and deduplicates", () => {
    const valid = command("a", "One", "echo one", "gs");
    expect(
      parseQuickCommands({
        version: 1,
        commands: [
          valid,
          { ...valid, id: "b", abbreviation: "GS" },
          { id: "broken" },
          "nope",
        ],
      }),
    ).toEqual([valid]);
    expect(parseQuickCommands({ version: 2, commands: [valid] })).toEqual([]);
    expect(parseQuickCommands(null)).toEqual([]);
    expect(parseQuickCommands({ version: 1, commands: "nope" })).toEqual([]);
  });

  it("calls it a match only at a word boundary and suffix", () => {
    expect(endsWithAbbreviation("gs", "gs")).toBe(true);
    expect(endsWithAbbreviation("sudo gs", "gs")).toBe(true);
    expect(endsWithAbbreviation("sudo;gs", "gs")).toBe(true);
    expect(endsWithAbbreviation("xgs", "gs")).toBe(false);
    expect(endsWithAbbreviation("gsx", "gs")).toBe(false);
    expect(endsWithAbbreviation("GS", "gs")).toBe(true);
  });

  it("generates distinct ids", () => {
    expect(newQuickCommandId()).not.toBe(newQuickCommandId());
  });
});

describe("quick command storage", () => {
  it("isolates accounts by key and survives a corrupt payload", () => {
    const storage = memoryStorage();
    expect(
      writeQuickCommands(storage, "u-1", [command("a", "One", "echo one", "o")]),
    ).toBe(true);
    expect(readQuickCommands(storage, "u-1")).toHaveLength(1);
    expect(readQuickCommands(storage, "u-2")).toEqual([]);
    expect(quickCommandsStorageKey("")).toBe("");

    storage.map.set(quickCommandsStorageKey("u-2"), "{not json");
    expect(readQuickCommands(storage, "u-2")).toEqual([]);

    storage.map.set(quickCommandsStorageKey("u-3"), "x".repeat(200 * 1024));
    expect(readQuickCommands(storage, "u-3")).toEqual([]);
  });

  it("reports blocked and quota writes without throwing", () => {
    const blocked = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    };
    expect(readQuickCommands(blocked, "u-1")).toEqual([]);
    expect(writeQuickCommands(blocked, "u-1", [])).toBe(false);

    const quota = {
      getItem: () => null,
      setItem: () => {
        throw new Error("QuotaExceededError");
      },
    };
    expect(writeQuickCommands(quota, "u-1", [command("a", "One", "echo")])).toBe(
      false,
    );
  });

  it("refuses an over-limit list", () => {
    const storage = memoryStorage();
    const many = Array.from({ length: QUICK_COMMANDS_MAX + 1 }, (_, index) =>
      command(`id-${index}`, `Label ${index}`, "echo"),
    );
    expect(writeQuickCommands(storage, "u-1", many)).toBe(false);
  });

  it("can store the full 40-command list with JSON-escaped text", () => {
    const storage = memoryStorage();
    const many = Array.from({ length: QUICK_COMMANDS_MAX }, (_, index) =>
      command(`id-${index}`, `Label ${index}`, '"'.repeat(QUICK_COMMAND_TEXT_MAX)),
    );
    expect(writeQuickCommands(storage, "u-1", many)).toBe(true);
    expect(readQuickCommands(storage, "u-1")).toHaveLength(QUICK_COMMANDS_MAX);
  });
});

describe("safe insertion", () => {
  it("inserts a single line verbatim and never appends Enter", () => {
    const plan = prepareInsertion("echo hello", false);
    expect(plan).toEqual({ ok: true, sequence: "echo hello" });
    expect(plan.sequence).not.toContain("\r");
    expect(plan.sequence).not.toContain("\n");
  });

  it("refuses multiline and tab content without bracketed paste", () => {
    expect(prepareInsertion("echo one\necho two", false)).toMatchObject({
      ok: false,
      failure: "multiline-no-paste",
      sequence: "",
    });
    expect(prepareInsertion("printf a\tb", false)).toMatchObject({
      ok: false,
      failure: "multiline-no-paste",
    });
  });

  it("wraps multiline/tab content in bracketed paste when enabled", () => {
    const plan = prepareInsertion("echo one\necho two", true);
    expect(plan.ok).toBe(true);
    expect(plan.sequence).toBe("\x1b[200~echo one\necho two\x1b[201~");
    expect(plan.sequence).not.toContain("\r");
  });

  it("refuses every control byte rather than stripping it", () => {
    for (const text of ["echo \x00", "echo \x07", "echo \x1b[31m", "echo \x7f"]) {
      expect(prepareInsertion(text, true)).toMatchObject({
        ok: false,
        failure: "control",
        sequence: "",
      });
    }
    expect(prepareInsertion("echo \u202egnp.exe", true)).toMatchObject({
      ok: false,
      failure: "control",
    });
  });

  it("erases exactly the abbreviation and computes insertion first", () => {
    expect(prepareAcceptance(2, "git status", false)).toEqual({
      ok: true,
      sequence: "\x7f\x7fgit status",
    });
    expect(prepareAcceptance(2, "echo one\necho two", false)).toMatchObject({
      ok: false,
      failure: "multiline-no-paste",
    });
  });

  it("ships snippets only for tested shells", () => {
    expect(shellIntegrationSnippet("/bin/bash")).toContain("]133;B");
    expect(shellIntegrationSnippet("/usr/bin/zsh")).toContain("133;B");
    expect(shellIntegrationSnippet("/bin/sh")).toBe("");
    expect(shellIntegrationSnippet("/usr/bin/fish")).toBe("");
  });
});

describe("prompt scanner", () => {
  it("tracks A/B/C/D across split frames, including OSC 633", () => {
    const states: string[] = [];
    const scanner = new PromptScanner((state) =>
      states.push(`${state.phase}:${state.detected}`),
    );
    scanner.push(osc("A").subarray(0, 3));
    scanner.push(osc("A").subarray(3));
    scanner.push(osc("B"));
    expect(scanner.getState()).toMatchObject({
      phase: "input",
      detected: true,
    });
    scanner.push(osc("C"));
    expect(scanner.getState().phase).toBe("running");
    scanner.push(osc("D;0"));
    expect(scanner.getState().phase).toBe("running");
    scanner.push(osc("B", "633"));
    expect(scanner.getState().phase).toBe("input");
    expect(states).toEqual([
      "prompt:true",
      "input:true",
      "running:true",
      "input:true",
    ]);
  });

  it("treats alternate-screen enter/exit as a capture", () => {
    const scanner = new PromptScanner();
    scanner.push(bytes("\x1b[?1049h"));
    expect(scanner.getState()).toMatchObject({
      phase: "unknown",
      alternateScreen: true,
    });
    scanner.push(bytes("\x1b[?1049l"));
    expect(scanner.getState()).toMatchObject({
      phase: "unknown",
      alternateScreen: false,
    });
    scanner.push(bytes("\x1b[?25;1049h"));
    expect(scanner.getState().alternateScreen).toBe(true);
  });

  it("stays unknown for a shell that emits no marks", () => {
    const scanner = new PromptScanner();
    scanner.push(bytes("$ gs"));
    expect(scanner.getState()).toEqual({
      phase: "unknown",
      detected: false,
      alternateScreen: false,
    });
  });

  it("ignores unrelated escapes without dropping later marks", () => {
    const scanner = new PromptScanner();
    scanner.push(bytes("\x1b[31mred\x1b[0m"));
    scanner.push(osc("B"));
    expect(scanner.getState().phase).toBe("input");
  });
});

describe("quick command engine", () => {
  const commands = [
    command("c1", "Git status", "git status", "gs"),
    command("c2", "Compose up", "docker compose up -d", "dcu"),
  ];

  function makeEngine(list: QuickCommand[] = commands) {
    const states: Array<{ suggestion: unknown; phase: string }> = [];
    const engine = new QuickCommandEngine({
      getCommands: () => list,
      onChange: (state) =>
        states.push({ suggestion: state.suggestion, phase: state.phase }),
    });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    return { engine, states };
  }

  it("offers a suggestion only after the full abbreviation and forwards typing", () => {
    const { engine } = makeEngine();
    expect(engine.handleInput("g")).toBe("g");
    expect(engine.getState().suggestion).toBeNull();
    expect(engine.handleInput("s")).toBe("s");
    expect(engine.getState().suggestion).toMatchObject({
      commandId: "c1",
      abbreviation: "gs",
      command: "git status",
    });
    expect(engine.handleInput("x")).toBe("x");
    expect(engine.getState().suggestion).toBeNull();
  });

  it("updates on backspace, then suspends after cursor/paste/IME until a new B", () => {
    const { engine } = makeEngine();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
    engine.handleInput("\x7f");
    expect(engine.getState().suggestion).toBeNull();
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();

    expect(engine.handleInput("\x1b[D")).toBe("\x1b[D");
    expect(engine.getState().suggestion).toBeNull();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });

    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
    expect(engine.handleInput("\x1b[200~text\x1b[201~")).toContain("text");
    expect(engine.getState().suggestion).toBeNull();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });

    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
    expect(engine.handleInput("gs")).toBe("gs");
    expect(engine.getState().suggestion).toBeNull();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
  });

  it("forwards Escape, dismisses, and suspends until a new prompt", () => {
    const { engine } = makeEngine();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.handleInput("\x1b")).toBe("\x1b");
    expect(engine.getState().suggestion).toBeNull();
    expect(engine.getState().suspended).toBe(true);
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
  });

  it("clears on Ctrl-C and Enter", () => {
    const { engine } = makeEngine();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.handleInput("\x03")).toBe("\x03");
    expect(engine.getState().suggestion).toBeNull();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.handleInput("\r")).toBe("\r");
    expect(engine.getState().suggestion).toBeNull();

    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
  });

  it("suspends during IME composition and after its commit", () => {
    const { engine } = makeEngine();
    engine.setComposing(true);
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
    engine.setComposing(false);
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
  });

  it("accepts without mutating, then clearTracking suppresses more", () => {
    const { engine } = makeEngine();
    engine.handleInput("g");
    engine.handleInput("s");
    const acceptance = engine.accept("c1");
    expect(acceptance).toEqual({
      erase: 2,
      text: "git status",
      abbreviation: "gs",
    });
    expect(engine.getState().suggestion).not.toBeNull();
    engine.clearTracking();
    expect(engine.getState().suggestion).toBeNull();
  });

  it("refuses to accept after the line moved on", () => {
    const { engine } = makeEngine();
    engine.handleInput("g");
    engine.handleInput("s");
    engine.handleInput("\x1b[D");
    expect(engine.accept("c1")).toBeNull();
  });

  it("never suggests at a running command, password prompt or TUI", () => {
    const { engine } = makeEngine();
    engine.setPrompt({ phase: "running", detected: true, alternateScreen: false });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();

    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
    engine.setPrompt({ phase: "unknown", detected: true, alternateScreen: true });
    expect(engine.getState().suggestion).toBeNull();
  });

  it("stays silent for an unsupported shell (no marks)", () => {
    const { engine } = makeEngine();
    engine.setPrompt({
      phase: "unknown",
      detected: false,
      alternateScreen: false,
    });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState()).toMatchObject({
      phase: "unknown",
      detected: false,
      suggestion: null,
    });
  });

  it("suspends inside Ctrl-R/Ctrl-S search and Tab completion", () => {
    const { engine } = makeEngine();
    expect(engine.handleInput("\x12")).toBe("\x12");
    expect(engine.getState().suggestion).toBeNull();
    expect(engine.getState().suspended).toBe(true);
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();

    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
    expect(engine.handleInput("\t")).toBe("\t");
    expect(engine.getState().suggestion).toBeNull();
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
  });

  it("does not false-match after a history/arrow reset", () => {
    const { engine } = makeEngine();
    engine.handleInput("e");
    engine.handleInput("c");
    engine.handleInput("h");
    engine.handleInput("o");
    engine.handleInput(" ");
    engine.handleInput("f");
    engine.handleInput("o");
    engine.handleInput("o");
    expect(engine.handleInput("\x1b[A")).toBe("\x1b[A");
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
    expect(engine.getState().suspended).toBe(true);
  });

  it("never matches type-ahead queued while a command was running", () => {
    const { engine } = makeEngine();
    engine.setPrompt({ phase: "running", detected: true, alternateScreen: false });
    engine.handleInput("f");
    engine.handleInput("o");
    engine.handleInput("o");
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();

    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    expect(engine.getState().suspended).toBe(true);
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();

    engine.handleInput("\r");
    engine.setPrompt({ phase: "prompt", detected: true, alternateScreen: false });
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
  });

  it("records Enter submitted while suspended, even without a C mark", () => {
    const { engine } = makeEngine();
    engine.handleInput("\x1b[A");
    expect(engine.getState().suspended).toBe(true);
    engine.handleInput("\r");
    expect(engine.getState().submitted).toBe(true);

    engine.handleInput("f");
    engine.handleInput("o");
    engine.handleInput("o");
    engine.setPrompt({ phase: "input", detected: true, alternateScreen: false });
    expect(engine.getState().suspended).toBe(true);
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
  });

  it("does not match a suffix glued to real text from a clean prompt", () => {
    const { engine } = makeEngine();
    for (const char of "foogs") {
      engine.handleInput(char);
    }
    expect(engine.getState().suggestion).toBeNull();
    engine.handleInput(" ");
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).not.toBeNull();
  });

  it("does not guess when stored data is ambiguous", () => {
    const { engine } = makeEngine([
      command("a", "One", "echo one", "gs"),
      command("b", "Two", "echo two", "gs"),
    ]);
    engine.handleInput("g");
    engine.handleInput("s");
    expect(engine.getState().suggestion).toBeNull();
  });
});

class FakeSocket implements TerminalSocket {
  binaryType = "arraybuffer";
  readyState = 0;
  sent: Uint8Array[] = [];
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((_event: { data: unknown }) => void) | null = null;
  onclose: ((_event: { code: number; reason: string }) => void) | null = null;
  onerror: ((_event: unknown) => void) | null = null;

  open(): void {
    this.readyState = 1;
    this.onopen?.();
  }

  message(data: ArrayBuffer): void {
    this.onmessage?.({ data });
  }

  serverClose(code = 1006): void {
    this.readyState = 3;
    this.onclose?.({ code, reason: "" });
  }

  send(data: ArrayBuffer | Uint8Array): void {
    this.sent.push(data instanceof Uint8Array ? data : new Uint8Array(data));
  }

  close(): void {
    this.closed = true;
    this.readyState = 3;
    this.onclose?.({ code: 1000, reason: "" });
  }

  inputs(): string[] {
    return this.sent
      .filter((frame) => frame[0] === CLIENT_INPUT)
      .map((frame) => new TextDecoder().decode(frame.subarray(1)));
  }
}

class FakeTerminal {
  static last: FakeTerminal | null = null;
  cols: number;
  rows: number;
  modes = { bracketedPasteMode: false };
  written: Uint8Array[] = [];
  disposed = false;
  focused = false;
  host: HTMLElement | null = null;
  private dataListeners: Array<(_data: string) => void> = [];
  private resizeListeners: Array<(_size: { cols: number; rows: number }) => void> = [];

  constructor(options: { cols: number; rows: number }) {
    this.cols = options.cols;
    this.rows = options.rows;
    FakeTerminal.last = this;
  }

  open(host: HTMLElement): void {
    this.host = host;
  }

  loadAddon(): void {
  }

  onData(listener: (_data: string) => void): { dispose: () => void } {
    this.dataListeners.push(listener);
    return { dispose: () => this.dataListeners.splice(this.dataListeners.indexOf(listener), 1) };
  }

  onResize(
    listener: (_size: { cols: number; rows: number }) => void,
  ): { dispose: () => void } {
    this.resizeListeners.push(listener);
    return {
      dispose: () => this.resizeListeners.splice(this.resizeListeners.indexOf(listener), 1),
    };
  }

  write(data: string | Uint8Array): void {
    this.written.push(typeof data === "string" ? bytes(data) : data);
  }

  focus(): void {
    this.focused = true;
  }

  dispose(): void {
    this.disposed = true;
  }

  type(data: string): void {
    for (const listener of this.dataListeners) {
      listener(data);
    }
  }

  resize(cols: number, rows: number): void {
    this.cols = cols;
    this.rows = rows;
    for (const listener of this.resizeListeners) {
      listener({ cols, rows });
    }
  }
}

class FakeFit {
  fit(): void {
  }
  dispose(): void {
  }
}

function serverFrame(opcode: number, payload: Uint8Array = new Uint8Array()): ArrayBuffer {
  const frame = new Uint8Array(payload.length + 1);
  frame[0] = opcode;
  frame.set(payload, 1);
  return frame.buffer;
}

function setupRuntime(commands: QuickCommand[] = []) {
  FakeTerminal.last = null;
  let socket: FakeSocket | null = null;
  const states: TerminalStatePatch[] = [];
  const suggestions: Array<{ phase: string; detected: boolean; suggestion: unknown }> = [];
  const runtime = new TerminalRuntime({
    session: {
      id: "session-1",
      websocket_path: "/api/v1/servers/s1/terminal/sessions/session-1/ws",
      cols: 120,
      rows: 32,
    },
    getToken: () => "jwt-token",
    onState: (patch) => states.push(patch),
    quickCommands: () => commands,
    onSuggestion: (state) =>
      suggestions.push({
        phase: state.phase,
        detected: state.detected,
        suggestion: state.suggestion,
      }),
    deps: {
      createSocket: () => {
        socket = new FakeSocket();
        return socket;
      },
      loadTerminal: async () =>
        ({ Terminal: FakeTerminal, FitAddon: FakeFit }) as unknown as TerminalModules,
    },
  });
  return {
    runtime,
    states,
    suggestions,
    get socket() {
      if (!socket) {
        throw new Error("socket not created");
      }
      return socket;
    },
    terminal() {
      if (!FakeTerminal.last) {
        throw new Error("terminal not created");
      }
      return FakeTerminal.last;
    },
  };
}

async function connectedRuntime(commands: QuickCommand[] = []) {
  const context = setupRuntime(commands);
  await context.runtime.connect();
  context.socket.open();
  context.socket.message(
    serverFrame(0x04, bytes(JSON.stringify({
      shell: "/bin/bash",
      cols: 120,
      rows: 32,
      service_user: "gotham-agent",
    }))),
  );
  return context;
}

function output(context: ReturnType<typeof setupRuntime>, data: Uint8Array | string): void {
  const chunk = typeof data === "string" ? bytes(data) : data;
  context.socket.message(serverFrame(0x01, chunk));
}

describe("terminal runtime quick commands", () => {
  const saved = [
    command("c1", "Git status", "git status", "gs"),
    command("c2", "Compose up", "docker compose up -d", "dcu"),
  ];

  it("enables suggestions only after an OSC 133 B mark and mirrors typing", async () => {
    const context = await connectedRuntime(saved);
    expect(context.runtime.getPromptState()).toEqual({
      phase: "unknown",
      detected: false,
      alternateScreen: false,
    });
    output(context, concat([osc("A"), bytes("gotham$ "), osc("B")]));
    expect(context.runtime.getPromptState()).toMatchObject({
      phase: "input",
      detected: true,
    });

    context.terminal().type("g");
    context.terminal().type("s");
    expect(context.socket.inputs()).toEqual(["g", "s"]);
    expect(context.runtime.getSuggestionState().suggestion).toMatchObject({
      commandId: "c1",
    });
    expect(context.suggestions.at(-1)).toMatchObject({
      phase: "input",
      detected: true,
      suggestion: { commandId: "c1" },
    });
  });

  it("accepts with exactly DEL x abbreviation + command and no Enter", async () => {
    const context = await connectedRuntime(saved);
    output(context, concat([osc("B")]));
    context.terminal().type("g");
    context.terminal().type("s");
    context.socket.sent = [];

    const result = context.runtime.acceptSuggestion();
    expect(result).toEqual({ ok: true });
    const inserted = context.socket.inputs().join("");
    expect(inserted).toBe("\x7f\x7fgit status");
    expect(inserted).not.toContain("\r");
    expect(inserted).not.toContain("\n");
    expect(context.runtime.getSuggestionState().suggestion).toBeNull();
  });

  it("does not erase when the command cannot be inserted safely", async () => {
    const multi = [command("m1", "Multi", "echo one\necho two", "mm")];
    const context = await connectedRuntime(multi);
    output(context, concat([osc("B")]));
    context.terminal().type("m");
    context.terminal().type("m");
    context.socket.sent = [];

    expect(context.runtime.acceptSuggestion()).toEqual({
      ok: false,
      failure: "multiline-no-paste",
    });
    expect(context.socket.inputs()).toEqual([]);
  });

  it("inserts multiline text only under bracketed paste, never with Enter", async () => {
    const multi = [command("m1", "Multi", "echo one\necho two")];
    const context = await connectedRuntime(multi);
    context.socket.sent = [];

    expect(context.runtime.insertCommand("echo one\necho two")).toEqual({
      ok: false,
      failure: "multiline-no-paste",
    });
    expect(context.socket.inputs()).toEqual([]);

    context.terminal().modes.bracketedPasteMode = true;
    expect(context.runtime.insertCommand("echo one\necho two")).toEqual({
      ok: true,
    });
    expect(context.socket.inputs()).toEqual([
      "\x1b[200~echo one\necho two\x1b[201~",
    ]);
  });

  it("refuses control characters in a manual insertion", async () => {
    const context = await connectedRuntime();
    context.socket.sent = [];
    expect(context.runtime.insertCommand("echo \x1b[31mred")).toEqual({
      ok: false,
      failure: "control",
    });
    expect(context.socket.inputs()).toEqual([]);
  });

  it("keeps suggestions off at a running command / password prompt", async () => {
    const context = await connectedRuntime(saved);
    output(context, concat([osc("A"), osc("B")]));
    context.terminal().type("g");
    context.terminal().type("s");
    expect(context.runtime.getSuggestionState().suggestion).not.toBeNull();

    context.terminal().type("\r");
    output(context, concat([osc("C"), bytes("[sudo] password for gotham: ")]));
    context.terminal().type("secret");
    expect(context.runtime.getSuggestionState().suggestion).toBeNull();
    expect(context.runtime.getSuggestionState().phase).toBe("running");
  });

  it("keeps suggestions off inside a fullscreen TUI and restores at the prompt", async () => {
    const context = await connectedRuntime(saved);
    output(context, concat([osc("B")]));
    output(context, bytes("\x1b[?1049h"));
    context.terminal().type("g");
    context.terminal().type("s");
    expect(context.runtime.getSuggestionState().suggestion).toBeNull();
    expect(context.runtime.getSuggestionState().alternateScreen).toBe(true);

    output(context, bytes("\x1b[?1049l"));
    output(context, concat([osc("A"), osc("B")]));
    context.terminal().type("g");
    context.terminal().type("s");
    expect(context.runtime.getSuggestionState().suggestion).not.toBeNull();
  });

  it("produces no suggestion for a shell that emits no marks", async () => {
    const context = await connectedRuntime(saved);
    output(context, bytes("$ "));
    context.terminal().type("g");
    context.terminal().type("s");
    expect(context.socket.inputs()).toEqual(["g", "s"]);
    expect(context.runtime.getSuggestionState()).toMatchObject({
      phase: "unknown",
      detected: false,
      suggestion: null,
    });
  });

  it("forwards Escape while dismissing and suspends until a new prompt mark", async () => {
    const context = await connectedRuntime(saved);
    output(context, concat([osc("B")]));
    context.terminal().type("g");
    context.terminal().type("s");
    context.socket.sent = [];
    context.terminal().type("\x1b");
    expect(context.socket.inputs()).toEqual(["\x1b"]);
    expect(context.runtime.getSuggestionState().suggestion).toBeNull();
    context.terminal().type("g");
    context.terminal().type("s");
    expect(context.runtime.getSuggestionState().suggestion).toBeNull();
    output(context, concat([osc("A"), osc("B")]));
    context.terminal().type("g");
    context.terminal().type("s");
    expect(context.runtime.getSuggestionState().suggestion).not.toBeNull();
  });

  it("closes cleanly with a suggestion pending", async () => {
    const context = await connectedRuntime(saved);
    output(context, concat([osc("B")]));
    context.terminal().type("g");
    context.terminal().type("s");
    context.runtime.dispose(true);
    expect(context.runtime.getSuggestionState().suggestion).toBeNull();
    expect(context.terminal().disposed).toBe(true);
    expect(context.socket.closed).toBe(true);
  });
});

describe("quick commands store", () => {
  it("adds, updates, deletes and rejects duplicates", () => {
    setSession({ user: account("u-1"), accessToken: "a", refreshToken: "r" });
    const store = useQuickCommandsStore();
    const saved = store.add({ label: "One", command: "echo one", abbreviation: "o" });
    expect(saved).not.toBeNull();
    expect(store.commands).toHaveLength(1);

    expect(store.add({ label: "Two", command: "echo two", abbreviation: "O" })).toBeNull();
    expect(store.commands).toHaveLength(1);

    expect(store.update(saved!.id, { label: "One", command: "echo 1", abbreviation: "o" })).toBe(true);
    expect(store.commands[0]!.command).toBe("echo 1");

    store.remove(saved!.id);
    expect(store.commands).toEqual([]);
  });

  it("isolates accounts and reloads their list on an in-SPA switch", () => {
    setSession({ user: account("u-1"), accessToken: "a", refreshToken: "r" });
    const store = useQuickCommandsStore();
    store.add({ label: "One", command: "echo one", abbreviation: "o" });

    setSession({ user: account("u-2"), accessToken: "a", refreshToken: "r" });
    expect(store.userId).toBe("u-2");
    expect(store.commands).toEqual([]);

    setSession({ user: account("u-1"), accessToken: "a", refreshToken: "r" });
    expect(store.commands.map((item) => item.label)).toEqual(["One"]);

    setSession({ user: null, accessToken: null, refreshToken: null });
    expect(store.commands).toEqual([]);
  });

  it("clears the list after logout without deleting the stored copy", () => {
    setSession({ user: account("u-1"), accessToken: "a", refreshToken: "r" });
    const store = useQuickCommandsStore();
    store.add({ label: "One", command: "echo one" });
    expect(readQuickCommands(window.localStorage, "u-1")).toHaveLength(1);

    setSession({ user: null, accessToken: null, refreshToken: null });
    expect(store.commands).toEqual([]);
    expect(readQuickCommands(window.localStorage, "u-1")).toHaveLength(1);
  });
});

describe("quick command transport golden bytes", () => {
  it("encodes an acceptance as DELs plus the command text", () => {
    const frame = encodeInput(prepareAcceptance(2, "git status", false).sequence);
    expect(frame[0]).toBe(CLIENT_INPUT);
    expect(new TextDecoder().decode(frame.subarray(1))).toBe("\x7f\x7fgit status");
  });
});
