import { ZodError } from "zod";

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
  | "cwdInvalid"
  | "cwdMissing"
  | "cwdUnreadable"
  | "agentUpdate"
  | "badResponse"
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
  signal?: AbortSignal,
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
    signal ? { signal } : undefined,
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
  // A 2xx body that does not match the create contract is a schema failure,
  // not a transport failure: report it as an unexpected response so the UI
  // never blames the network or echoes raw parse text.
  if (error instanceof ZodError) {
    return { code: "badResponse", message: "" };
  }
  const api = error as Partial<ApiError> | null;
  const status = typeof api?.status === "number" ? api.status : null;
  const message = typeof api?.message === "string" ? api.message : "";
  switch (status) {
    case 400:
      // A cwd the control plane refuses before the shell starts (absolute,
      // traversal, over-long). The agent's own refusal arrives later as an
      // error frame and is classified by terminalStreamFailureCode.
      return isInvalidWorkingDirectory(message)
        ? { code: "cwdInvalid", message }
        : { code: "generic", message };
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

/** isInvalidWorkingDirectory recognises the contract's stable cwd phrase. */
function isInvalidWorkingDirectory(message: string): boolean {
  return message.toLowerCase().includes("invalid working directory");
}

/**
 * terminalStreamFailureCode maps the control plane's bounded error-frame text
 * onto a translatable class (JUS-248). The relay deliberately never sends a
 * host path, so these exact phrases are the contract surface; an unknown text
 * stays null and the caller shows the bounded message verbatim.
 */
export function terminalStreamFailureCode(
  message: string,
): TerminalErrorCode | null {
  const text = message.trim().toLowerCase();
  if (isInvalidWorkingDirectory(text)) {
    return "cwdInvalid";
  }
  switch (text) {
    case "the working directory no longer exists":
      return "cwdMissing";
    case "the working directory is not readable":
      return "cwdUnreadable";
    case "the node agent does not support terminal sessions":
      return "unsupported";
    case "the node agent needs an update to open a terminal without a folder":
      return "agentUpdate";
    case "the node is running the maximum number of terminal sessions":
      return "quota";
    case "the node is unavailable":
    case "the node agent is unavailable":
    case "the node registry is unavailable":
    case "the node did not start the shell in time":
    case "the session expired before it started":
      return "unavailable";
    default:
      return null;
  }
}
