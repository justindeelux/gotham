// JUS-249 real-shell proof. This test starts a real PTY (Python's `pty`
// module) and a real bash with the shipped, additive rc snippet. The frontend
// scanner and suggestion engine observe the actual OSC 133 bytes, type an
// abbreviation, accept it, and press Enter: the command must run exactly once,
// proving the replacement did not duplicate or lose input. A negative run with
// `sh` (no prompt marks) proves suggestions stay off and ordinary typing still
// reaches the shell. Skips only when bash/sh/python3 are unavailable.
/* global setTimeout:readonly, clearTimeout:readonly, setInterval:readonly, clearInterval:readonly */
import { Buffer } from "node:buffer";
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

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
argv = json.loads(sys.argv[1])
master, slave = pty.openpty()
env = dict(os.environ)
env["TERM"] = "xterm-256color"
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

/** supportsPty reports whether this host can run the proof. */
function supportsPty(): boolean {
  if (!existsSync("/bin/bash") || !existsSync("/bin/sh")) {
    return false;
  }
  const probe = spawnSync("python3", ["-c", "import pty, select"], {
    stdio: "ignore",
  });
  return probe.status === 0;
}

/** PtyProcess is one real PTY session driven through the relay. */
class PtyProcess {
  private readonly child;
  private output = "";
  private waiters: Array<{ match: RegExp; resolve: () => void; reject: (_error: Error) => void; timer: ReturnType<typeof setTimeout> }> = [];

  constructor(argv: string[]) {
    this.child = spawn("python3", ["-c", PYTHON_RELAY, JSON.stringify(argv)], {
      stdio: ["pipe", "pipe", "pipe"],
    });
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

/** waitUntil polls a predicate so the test never trusts a fixed sleep. */
async function waitUntil(predicate: () => boolean, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!predicate()) {
    if (Date.now() > deadline) {
      throw new Error("timed out waiting for a condition");
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}

afterEach(() => {
  for (const session of sessions.splice(0)) {
    session.close();
  }
  for (const dir of tempDirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true });
  }
});

/** command builds one saved command for the engine. */
function command(text: string, abbreviation: string): QuickCommand {
  return { id: "cmd-1", label: "PTY proof", command: text, abbreviation };
}

describe.skipIf(!supportsPty())("real PTY quick-command proof", () => {
  it("bash: abbreviation is replaced and the command runs exactly once", async () => {
    const dir = mkdtempSync(join(tmpdir(), "gotham-pty-"));
    tempDirs.push(dir);
    const rc = join(dir, "bashrc");
    writeFileSync(
      rc,
      ["PS1='gotham$ '", shellIntegrationSnippet("/bin/bash"), ""].join("\n"),
    );

    const session = new PtyProcess([
      "/bin/bash",
      "--noprofile",
      "--rcfile",
      rc,
      "-i",
    ]);
    sessions.push(session);
    // eslint-disable-next-line no-control-regex -- matching the real OSC bytes.
    await session.waitFor(/\x1b\]133;B\x07/);

    // Feed the real shell output into the same scanner the runtime uses.
    const engine = new QuickCommandEngine({
      getCommands: () => [command("printf 'GOTHAM_%s\\n' RAN", "gs")],
    });
    const scanner = new PromptScanner((state) => engine.setPrompt(state));
    let consumed = 0;
    const drain = setInterval(() => {
      const text = session.getText();
      if (text.length > consumed) {
        scanner.push(Buffer.from(text.slice(consumed), "utf8"));
        consumed = text.length;
      }
    }, 20);
    try {
      await waitUntil(() => engine.getState().detected);
      await waitUntil(() => engine.getState().phase === "input");
      expect(engine.getState().detected).toBe(true);

      for (const char of "gs") {
        const forwarded = engine.handleInput(char);
        expect(forwarded).toBe(char);
        session.write(char);
      }
      expect(engine.getState().suggestion).toMatchObject({ commandId: "cmd-1" });

      const acceptance = engine.accept("cmd-1");
      expect(acceptance).not.toBeNull();
      const plan = prepareAcceptance(acceptance!.erase, acceptance!.text, false);
      expect(plan.ok).toBe(true);
      expect(plan.sequence.startsWith("\x7f\x7f")).toBe(true);
      expect(plan.sequence).not.toContain("\r");
      session.write(plan.sequence);
      engine.clearTracking();

      // Wait until readline has redrawn the replaced line, then press Enter.
      await session.waitFor(/GOTHAM_%s/);
      expect(session.getText()).not.toContain("gsprintf");
      session.write("\r");
      await session.waitFor(/GOTHAM_RAN/);
      expect(session.getText().match(/GOTHAM_RAN/g)).toHaveLength(1);
    } finally {
      clearInterval(drain);
    }
  }, 20_000);

  it("sh/dash: no prompt marks means no suggestion, but typing still works", async () => {
    const session = new PtyProcess(["/bin/sh", "-i"]);
    sessions.push(session);
    // A shell without integration has no 133 mark; give it a moment to print
    // its prompt, then prove the scanner never detects one.
    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(session.getText()).not.toContain("\x1b]133;");

    const engine = new QuickCommandEngine({
      getCommands: () => [command("echo SHOULD_NOT_APPEAR", "gs")],
    });
    const scanner = new PromptScanner((state) => engine.setPrompt(state));
    scanner.push(Buffer.from(session.getText(), "utf8"));
    expect(engine.getState()).toMatchObject({
      phase: "unknown",
      detected: false,
      suggestion: null,
    });

    for (const char of "gs") {
      const forwarded = engine.handleInput(char);
      expect(forwarded).toBe(char);
      session.write(char);
    }
    expect(engine.getState().suggestion).toBeNull();
    // Clear the line and run a marker command; the terminal is unaffected.
    session.write("\x15");
    session.write("echo GOTHAM_NEG_OK\r");
    await session.waitFor(/GOTHAM_NEG_OK/);
    expect(session.getText()).not.toContain("SHOULD_NOT_APPEAR");
  }, 20_000);
});
