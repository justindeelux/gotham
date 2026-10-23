/* global TextEncoder:readonly */
import { describe, expect, it } from "vitest";

import {
  CLIENT_CLOSE,
  CLIENT_HEARTBEAT,
  CLIENT_INPUT,
  CLIENT_RESIZE,
  clampDimension,
  decodeServerFrame,
  encodeClose,
  encodeHeartbeat,
  encodeInput,
  encodeResize,
} from "@/features/terminal/transport/frames";

function frame(opcode: number, payload: Uint8Array = new Uint8Array()): Uint8Array {
  const bytes = new Uint8Array(payload.length + 1);
  bytes[0] = opcode;
  bytes.set(payload, 1);
  return bytes;
}

function text(value: string): Uint8Array {
  return new TextEncoder().encode(value);
}

describe("client frames", () => {
  it("prefixes input with 0x01 and keeps raw bytes", () => {
    expect([...encodeInput("ls")]).toEqual([CLIENT_INPUT, 0x6c, 0x73]);
    expect([...encodeInput(new Uint8Array([0xff, 0x00]))]).toEqual([
      CLIENT_INPUT,
      0xff,
      0x00,
    ]);
  });

  it("writes resize as big-endian uint16 columns then rows", () => {
    expect([...encodeResize(120, 32)]).toEqual([
      CLIENT_RESIZE,
      0x00,
      120,
      0x00,
      32,
    ]);
    expect([...encodeResize(1000, 1)]).toEqual([
      CLIENT_RESIZE,
      0x03,
      0xe8,
      0x00,
      0x01,
    ]);
  });

  it("clamps dimensions to the contract band", () => {
    expect(clampDimension(0)).toBe(1);
    expect(clampDimension(2000)).toBe(1000);
    expect(clampDimension(Number.NaN)).toBe(1);
    expect(clampDimension(80.6)).toBe(81);
    expect([...encodeResize(-5, 4000)].slice(1)).toEqual([0, 1, 3, 0xe8]);
  });

  it("builds bare close and heartbeat frames", () => {
    expect([...encodeClose()]).toEqual([CLIENT_CLOSE]);
    expect([...encodeHeartbeat()]).toEqual([CLIENT_HEARTBEAT]);
  });
});

describe("server frames", () => {
  it("returns output bytes untouched, including split UTF-8", () => {
    const partial = new Uint8Array([0x01, 0xe2]);
    const first = decodeServerFrame(partial);
    const second = decodeServerFrame(frame(0x01, new Uint8Array([0x82, 0xac])));
    expect(first).toMatchObject({ kind: "output" });
    expect(second).toMatchObject({ kind: "output" });
    if (first?.kind === "output" && second?.kind === "output") {
      expect([...first.bytes]).toEqual([0xe2]);
      expect([...second.bytes]).toEqual([0x82, 0xac]);
    }
  });

  it("decodes opened, exited, error and ping frames", () => {
    expect(
      decodeServerFrame(
        frame(
          0x04,
          text(
            JSON.stringify({
              shell: "/bin/bash",
              cols: 120,
              rows: 32,
              service_user: "gotham-agent",
            }),
          ),
        ),
      ),
    ).toEqual({
      kind: "opened",
      info: {
        shell: "/bin/bash",
        cols: 120,
        rows: 32,
        service_user: "gotham-agent",
      },
    });
    expect(
      decodeServerFrame(
        frame(0x02, text(JSON.stringify({ exit_code: 130, signal: "" }))),
      ),
    ).toEqual({ kind: "exited", info: { exit_code: 130, signal: "" } });
    expect(decodeServerFrame(frame(0x03, text("node agent offline")))).toEqual({
      kind: "error",
      message: "node agent offline",
    });
    expect(decodeServerFrame(frame(0x05))).toEqual({ kind: "ping" });
  });

  it("accepts an ArrayBuffer delivered by the socket", () => {
    const raw = frame(0x05);
    expect(decodeServerFrame(raw.buffer as ArrayBuffer)).toEqual({ kind: "ping" });
  });

  it("never throws on malformed control frames", () => {
    expect(decodeServerFrame(new Uint8Array())).toBeNull();
    expect(decodeServerFrame(frame(0x04, text("not json")))).toEqual({
      kind: "invalid",
      opcode: 0x04,
    });
    expect(decodeServerFrame(frame(0x02, text("{}")))).toMatchObject({
      kind: "invalid",
    });
    expect(decodeServerFrame(frame(0x09))).toEqual({
      kind: "invalid",
      opcode: 0x09,
    });
  });
});
