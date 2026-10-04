#!/usr/bin/env python3
"""pty-drive.py: drive an interactive child on a pty, waiting for each prompt.

Usage:
    python3 pty-drive.py <script> -- <cmd> [args ...]
    python3 pty-drive.py --no-ctty -- <cmd> [args ...]

The script file holds one stanza per line (blank lines and lines starting
with '#' are ignored):
    EXPECT <python-regex>   wait until the child's output matches
    SEND <literal-text>     write the text plus a newline (empty = bare newline)
    SLEEP <seconds>         pause (lets the test prove nothing happens yet)
    EXPECT-EOF              wait for the child to exit

Every SEND goes out only after its EXPECT matched, so nothing is typed ahead
and the kernel never echoes secrets while echo is still on. The child's
output is forwarded to this process's stdout verbatim. The exit status is
the child's (3 on an EXPECT timeout).

With --no-ctty the child runs with the pty as stdio but in a new session
without a controlling terminal (setsid before exec), so opening /dev/tty
fails although [ -t ] is true. No interaction script is used in that mode.

Standard library only (pty, select, os, sys, re, time): python3 is already
required by the installer test suite.
"""

import os
import pty
import re
import select
import sys
import time

CHUNK = 65536
SELECT_TIMEOUT = 0.2


def fail(msg):
    sys.stderr.write("pty-drive: %s\n" % msg)
    sys.exit(3)


def load_script(path):
    stanzas = []
    with open(path, "r", encoding="utf-8") as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line.strip() or line.startswith("#"):
                continue
            kind, _, rest = line.partition(" ")
            if kind not in ("EXPECT", "SEND", "SLEEP", "EXPECT-EOF"):
                fail("bad stanza %r in %s" % (line, path))
            stanzas.append((kind, rest))
    return stanzas


def pump(master, out, deadline):
    """Read one chunk of child output, forwarding it verbatim.

    Returns the decoded chunk, "" on EOF, or None on timeout.
    """
    while True:
        if time.monotonic() > deadline:
            return None
        ready, _, _ = select.select([master], [], [], SELECT_TIMEOUT)
        if not ready:
            continue
        try:
            chunk = os.read(master, CHUNK)
        except OSError:
            return ""
        if not chunk:
            return ""
        out.write(chunk)
        out.flush()
        return chunk.decode("utf-8", "replace")


def run_interactive(cmd, stanzas, timeout):
    deadline = time.monotonic() + timeout
    pid, master = pty.fork()
    if pid == 0:
        os.execvp(cmd[0], cmd)
    out = sys.stdout.buffer
    if hasattr(out, "reconfigure"):
        try:
            out.reconfigure(write_through=True)
        except Exception:
            pass
    status = 0
    try:
        # Unmatched output. A successful EXPECT consumes through the end of
        # its match, so a repeated prompt never matches stale text and every
        # SEND waits for a fresh prompt.
        pending = ""
        idx = 0
        while idx < len(stanzas):
            kind, rest = stanzas[idx]
            if kind == "SLEEP":
                time.sleep(float(rest))
                idx += 1
                continue
            if kind == "EXPECT-EOF":
                while True:
                    chunk = pump(master, out, deadline)
                    if chunk is None:
                        fail("timed out waiting for EOF")
                    if chunk == "":
                        break
                idx += 1
                continue
            try:
                pattern = re.compile(rest, re.S)
            except re.error as err:
                fail("bad regex %r: %s" % (rest, err))
            while True:
                match = pattern.search(pending)
                if match:
                    pending = pending[match.end():]
                    break
                chunk = pump(master, out, deadline)
                if chunk is None:
                    fail("timed out waiting for %r" % rest)
                if chunk == "":
                    fail("EOF while waiting for %r" % rest)
                pending += chunk
            idx += 1
            while idx < len(stanzas) and stanzas[idx][0] == "SEND":
                os.write(master, (stanzas[idx][1] + "\n").encode("utf-8"))
                idx += 1
        _, status = os.waitpid(pid, 0)
    finally:
        try:
            os.close(master)
        except OSError:
            pass
    if os.WIFEXITED(status):
        sys.exit(os.WEXITSTATUS(status))
    sys.exit(1)


def run_no_ctty(cmd, timeout):
    deadline = time.monotonic() + timeout
    parent, child = pty.openpty()
    pid = os.fork()
    if pid == 0:
        try:
            os.setsid()
            os.dup2(child, 0)
            os.dup2(child, 1)
            os.dup2(child, 2)
            os.close(parent)
            os.close(child)
            os.execvp(cmd[0], cmd)
        finally:
            os._exit(127)
    os.close(child)
    out = sys.stdout.buffer
    status = 0
    try:
        while True:
            if time.monotonic() > deadline:
                fail("timed out")
            ready, _, _ = select.select([parent], [], [], SELECT_TIMEOUT)
            if ready:
                try:
                    chunk = os.read(parent, CHUNK)
                except OSError:
                    break
                if not chunk:
                    break
                out.write(chunk)
                out.flush()
            done, status = os.waitpid(pid, os.WNOHANG)
            if done:
                # Drain what is left, then leave.
                while True:
                    ready, _, _ = select.select([parent], [], [], 0.1)
                    if not ready:
                        break
                    try:
                        chunk = os.read(parent, CHUNK)
                    except OSError:
                        break
                    if not chunk:
                        break
                    out.write(chunk)
                    out.flush()
                break
    finally:
        try:
            os.close(parent)
        except OSError:
            pass
    if os.WIFEXITED(status):
        sys.exit(os.WEXITSTATUS(status))
    sys.exit(1)


def main(argv):
    timeout = 300.0
    no_ctty = False
    rest = list(argv)
    while rest and rest[0].startswith("--"):
        opt = rest.pop(0)
        if opt == "--no-ctty":
            no_ctty = True
        elif opt == "--":
            break
        elif opt.startswith("--timeout="):
            timeout = float(opt.split("=", 1)[1])
        else:
            fail("unknown option %s" % opt)
    if no_ctty:
        if not rest:
            fail("usage: pty-drive.py --no-ctty -- <cmd> [args ...]")
        run_no_ctty(rest, timeout)
    else:
        if len(rest) < 2 or rest[1] != "--":
            fail("usage: pty-drive.py <script> -- <cmd> [args ...]")
        run_interactive(rest[2:], load_script(rest[0]), timeout)


main(sys.argv[1:])
