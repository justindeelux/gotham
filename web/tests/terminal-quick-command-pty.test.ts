// JUS-249 real-shell proof. Every test starts a real PTY (Python's `pty`
// module) and a real shell, feeds the actual output through the same
// PromptScanner the runtime uses, types through the same QuickCommandEngine,
// and accepts through prepareAcceptance. The bash/zsh runs use the shipped,
// additive rc snippet; `sh` has no marks. Covers the review-r1 matrix: zsh,
// bash vi normal/insert mode, Ctrl-R, history/Tab/arrow resets, word
// boundaries, PS2, password prompts, alternate screen, a failing rc, and the
// no-marks negative. Each suite skips with a warning when its shell is absent.
/* global setTimeout:readonly, clearTimeout:readonly, setInterval:readonly, clearInterval:readonly, console:readonly */
import { Buffer } from "node:buffer";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";
import { PromptScanner } from "@/features/terminal/transport/promptScanner";
import { QuickCommandEngine } from "@/features/terminal/utils/quickCommandEngine";
import {
  prepareAcceptance,
  shellIntegrationSnippet,
} from "@/features/terminal/utils/quickCommandInsertion";

/** PYTHON_RELAY bridges stdin/stdout to a child connected to a real PTY. */
const PYTHON_RELAY = String.raw`
import json, os, pty, select, subprocess, sys
payload = json.loads(sys.argv[1])
argv = payload["argv"]
signal_int = payload.get("signal_int", False)
master, slave = pty.openpty()
env = dict(os.environ)
env["TERM"] = "xterm-256color"
env.update(payload.get("env", {}))
proc = subprocess.Popen(
    argv,
    stdin=slave,
    stdout=slave,
    stderr=slave,
    preexec_fn=os.setsid,
    env=env,
    close_fds=True,
)
os.close(slave)
inp = sys.stdin.buffer
out = sys.stdout.buffer
try:
    while True:
        ready, _, _ = select.select([master, inp], [], [], 0.1)
        if inp in ready:
            data = os.read(inp.fileno(), 4096)
            if not data:
                break
            # A real terminal sends SIGINT for Ctrl-C when ISIG is enabled;
            # the relay's pipe side cannot do that implicitly.
            if signal_int and b"\x03" in data:
                os.killpg(os.getpgid(proc.pid), 2)
            else:
                os.write(master, data)
        if master in ready:
            try:
                data = os.read(master, 65536)
            except OSError:
                break
            if not data:
                break
            out.write(data)
            out.flush()
finally:
    try:
        os.killpg(os.getpgid(proc.pid), 9)
    except OSError:
        pass
`;

/** findShell returns the first existing absolute path, or "". */
function findShell(candidates: string[]): string {
  return candidates.find((candidate) => existsSync(candidate)) ?? "";
}

/** supportsPty reports whether Python can open a pty on this host. */
function supportsPty(): boolean {
  if (findShell(["/bin/sh", "/usr/bin/sh"]) === "") {
    return false;
  }
  const probe = spawnSync("python3", ["-c", "import pty, select"], {
    stdio: "ignore",
  });
  return probe.status === 0;
}

const hasPty = supportsPty();
const bashPath = findShell(["/bin/bash", "/usr/bin/bash", "/usr/local/bin/bash"]);const zshPath = findShell([
  "/bin/zsh",
  "/usr/bin/zsh",
  "/usr/local/bin/zsh",
  "/opt/homebrew/bin/zsh",
]);
const shPath = findShell(["/bin/sh", "/usr/bin/sh"]);

// Real PTY startup and prompt transitions can exceed Vitest's 5s default.
vi.setConfig({ testTimeout: 15_000 });

if (!hasPty) {
  console.warn("[quick-command-pty] python3/pty unavailable; all PTY proofs skipped");
}
if (!bashPath) {
  console.warn("[quick-command-pty] bash not found; bash proofs skipped");
}
if (!zshPath) {
  console.warn("[quick-command-pty] zsh not found; zsh proof skipped");
}

