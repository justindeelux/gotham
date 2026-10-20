import type { ApiError } from "@/shared/api/http";
import { http } from "@/shared/api/http";
import { getAccessToken } from "@/shared/api/token";
import {
  terminalSessionSchema,
  type CreateTerminalInput,
  type TerminalSessionInfo,
} from "@/features/terminal/schemas/terminal";

/**
 * Host terminal session API (JUS-243 contract). Creating a session is an HTTP
 * call; attaching is a WebSocket opened by the runtime. The access token only
 * ever travels in the `gotham.token.<jwt>` subprotocol, never in the URL.
 */

/** TerminalErrorCode is a stable, translatable failure class. */
export type TerminalErrorCode =
  | "auth"
  | "forbidden"
  | "node"
  | "quota"
  | "unsupported"
  | "unavailable"
  | "network"
  | "generic";

/** TerminalFailure is a bounded failure a tab can render. */
export interface TerminalFailure {
  code: TerminalErrorCode;
  /** message is the control plane's bounded text, or "". */
  message: string;
}

/** createTerminalSession asks the control plane for a pending PTY session. */
export async function createTerminalSession(
  serverId: string,
  input: CreateTerminalInput,
): Promise<TerminalSessionInfo> {
  const response = await http.post<unknown>(
    `/servers/${encodeURIComponent(serverId)}/terminal/sessions`,
    {
      target: input.target ?? "host",
      ...(input.root !== undefined ? { root: input.root } : {}),
      ...(input.cwd !== undefined ? { cwd: input.cwd } : {}),
      cols: input.cols,
      rows: input.rows,
    },
  );
  return terminalSessionSchema.parse(response.data);
}

/**
 * closeTerminalSession cancels a pending or attached session. Cleanup is
 * best-effort: a session that already expired or ended answers 404, which is
 * success from the caller's point of view.
 */
export async function closeTerminalSession(
  serverId: string,
  sessionId: string,
): Promise<void> {
  try {
    await http.delete(
      `/servers/${encodeURIComponent(serverId)}/terminal/sessions/${encodeURIComponent(sessionId)}`,
    );
  } catch (error) {
    const status = (error as ApiError | null)?.status ?? null;
    if (status === 404 || status === 409) {
      return;
    }
    throw error;
  }
}

/** terminalWebSocketUrl resolves the server's path against the SPA origin. */
export function terminalWebSocketUrl(path: string): string {
  const url = new URL(path, window.location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}

/** terminalSubprotocols is the exact browser handshake list. */
export function terminalSubprotocols(token: string): string[] {
  return ["gotham.terminal.v1", `gotham.token.${token}`];
}

/** currentTerminalToken returns the access token used for a handshake. */
export function currentTerminalToken(): string | null {
  return getAccessToken();
}

/**
 * terminalFailure classifies an HTTP failure. The returned message is only
 * the control plane's bounded text; the UI prefers its own catalog copy for
 * every known code.
 */
export function terminalFailure(error: unknown): TerminalFailure {
  const api = error as Partial<ApiError> | null;
  const status = typeof api?.status === "number" ? api.status : null;
  const message = typeof api?.message === "string" ? api.message : "";
  switch (status) {
    case 401:
      return { code: "auth", message };
    case 403:
      return { code: "forbidden", message };
    case 404:
      return { code: "node", message };
    case 429:
      return { code: "quota", message };
    case 501:
      return { code: "unsupported", message };
    case 503:
      return { code: "unavailable", message };
    default:
      return { code: status === null ? "network" : "generic", message };
  }
}
