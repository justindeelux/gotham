import type { TerminalErrorCode } from "@/features/terminal/api/terminal";
import type { TerminalStatus } from "@/features/terminal/transport/terminalRuntime";

export type TerminalTone = "muted" | "accent" | "success" | "warn" | "danger";

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

export function isRetryable(status: TerminalStatus): boolean {
  return (
    status === "disconnected" || status === "error" || status === "exited"
  );
}

export function isLive(status: TerminalStatus): boolean {
  return status === "connecting" || status === "connected";
}

export function isContainerTerminalTarget(target: string): boolean {
  return target === "container";
}

export function errorKey(code: TerminalErrorCode | null): string {
  return `terminal.errors.${code ?? "generic"}`;
}

export function shortSessionId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}
