import { ZodError } from "zod";

import type { ApiError } from "@/shared/api/http";
import { http } from "@/shared/api/http";
import { getAccessToken } from "@/shared/api/token";
import {
  applicationTerminalTargetsSchema,
  terminalSessionSchema,
  terminalTargetsSchema,
  type ApplicationTerminalTargets,
  type CreateApplicationTerminalInput,
  type CreateTerminalInput,
  type TerminalSessionInfo,
  type TerminalTargets,
} from "@/features/terminal/schemas/terminal";

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
  | "originRefused"
  | "sessionExpired"
  | "applicationForbidden"
  | "applicationNotFound"
  | "noDeployment"
  | "deployInFlight"
  | "containerStopped"
  | "containerGone"
  | "containerNoShell"
  | "terminalDisabled";

export interface TerminalFailure {
  code: TerminalErrorCode;
  message: string;
}

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

export async function listApplicationTerminalTargets(
  applicationId: string,
  signal?: AbortSignal,
): Promise<ApplicationTerminalTargets> {
  const response = await http.get<unknown>(
    `/applications/${encodeURIComponent(applicationId)}/terminal/targets`,
    signal ? { signal } : undefined,
  );
  return applicationTerminalTargetsSchema.parse(response.data);
}

export async function listTerminalTargets(
  signal?: AbortSignal,
): Promise<TerminalTargets> {
  const response = await http.get<unknown>("/terminal/targets", {
    ...(signal ? { signal } : {}),
  });
  return terminalTargetsSchema.parse(response.data);
}

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
    {
      ...(signal ? { signal } : {}),
    },
  );
  return terminalSessionSchema.parse(response.data);
}

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

export function terminalWebSocketUrl(path: string): string {
  const url = new URL(path, window.location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}

export function terminalSubprotocols(token: string): string[] {
  return ["gotham.terminal.v1", `gotham.token.${token}`];
}

export function currentTerminalToken(): string | null {
  return getAccessToken();
}

export function terminalFailure(error: unknown): TerminalFailure {
  if (error instanceof ZodError) {
    return { code: "badResponse", message: "" };
  }
  const api = error as Partial<ApiError> | null;
  const status = typeof api?.status === "number" ? api.status : null;
  const message = typeof api?.message === "string" ? api.message : "";
  switch (status) {
    case 400:
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

function isInvalidWorkingDirectory(message: string): boolean {
  return message.toLowerCase().includes("invalid working directory");
}

export function applicationTerminalFailure(
  error: unknown,
): TerminalFailure {
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
