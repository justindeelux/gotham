
export const CLIENT_INPUT = 0x01 as const;
export const CLIENT_RESIZE = 0x02 as const;
export const CLIENT_CLOSE = 0x03 as const;
export const CLIENT_HEARTBEAT = 0x04 as const;

export const SERVER_OUTPUT = 0x01 as const;
export const SERVER_EXITED = 0x02 as const;
export const SERVER_ERROR = 0x03 as const;
export const SERVER_OPENED = 0x04 as const;
export const SERVER_PING = 0x05 as const;

export interface OpenedInfo {
  shell: string;
  cols: number;
  rows: number;
  service_user: string;
}

export interface ExitedInfo {
  exit_code: number;
  signal: string;
}

export type ServerFrame =
  | { kind: "output"; bytes: Uint8Array }
  | { kind: "opened"; info: OpenedInfo }
  | { kind: "exited"; info: ExitedInfo }
  | { kind: "error"; message: string }
  | { kind: "ping" }
  | { kind: "invalid"; opcode: number };

export function encodeInput(data: string | Uint8Array): Uint8Array {
  const bytes = typeof data === "string" ? new TextEncoder().encode(data) : data;
  const frame = new Uint8Array(bytes.length + 1);
  frame[0] = CLIENT_INPUT;
  frame.set(bytes, 1);
  return frame;
}

export function encodeResize(cols: number, rows: number): Uint8Array {
  const frame = new Uint8Array(5);
  frame[0] = CLIENT_RESIZE;
  const view = new DataView(frame.buffer);
  view.setUint16(1, clampDimension(cols), false);
  view.setUint16(3, clampDimension(rows), false);
  return frame;
}

export function encodeClose(): Uint8Array {
  return new Uint8Array([CLIENT_CLOSE]);
}

export function encodeHeartbeat(): Uint8Array {
  return new Uint8Array([CLIENT_HEARTBEAT]);
}

export function clampDimension(value: number): number {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.min(1000, Math.max(1, Math.round(value)));
}

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

function parseJson<T>(bytes: Uint8Array): T | null {
  try {
    return JSON.parse(decodeText(bytes)) as T;
  } catch {
    return null;
  }
}

function decodeText(bytes: Uint8Array): string {
  return new TextDecoder().decode(bytes);
}