/** PtyProcess is one real PTY session driven through the relay. */
class PtyProcess {
  private readonly child;
  private output = "";
  private waiters: Array<{ match: RegExp; resolve: () => void; reject: (_error: Error) => void; timer: ReturnType<typeof setTimeout> }> = [];

  constructor(argv: string[], env: Record<string, string> = {}, signalInt = false) {
    this.child = spawn(
      "python3",
      ["-c", PYTHON_RELAY, JSON.stringify({ argv, env, signal_int: signalInt })],
      { stdio: ["pipe", "pipe", "pipe"] },
    );
    this.child.stdout?.on("data", (chunk: Buffer) => {
      this.output += chunk.toString("utf8");
      this.checkWaiters();
    });
  }

  /** write sends raw bytes to the PTY. */
  write(data: string): void {
    this.child.stdin?.write(data);
  }

  /** text returns everything received so far. */
  getText(): string {
    return this.output;
  }

  /** waitFor resolves once the accumulated output matches. */
  waitFor(match: RegExp, timeoutMs = 5000): Promise<void> {
    if (match.test(this.output)) {
      return Promise.resolve();
    }
    return new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiters = this.waiters.filter((waiter) => waiter.timer !== timer);
        reject(new Error(`timed out waiting for ${match}; output=${JSON.stringify(this.output)}`));
      }, timeoutMs);
      this.waiters.push({ match, resolve, reject, timer });
    });
  }

  /** checkWaiters resolves every pattern that now matches. */
  private checkWaiters(): void {
    const pending = this.waiters;
    this.waiters = [];
    for (const waiter of pending) {
      if (waiter.match.test(this.output)) {
        clearTimeout(waiter.timer);
        waiter.resolve();
      } else {
        this.waiters.push(waiter);
      }
    }
  }

  /** close kills the relay and its process group. */
  close(): void {
    for (const waiter of this.waiters) {
      clearTimeout(waiter.timer);
      waiter.reject(new Error("pty closed"));
    }
    this.waiters = [];
    this.child.kill("SIGKILL");
  }
}

/** sessions tracks every live process for teardown. */
const sessions: PtyProcess[] = [];
let tempDirs: string[] = [];

afterEach(() => {
  for (const session of sessions.splice(0)) {
    session.close();
  }
  for (const dir of tempDirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true });
  }
});

/** sleep pauses without fake timers. */
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** countMatches counts non-overlapping occurrences of a literal needle. */
function countMatches(haystack: string, needle: string): number {
  let count = 0;
  let index = 0;
  while ((index = haystack.indexOf(needle, index)) !== -1) {
    count += 1;
    index += needle.length;
  }
  return count;
}

/** command builds one saved command for the engine. */
function command(text: string, abbreviation: string): QuickCommand {
  return { id: "cmd-1", label: "PTY proof", command: text, abbreviation };
}

/** ShellSession wires a real PTY to the production scanner and engine. */
class ShellSession {
  readonly engine: QuickCommandEngine;
  readonly scanner: PromptScanner;
  private readonly pty: PtyProcess;
  private consumed = 0;
  private readonly drain: ReturnType<typeof setInterval>;

  constructor(
    argv: string[],
    commands: QuickCommand[],
    env: Record<string, string> = {},
    signalInt = false,
  ) {
    this.engine = new QuickCommandEngine({ getCommands: () => commands });
    this.scanner = new PromptScanner((state) => this.engine.setPrompt(state));
    this.pty = new PtyProcess(argv, env, signalInt);
    sessions.push(this.pty);
    this.drain = setInterval(() => this.scan(), 10);
  }

  /** scan feeds newly arrived output through the real scanner. */
  scan(): void {
    const text = this.pty.getText();
    if (text.length > this.consumed) {
      this.scanner.push(Buffer.from(text.slice(this.consumed), "utf8"));
      this.consumed = text.length;
    }
  }

