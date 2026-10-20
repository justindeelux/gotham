import { ZodError } from "zod";

import type { ApiError } from "@/shared/api/http";
import { http } from "@/shared/api/http";
import { getAccessToken } from "@/shared/api/token";
import {
  applicationTerminalTargetsSchema,
  terminalSessionSchema,
  type ApplicationTerminalTargets,
  type CreateApplicationTerminalInput,
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
  | "generic"
  // Application container targets (JUS-247). Kept distinct from the host
  // classes so a container failure never borrows host-only copy ("platform
  // operator", "server root") and vice versa.
  | "applicationForbidden"
  | "applicationNotFound"
  | "noDeployment"
  | "deployInFlight"
  | "containerStopped"
  | "containerGone"
  | "containerNoShell"
  | "terminalDisabled";

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

/**
 * listApplicationTerminalTargets discovers the application's running service
 * containers (GET /applications/{id}/terminal/targets). No session is created:
 * this is the lazy discovery/recovery step behind the Terminal tab. The route
 * needs an interactive JWT and team write access; the bounded failures are
 * classified by `applicationTerminalFailure`.
 */
export async function listApplicationTerminalTargets(
  applicationId: string,
  signal?: AbortSignal,
): Promise<ApplicationTerminalTargets> {
  const response = await http.get<unknown>(
    `/applications/${encodeURIComponent(applicationId)}/terminal/targets`,
    signal ? { signal } : undefined,
  );
  // Strict parse at the boundary, like the create response (JUS-253): a
  // drifted 2xx body is a bounded `badResponse` failure, never a cast and
  // never misreported as a network error.
  return applicationTerminalTargetsSchema.parse(response.data);
}

/**
 * createApplicationTerminalSession asks for a container exec with the exact
 * `(service, container_id)` pair the discovery response returned (JUS-244).
 * The browser never invents an id: the control plane re-resolves the running
 * set and rejects a stale target with 404.
 */
export async function createApplicationTerminalSession(
  applicationId: string,
  input: CreateApplicationTerminalInput,
  signal?: AbortSignal,
): Promise<TerminalSessionInfo> {
  const response = await http.post<unknown>(
    `/applications/${encodeURIComponent(applicationId)}/terminal/sessions`,
    {
      service: input.service,
      container_id: input.containerId,
      cols: input.cols,
      rows: input.rows,
    },
    signal ? { signal } : undefined,
  );
  return terminalSessionSchema.parse(response.data);
}

/** closeApplicationTerminalSession releases a pending or attached container exec. */
export async function closeApplicationTerminalSession(
  applicationId: string,
  sessionId: string,
): Promise<void> {
  try {
    await http.delete(
      `/applications/${encodeURIComponent(applicationId)}/terminal/sessions/${encodeURIComponent(sessionId)}`,
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
 * applicationTerminalFailure classifies a container target/session failure.
 *
 * The contract returns fixed, bounded phrases per status (409 has three
 * meanings: no running deployment, an in-flight deploy, a stopped container).
 * The classifier reads those phrases only to pick a catalog code; the phrase
 * itself is never rendered, so a drifting or hostile body cannot reach the DOM.
 */
export function applicationTerminalFailure(
  error: unknown,
): TerminalFailure {
  // A 2xx body outside the contract is an unexpected response, not a
  // transport failure (same rule as the host classifier, JUS-253).
  if (error instanceof ZodError) {
    return { code: "badResponse", message: "" };
  }
  const api = error as Partial<ApiError> | null;
  const status = typeof api?.status === "number" ? api.status : null;
  const message = typeof api?.message === "string" ? api.message : "";
  const text = message.toLowerCase();
  switch (status) {
    case 400:
      return { code: "generic", message };
    case 401:
      return { code: "auth", message };
    case 403:
      return { code: "applicationForbidden", message };
    case 404:
      return text.includes("container")
        ? { code: "containerGone", message }
        : { code: "applicationNotFound", message };
    case 409:
      if (text.includes("no running deployment")) {
        return { code: "noDeployment", message };
      }
      if (text.includes("deployment is in progress")) {
        return { code: "deployInFlight", message };
      }
      if (text.includes("not running")) {
        return { code: "containerStopped", message };
      }
      return { code: "generic", message };
    case 429:
      return { code: "quota", message };
    case 501:
      return { code: "unsupported", message };
    case 502:
      return { code: "unavailable", message };
    case 503:
      return { code: "terminalDisabled", message };
    default:
      return { code: status === null ? "network" : "generic", message };
  }
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
    // Container exec stream failures (JUS-244 relayError phrases).
    case "the container no longer exists; redeploy the application":
      return "containerGone";
    case "the container is not running; start the application first":
      return "containerStopped";
    case "the container has no supported shell":
      return "containerNoShell";
    case "this node agent does not support container terminals":
      return "unsupported";
    case "the node agent or docker daemon is unavailable":
      return "unavailable";
    default:
      return null;
  }
}
