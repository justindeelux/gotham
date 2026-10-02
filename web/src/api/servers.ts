import type { AxiosResponse } from "axios";

import type { ApiError } from "./http";
import { http } from "./http";

/** Lifecycle state of a managed server. */
export type ServerStatus =
  | "pending"
  | "validating"
  | "ready"
  | "offline"
  | "error";

/** Names of the probes the backend reports during validation. */
export type ServerCheckName = "docker" | "cpu" | "ram" | "disk";

/** A managed server as returned by the control-plane API. */
export interface Server {
  id: string;
  name: string;
  ip: string;
  port: number;
  ssh_user: string;
  ssh_key_id: string | null;
  status: ServerStatus;
  node_id: string | null;
  os: string | null;
  docker_version: string | null;
  arch: string | null;
  total_mem: number | null;
  total_disk: number | null;
  cpu_usage: number | null;
  mem_usage: number | null;
  disk_usage: number | null;
  container_count: number | null;
  last_seen: string | null;
  created_at: string;
  updated_at: string;
}

/** One probe outcome from a validation run. */
export interface CheckResult {
  name: ServerCheckName;
  ok: boolean;
  detail: string;
}

/** Private-key metadata; key material is never returned. */
export interface PrivateKeyMeta {
  id: string;
  name: string;
  created_at: string;
}

/** Body accepted by POST /servers. */
export interface CreateServerInput {
  name: string;
  ip: string;
  port?: number;
  ssh_user: string;
  ssh_key_id?: string | null;
}

/** Body accepted by POST /private-keys. */
export interface CreatePrivateKeyInput {
  name: string;
  private_key: string;
}

/**
 * Result of POST /servers/{id}/validate. The endpoint answers 200 when every
 * probe passes and 422 with the same shape when one or more fail, so callers
 * always receive the checks instead of an opaque error.
 */
export interface ValidateOutcome {
  ok: boolean;
  checks: CheckResult[];
  server: Server | null;
  message: string;
}

/** Wire envelope for a single server. */
interface ServerEnvelope {
  server: Server;
}

/** Wire envelope for a server list. */
interface ServerListEnvelope {
  servers: Server[];
}

/** Wire body shared by the 200 and 422 validation responses. */
interface ValidateResponse {
  message?: string;
  checks?: CheckResult[];
  server?: Server;
}

/**
 * A validation run is bounded server-side at 15s, matching the default client
 * timeout, so the request could race and abort before the checks arrive. Give
 * it extra headroom to always read the 200/422 body.
 */
const validateTimeoutMs = 45_000;

/** listServers returns every managed server. */
export async function listServers(): Promise<Server[]> {
  const response = await http.get<ServerListEnvelope>("/servers");
  return response.data.servers ?? [];
}

/** createServer registers a new server. */
export async function createServer(input: CreateServerInput): Promise<Server> {
  const response = await http.post<ServerEnvelope>("/servers", input);
  return response.data.server;
}

/** getServer returns one server by id. */
export async function getServer(id: string): Promise<Server> {
  const response = await http.get<ServerEnvelope>(`/servers/${id}`);
  return response.data.server;
}

/** deleteServer removes one server. */
export async function deleteServer(id: string): Promise<void> {
  await http.delete(`/servers/${id}`);
}

/**
 * validateServer runs the SSH probes. A 422 is a normal outcome rather than an
 * error, so both status codes resolve to a {@link ValidateOutcome}. passphrase
 * decrypts a passphrase-protected key for this run only.
 */
export async function validateServer(
  id: string,
  passphrase?: string,
): Promise<ValidateOutcome> {
  const response: AxiosResponse<ValidateResponse> = await http.post<ValidateResponse>(
    `/servers/${id}/validate`,
    passphrase ? { passphrase } : {},
    {
      timeout: validateTimeoutMs,
      validateStatus: (status) => status === 200 || status === 422,
    },
  );

  return {
    ok: response.status === 200,
    checks: response.data.checks ?? [],
    server: response.data.server ?? null,
    message: response.data.message ?? "",
  };
}

/** createPrivateKey stores an encrypted SSH private key, returning its metadata. */
export async function createPrivateKey(
  input: CreatePrivateKeyInput,
): Promise<PrivateKeyMeta> {
  const response = await http.post<PrivateKeyMeta>("/private-keys", input);
  return response.data;
}

/** isApiError narrows an unknown error thrown by the shared HTTP layer. */
export function isApiError(error: unknown): error is ApiError {
  return (
    typeof error === "object" &&
    error !== null &&
    "message" in error &&
    "status" in error
  );
}

/** describeServerError maps a thrown error to a user-facing message. */
export function describeServerError(error: unknown): string {
  if (isApiError(error)) {
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}