  /** waitUntil polls after scanning each newly arrived chunk. */
  async waitUntil(predicate: () => boolean, timeoutMs = 6000): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    while (!predicate()) {
      this.scan();
      if (Date.now() > deadline) {
        throw new Error(
          `timed out waiting for a condition; output=${JSON.stringify(this.pty.getText())}`,
        );
      }
      await sleep(10);
    }
  }

  /** waitForPrompt waits for a NEW B mark and the engine's input phase. */
  async waitForPrompt(): Promise<void> {
    const start = this.pty.getText().length;
    await this.waitUntil(() => {
      const tail = this.pty.getText().slice(start);
      // eslint-disable-next-line no-control-regex -- matching the real OSC bytes.
      return /\x1b\](133|633);B\x07/.test(tail) &&
        this.engine.getState().phase === "input";
    });
  }

  /** waitFor delegates to the raw output accumulator. */
  waitFor(match: RegExp, timeoutMs = 5000): Promise<void> {
    return this.pty.waitFor(match, timeoutMs);
  }

  /** text returns the raw PTY output received so far. */
  text(): string {
    return this.pty.getText();
  }

  /** key runs one onData chunk through the engine and writes what it returns. */
  key(data: string): void {
    this.scan();
    const forwarded = this.engine.handleInput(data);
    if (forwarded !== null) {
      this.pty.write(forwarded);
    }
  }

  /** typeText sends printable text one key at a time. */
  typeText(text: string): void {
    for (const char of text) {
      this.key(char);
    }
  }

  /** run types a command and presses Enter. */
  run(text: string): void {
    this.typeText(text);
    this.key("\r");
  }

  /** accept replaces the visible suggestion and returns the sent sequence. */
  accept(commandId = "cmd-1"): string {
    const acceptance = this.engine.accept(commandId);
    if (!acceptance) {
      throw new Error("no suggestion to accept");
    }
    const plan = prepareAcceptance(acceptance.erase, acceptance.text, false);
    if (!plan.ok) {
      throw new Error(`insertion refused: ${plan.failure}`);
    }
    this.pty.write(plan.sequence);
    this.engine.clearTracking();
    return plan.sequence;
  }

  /** expectNoSuggestion gives late marks a chance, then asserts silence. */
  async expectNoSuggestion(): Promise<void> {
    await sleep(100);
    this.scan();
    expect(this.engine.getState().suggestion).toBeNull();
  }

  /** stop clears the drain interval (pty teardown is in afterEach). */
  stop(): void {
    clearInterval(this.drain);
  }
}

/** makeBashRc writes an rc file with a visible prompt and the shipped marks. */
function makeBashRc(extra = ""): string {
  const dir = mkdtempSync(join(tmpdir(), "gotham-pty-"));
  tempDirs.push(dir);
  const rc = join(dir, "bashrc");
  writeFileSync(
    rc,
    [
      "PS1='gotham$ '",
      extra,
      shellIntegrationSnippet("/bin/bash"),
      "",
    ].join("\n"),
  );
  return rc;
}

/** bashSession starts bash with one rc file. */
function bashSession(
  commands: QuickCommand[],
  rc: string,
  env: Record<string, string> = {},
): ShellSession {
  return new ShellSession(
    [bashPath, "--noprofile", "--rcfile", rc, "-i"],
    commands,
    env,
  );
}

/** startBash starts a normal integrated bash and waits for its prompt. */
async function startBash(
  commands: QuickCommand[],
  extraRc = "",
): Promise<ShellSession> {
  const session = bashSession(commands, makeBashRc(extraRc));
  await session.waitForPrompt();
  return session;
}

/** makeZshRc writes a ZDOTDIR/.zshrc with a visible prompt and marks. */
function makeZshRc(): { dir: string; zdotdir: string } {
  const dir = mkdtempSync(join(tmpdir(), "gotham-zsh-"));
  tempDirs.push(dir);
  writeFileSync(
    join(dir, ".zshrc"),
    [
      "PROMPT='gotham$ '",
      shellIntegrationSnippet("/bin/zsh"),
      "",
    ].join("\n"),
  );
  return { dir, zdotdir: dir };
}

