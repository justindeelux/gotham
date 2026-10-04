// Pure formatting helpers for the log viewer (extracted from LogViewer).

import type { WebSocketMessage } from "@/shared/composables/useWebSocket";

/** One rendered log line. */
export interface LogLine {
  id: number;
  ts: string;
  text: string;
  kind: "line" | "notice";
}

/** splitLines breaks a chunk into lines, dropping a single trailing newline. */
export function splitLines(text: string): string[] {
  const parts = text.split(/\r?\n/);
  if (parts.length > 1 && parts[parts.length - 1] === "") {
    parts.pop();
  }
  return parts;
}

/** formatTimestamp renders a hub timestamp as HH:MM:SS. */
export function formatTimestamp(ts: unknown, receivedAt: number): string {
  if (typeof ts === "string" && /^\d{2}:\d{2}:\d{2}$/.test(ts)) {
    return ts;
  }

  let value: Date;
  if (typeof ts === "number") {
    value = new Date(ts < 1e12 ? ts * 1000 : ts);
  } else if (typeof ts === "string" && ts !== "") {
    const parsed = new Date(ts);
    value = Number.isNaN(parsed.getTime()) ? new Date(receivedAt) : parsed;
  } else {
    value = new Date(receivedAt);
  }
  return value.toTimeString().slice(0, 8);
}

/** noticeText extracts the server's own notice copy when provided. */
export function noticeText(message: WebSocketMessage): string {
  const payload = message.payload;
  const candidate =
    payload?.message ?? payload?.notice ?? payload?.reason ?? payload?.data;
  if (typeof candidate === "string" && candidate.trim() !== "") {
    return candidate;
  }
  return "Log stream interrupted; reconnecting…";
}
