// Unit tests for the log viewer formatting helpers (JUS-24 split of LogViewer).

import { describe, expect, it } from "vitest";

import {
  formatTimestamp,
  noticeText,
  splitLines,
} from "../src/features/servers/utils/logFormat";

describe("splitLines", () => {
  it("breaks chunks on newlines and drops one trailing newline", () => {
    expect(splitLines("a\nb\n")).toEqual(["a", "b"]);
    expect(splitLines("a\r\nb")).toEqual(["a", "b"]);
    expect(splitLines("single")).toEqual(["single"]);
  });
});

describe("formatTimestamp", () => {
  it("passes through HH:MM:SS strings", () => {
    expect(formatTimestamp("12:34:56", 0)).toBe("12:34:56");
  });

  it("renders epoch seconds and millis as HH:MM:SS", () => {
    const at = new Date("2026-03-01T12:34:56Z").getTime();
    expect(formatTimestamp(Math.floor(at / 1000), 0)).toBe(
      new Date(at).toTimeString().slice(0, 8),
    );
    expect(formatTimestamp(at, 0)).toBe(new Date(at).toTimeString().slice(0, 8));
  });

  it("falls back to the receive time for missing or invalid stamps", () => {
    const at = new Date("2026-03-01T01:02:03Z").getTime();
    const expected = new Date(at).toTimeString().slice(0, 8);
    expect(formatTimestamp(null, at)).toBe(expected);
    expect(formatTimestamp("not-a-date", at)).toBe(expected);
  });
});

describe("noticeText", () => {
  it("prefers the server's own notice copy", () => {
    expect(
      noticeText({ raw: "", channel: "c", kind: "notice", receivedAt: 0, payload: { message: "boom" } }),
    ).toBe("boom");
  });

  it("falls back to the reconnect copy", () => {
    expect(
      noticeText({ raw: "", channel: "c", kind: "notice", receivedAt: 0, payload: {} }),
    ).toBe("Log stream interrupted; reconnecting…");
  });
});