/** resetLine cancels any half-typed line and waits for a fresh prompt. */
async function resetLine(session: ShellSession): Promise<void> {
  session.key("\x03");
  await session.waitForPrompt();
}

describe.skipIf(!hasPty || bashPath === "")("real PTY: bash", () => {
  it("replaces an abbreviation and runs the command exactly once", async () => {
    const session = await startBash([
      command("printf 'GOTHAM_%s\\n' RAN", "gs"),
    ]);
    try {
      expect(session.engine.getState().detected).toBe(true);
      expect(session.engine.getState().phase).toBe("input");

      session.typeText("gs");
      expect(session.engine.getState().suggestion).toMatchObject({ commandId: "cmd-1" });
      const sequence = session.accept();
      expect(sequence.startsWith("\x7f\x7f")).toBe(true);
      expect(sequence).not.toContain("\r");

      await session.waitFor(/GOTHAM_%s/);
      expect(session.text()).not.toContain("gsprintf");
      session.key("\r");
      await session.waitFor(/GOTHAM_RAN/);
      expect(session.text().match(/GOTHAM_RAN/g)).toHaveLength(1);
    } finally {
      session.stop();
    }
  });

  it("vi mode: no suggestion in normal mode, replacement works in insert mode", async () => {
    const session = await startBash(
      [command("printf 'VI_%s\\n' OK", "gs")],
      "set -o vi",
    );
    try {
      // Insert mode: build an inert line, then Esc to vi normal mode.
      session.typeText("echo X ");
      session.key("\x1b");
      expect(session.engine.getState().suspended).toBe(true);
      session.typeText("gs");
      await session.expectNoSuggestion();
      // The mint bytes were forwarded; normal-mode commands are ignored by
      // the engine and the line is cleared before the positive check.
      await resetLine(session);

      // A fresh prompt is insert mode: the same abbreviation is safe.
      session.typeText("gs");
      expect(session.engine.getState().suggestion).toMatchObject({ commandId: "cmd-1" });
      session.accept();
      await session.waitFor(/VI_%s/);
      session.key("\r");
      await session.waitFor(/VI_OK/);
      expect(session.text().match(/VI_OK/g)).toHaveLength(1);
      expect(session.text()).not.toContain("echoecho");
    } finally {
      session.stop();
    }
  });

  it("Ctrl-R search never suggests and a fresh prompt recovers", async () => {
    const session = await startBash([command("printf 'SEARCH_%s\\n' OK", "gs")]);
    try {
      session.typeText("echo foo");
      session.key("\x12");
      expect(session.engine.getState().suspended).toBe(true);
      session.typeText("gs");
      await session.expectNoSuggestion();
      await resetLine(session);

      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      session.key("\r");
      await session.waitFor(/SEARCH_OK/);
    } finally {
      session.stop();
    }
  });

  it("history/Tab/arrow resets cannot false-match a glued suffix", async () => {
    const session = await startBash([command("printf 'RESET_%s\\n' OK", "gs")]);
    try {
      // Clean line: `foogs` is glued to real text, so no match.
      session.typeText("foogs");
      await session.expectNoSuggestion();
      await resetLine(session);

      // History recall (Up) resets the mirror.
      session.typeText("echo foo");
      session.key("\x1b[A");
      session.typeText("gs");
      await session.expectNoSuggestion();
      await resetLine(session);

      // Tab completion resets the mirror.
      session.typeText("echo foo");
      session.key("\t");
      session.typeText("gs");
      await session.expectNoSuggestion();
      await resetLine(session);

      // Cursor movement resets the mirror.
      session.typeText("echo foo");
      session.key("\x05");
      session.typeText("gs");
      await session.expectNoSuggestion();
      await resetLine(session);

      // A clean prompt still works.
      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      session.key("\r");
      await session.waitFor(/RESET_OK/);
    } finally {
      session.stop();
    }
  });

  it("PS2 continuation has no B mark and no suggestion", async () => {
    const session = await startBash([command("printf 'PS2_%s\\n' OK", "gs")]);
    try {
      session.typeText('echo "');
      session.key("\r");
      await session.waitFor(/> /);
      session.typeText("gs");
      await session.expectNoSuggestion();
      await resetLine(session);

      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      session.key("\r");
      await session.waitFor(/PS2_OK/);
    } finally {
      session.stop();
    }
  });

  it("a password prompt has no B mark and no suggestion", async () => {
    const session = await startBash([command("printf 'PW_%s\\n' OK", "gs")]);
    try {
      session.run("read -s -p 'PW: ' x");
      await session.waitFor(/PW: /);
      session.typeText("gs");
      await session.expectNoSuggestion();
      // A shell without PS0 (older bash) cannot emit C; Enter already
      // suspended the mirror, which is the property that matters.
      expect(session.engine.getState().suspended).toBe(true);
      session.key("\r");
      await session.waitForPrompt();

      // The password type-ahead may have been consumed by `read`; the next
      // prompt is conservatively tainted. Submit the empty line once more.
      session.key("\r");
      await session.waitForPrompt();

      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      session.key("\r");
      await session.waitFor(/PW_OK/);
    } finally {
      session.stop();
    }
  });

  it("alternate screen disables suggestions until the prompt returns", async () => {
    const session = await startBash([command("printf 'ALT_%s\\n' OK", "gs")]);
    try {
      session.run("printf '\\033[?1049h'");
      await session.waitUntil(() => session.engine.getState().alternateScreen);
      session.typeText("gs");
      await session.expectNoSuggestion();
      // Clear the inert `gs` from the line before leaving the alternate screen.
      session.key("\x15");

      session.run("printf '\\033[?1049l'");
      await session.waitForPrompt();
      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      session.key("\r");
      await session.waitFor(/ALT_OK/);
    } finally {
      session.stop();
    }
  });

  it("a failing rc file leaves the terminal usable and suggestions off", async () => {
    const dir = mkdtempSync(join(tmpdir(), "gotham-badrc-"));
    tempDirs.push(dir);
    const rc = join(dir, "bashrc");
    writeFileSync(rc, "echo GOTHAM_BAD_RC >&2\nreturn 1\n");
    const session = bashSession([command("echo NEVER", "gs")], rc);
    try {
      await session.waitFor(/GOTHAM_BAD_RC/);
      await sleep(200);
      session.scan();
      expect(session.engine.getState().detected).toBe(false);
      session.typeText("gs");
      await session.expectNoSuggestion();
      session.run("echo RC_OK");
      await session.waitFor(/RC_OK/);
    } finally {
      session.stop();
    }
  });

  it("type-ahead during a running command cannot false-match after the prompt", async () => {
    const session = await startBash([command("printf 'TA_%s\\n' OK", "gs")]);
    try {
      // Submit a slow command, then type `foogs` while it still runs. That
      // text is queued for the next prompt, so the next B is tainted.
      session.run("sleep 0.6");
      session.typeText("foogs");
      await session.waitForPrompt();
      session.typeText("gs");
      await session.expectNoSuggestion();
      expect(session.engine.getState().suspended).toBe(true);

      // Submit the type-ahead, then the following prompt is clean.
      session.key("\r");
      await session.waitForPrompt();
      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      session.key("\r");
      await session.waitFor(/TA_OK/);
    } finally {
      session.stop();
    }
  });

  it("is idempotent when the same rc snippet is sourced twice", async () => {
    const dir = mkdtempSync(join(tmpdir(), "gotham-twice-"));
    tempDirs.push(dir);
    const rc = join(dir, "bashrc");
    const snippet = shellIntegrationSnippet("/bin/bash");
    writeFileSync(rc, ["PS1='gotham$ '", snippet, snippet, ""].join("\n"));
    const session = bashSession(
      [command("printf 'TWICE_%s\\n' OK", "gs")],
      rc,
    );
    try {
      await session.waitForPrompt();
      const startup = session.text();
      expect(countMatches(startup, "\x1b]133;A\x07")).toBe(1);
      expect(countMatches(startup, "\x1b]133;B\x07")).toBe(1);

      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      await session.waitFor(/TWICE_%s/);
      session.key("\r");
      await session.waitFor(/TWICE_OK/);
      // PS0 exists only on bash >= 4.4. Where it exists, sourcing twice must
      // still emit exactly one C; on older bash there is no C at all.
      const cCount = countMatches(session.text(), "\x1b]133;C\x07");
      expect(cCount).toBeLessThanOrEqual(1);
      if (session.text().includes("\x1b]133;C\x07")) {
        expect(cCount).toBe(1);
      }
    } finally {
      session.stop();
    }
  });
});

