/**
 * Display helpers for the terminal feature (JUS-245). Kept pure so the
 * surface, the window and the tests agree on the visible lifecycle wording.
 */
import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import type { TerminalStatus } from "@/features/terminal/transport/terminalRuntime";

/** TerminalTone is the semantic color the status line uses. */
export type TerminalTone = "muted" | "accent" | "success" | "warn" | "danger";

/** statusTone maps a lifecycle state onto a design-token tone. */
export function statusTone(status: TerminalStatus): TerminalTone {
  switch (status) {
    case "connected":
      return "success";
    case "connecting":
      return "accent";
    case "exited":
      return "warn";
    case "disconnected":
      return "warn";
    case "error":
      return "danger";
  }
}

/** isRetryable reports whether an explicit new session is offered. */
export function isRetryable(status: TerminalStatus): boolean {
  return (
    status === "disconnected" || status === "error" || status === "exited"
  );
}

/** isLive reports whether the PTY is expected to be running. */
export function isLive(status: TerminalStatus): boolean {
  return status === "connecting" || status === "connected";
}

/**
 * isContainerTerminalTarget reports whether a tab targets an application
 * container. The value is the exact contract target, never a heuristic on a
 * server/application id, so host and container paths can never be confused.
 */
export function isContainerTerminalTarget(target: string): boolean {
  return target === "container";
}

/** errorKey maps a stable failure class onto its catalog key. */
export function errorKey(code: TerminalErrorCode | null): string {
  return `terminal.errors.${code ?? "generic"}`;
}

/** shortSessionId trims a session id for a diagnostic label. */
export function shortSessionId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}
