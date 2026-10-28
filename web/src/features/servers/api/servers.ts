import type { AxiosResponse } from "axios";

import type { ApiError } from "@/shared/api/http";
import { http } from "@/shared/api/http";
import { activeLocale } from "@/shared/i18n/locale";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export type ServerStatus =
  | "pending"
  | "validating"
  | "ready"
  | "offline"
  | "error";

export type ServerCheckName = "docker" | "cpu" | "ram" | "disk";

export interface Server {
  id: string;
  name: string;
  ip: string;
  port: number;
  ssh_user: string;
  ssh_key_id: string | null;
  has_password: boolean;
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

export interface CheckResult {
  name: ServerCheckName;
  ok: boolean;
  detail: string;
  installable?: boolean;
}

export interface PrivateKeyMeta {
  id: string;
  name: string;
  created_at: string;
}

export interface CreateServerInput {
  name: string;
  ip: string;
  port?: number;
  ssh_user: string;
  ssh_key_id?: string | null;
  password?: string;
}

export interface UpdateServerInput {
  name?: string;
  ip?: string;
  port?: number;
  ssh_user?: string;
  ssh_key_id?: string;
  password?: string;
}

export interface ValidateOptions {
  password?: string;
  trustHostKey?: boolean;
}

export interface CreatePrivateKeyInput {
  name: string;
  private_key: string;
}

export interface ValidateOutcome {
  ok: boolean;
  checks: CheckResult[];
  server: Server | null;
  message: string;
}

interface ServerEnvelope {
  server: Server;
}

interface ServerListEnvelope {
  servers: Server[];
}

interface ValidateResponse {
  message?: string;
  checks?: CheckResult[];
  server?: Server;
}

const validateTimeoutMs = 45_000;

export async function listServers(): Promise<Server[]> {
  const response = await http.get<ServerListEnvelope>("/servers");
  return response.data.servers ?? [];
}

export async function createServer(input: CreateServerInput): Promise<Server> {
  const response = await http.post<ServerEnvelope>("/servers", input);
  return response.data.server;
}

export async function getServer(id: string): Promise<Server> {
  const response = await http.get<ServerEnvelope>(`/servers/${id}`);
  return response.data.server;
}

export async function deleteServer(id: string): Promise<void> {
  await http.delete(`/servers/${id}`);
}

export async function validateServer(
  id: string,
  passphrase?: string,
  options: ValidateOptions = {},
): Promise<ValidateOutcome> {
  const body: Record<string, unknown> = {};
  if (passphrase) {
    body.passphrase = passphrase;
  }
  if (options.password) {
    body.password = options.password;
  }
  if (options.trustHostKey) {
    body.trust_host_key = true;
  }
  const response: AxiosResponse<ValidateResponse> = await http.post<ValidateResponse>(
    `/servers/${id}/validate`,
    body,
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

export function dockerInstallPath(id: string): string {
  return `/api/v1/servers/${id}/docker-install`;
}

export async function updateServer(id: string, input: UpdateServerInput): Promise<Server> {
  const response = await http.patch<ServerEnvelope>(`/servers/${id}`, input);
  return response.data.server;
}

export async function createPrivateKey(
  input: CreatePrivateKeyInput,
): Promise<PrivateKeyMeta> {
  const response = await http.post<PrivateKeyMeta>("/private-keys", input);
  return response.data;
}

export function isApiError(error: unknown): error is ApiError {
  return (
    typeof error === "object" &&
    error !== null &&
    "message" in error &&
    "status" in error
  );
}

export function describeServerError(error: unknown, locale?: string | null): string {
  const errors = catalogFor(locale).errors;
  if (isApiError(error)) {
    return stripErrorPrefix(error.message) || errors.requestFailed;
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || errors.unexpected;
  }
  return errors.unexpected;
}

export function failureText(error: unknown, locale?: string | null): string {
  const errors = catalogFor(locale).errors;
  const raw =
    isApiError(error) || error instanceof Error ? error.message : "";
  const detail = stripErrorPrefix(raw ?? "");
  if (detail === "") {
    return isApiError(error) ? errors.requestFailed : errors.unexpected;
  }
  const summary = isApiError(error) ? errors.requestFailed : errors.unexpected;
  return `${summary} — ${detail}`;
}

const errorPrefixPackages = new Set([
  "auth",
  "backupwatch",
  "builds",
  "containers",
  "databases",
  "deploy",
  "docker",
  "domainwatch",
  "files",
  "githubapp",
  "incidents",
  "instance",
  "monitoring",
  "notifications",
  "oauth",
  "providers",
  "projects",
  "proxy",
  "replay",
  "runtimewatch",
  "server",
  "servers",
  "services",
  "spa",
  "ssh",
  "store",
  "templates",
  "terminal",
  "toolchain",
  "updates",
  "webhooks",
  "ws",
]);

export function stripErrorPrefix(message: string): string {
  const match = /^\s*([A-Za-z][A-Za-z0-9_-]*)\s*:\s*([\s\S]*)$/.exec(message ?? "");
  if (match && errorPrefixPackages.has(match[1].toLowerCase())) {
    return match[2].trim();
  }
  return (message ?? "").trim();
}

export function conflictDetail(message: string): string {
  const text = (message ?? "").trim();
  const separator = text.indexOf(": ");
  if (separator === -1) {
    return text;
  }
  return text.slice(separator + 2).trim() || text;
}