describe.skipIf(!hasPty || zshPath === "")("real PTY: zsh", () => {
  it("emits marks without errors, then replaces and runs once", async () => {
    const { dir, zdotdir } = makeZshRc();
    const session = new ShellSession(
      [zshPath, "-i"],
      [command("printf 'ZSH_%s\\n' OK", "gs")],
      { ZDOTDIR: zdotdir, HOME: dir, skip_global_compinit: "1" },
    );
    try {
      await session.waitForPrompt();
      const startup = session.text();
      expect(startup).not.toContain("read-only variable");
      expect(session.engine.getState().detected).toBe(true);

      session.typeText("gs");
      expect(session.engine.getState().suggestion).toMatchObject({ commandId: "cmd-1" });
      session.accept();
      await session.waitFor(/ZSH_%s/);
      session.key("\r");
      await session.waitFor(/ZSH_OK/);
      expect(session.text().match(/ZSH_OK/g)).toHaveLength(1);
      expect(session.text()).not.toContain("read-only variable");
    } finally {
      session.stop();
    }
  });

  it("re-emits the B mark after Ctrl-C so suggestions recover", async () => {
    const { dir, zdotdir } = makeZshRc();
    const session = new ShellSession(
      [zshPath, "-i"],
      [command("printf 'ZLE_%s\\n' OK", "gs")],
      { ZDOTDIR: zdotdir, HOME: dir, skip_global_compinit: "1" },
      true,
    );
    try {
      await session.waitForPrompt();
      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      // Let zsh finish processing the typed keys before interrupting.
      await sleep(150);

      // Ctrl-C cancels the line; zle-line-init must emit a fresh B. Wait for
      // the engine to leave the suspended state, not merely for a known phase.
      session.key("\x03");
      await sleep(150);
      await session.waitUntil(
        () =>
          session.engine.getState().phase === "input" &&
          !session.engine.getState().suspended,
      );
      session.typeText("gs");
      expect(session.engine.getState().suggestion).not.toBeNull();
      session.accept();
      session.key("\r");
      await session.waitFor(/ZLE_OK/);
    } finally {
      session.stop();
    }
  });
});

describe.skipIf(!hasPty || shPath === "")("real PTY: sh/dash (no marks)", () => {
  it("never suggests, and ordinary typing still reaches the shell", async () => {
    const session = new ShellSession(
      [shPath, "-i"],
      [command("echo SHOULD_NOT_APPEAR", "gs")],
    );
    try {
      await sleep(250);
      expect(session.engine.getState()).toMatchObject({
        phase: "unknown",
        detected: false,
        suggestion: null,
      });

      session.typeText("gs");
      await session.expectNoSuggestion();
      // Clear the line and run a marker; the terminal is unaffected.
      session.key("\x15");
      session.run("echo GOTHAM_NEG_OK");
      await session.waitFor(/GOTHAM_NEG_OK/);
      expect(session.text()).not.toContain("SHOULD_NOT_APPEAR");
    } finally {
      session.stop();
    }
  });
});
