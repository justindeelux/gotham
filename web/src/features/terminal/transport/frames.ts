/**
 * Terminal WebSocket framing (docs/terminal-session-api.md, JUS-243).
 *
 * Every frame is binary and starts with one opcode byte. Input and output
 * carry raw PTY bytes, so they are never decoded as text: a UTF-8 code point
 * split across frames must reach xterm unchanged. These helpers are pure and
 * DOM-free so the framing contract is tested without a browser.
 */

/** CLIENT_INPUT carries raw keystrokes to the PTY. */
export const CLIENT_INPUT = 0x01 as const;
/** CLIENT_RESIZE carries columns/rows as uint16 big-endian. */
export const CLIENT_RESIZE = 0x02 as const;
/** CLIENT_CLOSE asks the server to end the session. */
export const CLIENT_CLOSE = 0x03 as const;
/** CLIENT_HEARTBEAT keeps a quiet session alive (the server allows 60s). */
export const CLIENT_HEARTBEAT = 0x04 as const;

/** SERVER_OUTPUT carries raw PTY output bytes. */
export const SERVER_OUTPUT = 0x01 as const;
/** SERVER_EXITED ends the session with `{exit_code, signal}`. */
export const SERVER_EXITED = 0x02 as const;
/** SERVER_ERROR ends the session with a bounded message. */
export const SERVER_ERROR = 0x03 as const;
/** SERVER_OPENED confirms the PTY with shell/cols/rows/service_user. */
export const SERVER_OPENED = 0x04 as const;
/** SERVER_PING is the server's 25-second liveness heartbeat. */
export const SERVER_PING = 0x05 as const;

/** OpenedInfo is the JSON payload of an `opened` frame. */
export interface OpenedInfo {
  shell: string;
  cols: number;
  rows: number;
  service_user: string;
}

/** ExitedInfo is the JSON payload of an `exited` frame. */
export interface ExitedInfo {
  exit_code: number;
  signal: string;
}

/** ServerFrame is a decoded server→client frame. */
export type ServerFrame =
  | { kind: "output"; bytes: Uint8Array }
  | { kind: "opened"; info: OpenedInfo }
  | { kind: "exited"; info: ExitedInfo }
  | { kind: "error"; message: string }
  | { kind: "ping" }
  | { kind: "invalid"; opcode: number };

/** encodeInput wraps raw keystroke bytes in an input frame. */
export function encodeInput(data: string | Uint8Array): Uint8Array {
  const bytes = typeof data === "string" ? new TextEncoder().encode(data) : data;
  const frame = new Uint8Array(bytes.length + 1);
  frame[0] = CLIENT_INPUT;
  frame.set(bytes, 1);
  return frame;
}

/** encodeResize wraps columns/rows in a big-endian resize frame. */
export function encodeResize(cols: number, rows: number): Uint8Array {
  const frame = new Uint8Array(5);
  frame[0] = CLIENT_RESIZE;
  const view = new DataView(frame.buffer);
  view.setUint16(1, clampDimension(cols), false);
  view.setUint16(3, clampDimension(rows), false);
  return frame;
}

/** encodeClose builds the close frame. */
export function encodeClose(): Uint8Array {
  return new Uint8Array([CLIENT_CLOSE]);
}

/** encodeHeartbeat builds the liveness frame. */
export function encodeHeartbeat(): Uint8Array {
  return new Uint8Array([CLIENT_HEARTBEAT]);
}

/** clampDimension keeps a terminal dimension inside the contract's 1-1000. */
export function clampDimension(value: number): number {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.min(1000, Math.max(1, Math.round(value)));
}

/**
 * decodeServerFrame parses one binary server frame. Output bytes are returned
 * verbatim; a malformed control frame becomes `invalid` rather than throwing,
 * so one bad frame cannot tear down the socket handler.
 */
export function decodeServerFrame(
  data: ArrayBuffer | Uint8Array,
): ServerFrame | null {
  const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
  if (bytes.length === 0) {
    return null;
  }
  const opcode = bytes[0]!;
  const payload = bytes.subarray(1);
  switch (opcode) {
    case SERVER_OUTPUT:
      return { kind: "output", bytes: payload };
    case SERVER_PING:
      return { kind: "ping" };
    case SERVER_EXITED: {
      const info = parseJson<ExitedInfo>(payload);
      if (
        info === null ||
        typeof info.exit_code !== "number" ||
        typeof info.signal !== "string"
      ) {
        return { kind: "invalid", opcode };
      }
      return { kind: "exited", info };
    }
    case SERVER_OPENED: {
      const info = parseJson<OpenedInfo>(payload);
      if (
        info === null ||
        typeof info.shell !== "string" ||
        typeof info.service_user !== "string" ||
        typeof info.cols !== "number" ||
        typeof info.rows !== "number"
      ) {
        return { kind: "invalid", opcode };
      }
      return { kind: "opened", info };
    }
    case SERVER_ERROR:
      return { kind: "error", message: decodeText(payload) };
    default:
      return { kind: "invalid", opcode };
  }
}

/** parseJson decodes a UTF-8 JSON control payload, or null when unusable. */
function parseJson<T>(bytes: Uint8Array): T | null {
  try {
    return JSON.parse(decodeText(bytes)) as T;
  } catch {
    return null;
  }
}

/** decodeText decodes UTF-8 control text without throwing on bad bytes. */
function decodeText(bytes: Uint8Array): string {
  return new TextDecoder().decode(bytes);
}
