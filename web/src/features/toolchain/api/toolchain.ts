import { http } from "@/shared/api/http";
import type { ApiError } from "@/shared/api/http";
import { activeLocale } from "@/shared/i18n/locale";
import { parseWith } from "@/shared/validation/parse";
import { isApiError, stripErrorPrefix } from "@/features/servers";

import {
  toolchainCancelEnvelopeSchema,
  toolchainEnqueueEnvelopeSchema,
  toolchainJobDetailEnvelopeSchema,
  toolchainJobSchema,
  toolchainJobsEnvelopeSchema,
  toolchainToolsEnvelopeSchema,
} from "@/features/toolchain/schemas/toolchain";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export type ToolchainAction = "install" | "uninstall";

export type ToolchainJobStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled";

export interface ToolchainTool {
  id: string;
  name: string;
  description?: string;
  category?: string;
  installed: boolean;
  version?: string;
  path?: string;
  installable: boolean;
  uninstallable: boolean;
  needs_root: boolean;
  deps?: string[];
  min_version?: string;
  version_ok: boolean;
}

export interface ToolchainJob {
  id: string;
  server_id: string;
  tool_id: string;
  action: ToolchainAction;
  status: ToolchainJobStatus;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
}

export interface ToolchainJobDetail {
  job: ToolchainJob;
  log: string;
  next_seq: number;
}

export interface ToolchainEnqueueResult {
  jobs: ToolchainJob[];
  skipped: string[];
}

export interface ToolchainLogLine {
  job_id: string;
  seq: number;
  stream?: string;
  line: string;
}

export function toolchainChannel(serverId: string): string {
  return `logs:${serverId}:toolchain`;
}

export function isTerminalJobStatus(status: ToolchainJobStatus): boolean {
  return status === "succeeded" || status === "failed" || status === "canceled";
}

export function isToolSelectable(tool: ToolchainTool): boolean {
  return !tool.installed && tool.installable;
}

function pathOf(serverId: string): string {
  return `/servers/${encodeURIComponent(serverId)}`;
}

export async function listTools(serverId: string): Promise<ToolchainTool[]> {
  const { data } = await http.get(`${pathOf(serverId)}/tools`);
  const envelope = parseWith(toolchainToolsEnvelopeSchema, data, {
    context: "ToolchainToolsEnvelope",
  });
  return envelope.tools;
}

export async function listJobs(serverId: string, limit = 100): Promise<ToolchainJob[]> {
  const { data } = await http.get(`${pathOf(serverId)}/toolchain/jobs`, {
    params: { limit },
  });
  const envelope = parseWith(toolchainJobsEnvelopeSchema, data, {
    context: "ToolchainJobsEnvelope",
  });
  return envelope.jobs;
}

export async function getJob(
  serverId: string,
  jobId: string,
  after?: number,
): Promise<ToolchainJobDetail> {
  const { data } = await http.get(
    `${pathOf(serverId)}/toolchain/jobs/${encodeURIComponent(jobId)}`,
    after === undefined ? undefined : { params: { after } },
  );
  return parseWith(toolchainJobDetailEnvelopeSchema, data, {
    context: "ToolchainJobDetailEnvelope",
  });
}

export async function enqueueJobs(
  serverId: string,
  toolIds: string[],
  action: ToolchainAction,
): Promise<ToolchainEnqueueResult> {
  const { data } = await http.post(`${pathOf(serverId)}/toolchain/jobs`, {
    tools: toolIds,
    action,
  });
  return parseWith(toolchainEnqueueEnvelopeSchema, data, {
    context: "ToolchainEnqueueEnvelope",
  });
}

export async function cancelJob(serverId: string, jobId: string): Promise<ToolchainJob> {
  const { data } = await http.post(
    `${pathOf(serverId)}/toolchain/jobs/${encodeURIComponent(jobId)}/cancel`,
  );
  const envelope = parseWith(toolchainCancelEnvelopeSchema, data, {
    context: "ToolchainCancelEnvelope",
  });
  return envelope.job;
}

interface ToolchainFrame {
  channel?: unknown;
  type?: unknown;
  data?: unknown;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function asJob(value: unknown): ToolchainJob | null {
  const parsed = toolchainJobSchema.safeParse(value);
  return parsed.success ? (parsed.data as ToolchainJob) : null;
}

export function parseToolchainJobFrame(raw: string): ToolchainJob | null {
  let frame: ToolchainFrame;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed)) {
      return null;
    }
    frame = parsed;
  } catch {
    return null;
  }
  if (frame.type !== "toolchain_job" || typeof frame.data !== "string") {
    return null;
  }
  try {
    return asJob(JSON.parse(frame.data));
  } catch {
    return null;
  }
}

export function parseToolchainLogFrame(raw: string): ToolchainLogLine | null {
  let frame: ToolchainFrame;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed)) {
      return null;
    }
    frame = parsed;
  } catch {
    return null;
  }
  if (frame.type !== "toolchain_log" || typeof frame.data !== "string") {
    return null;
  }
  let payload: unknown;
  try {
    payload = JSON.parse(frame.data);
  } catch {
    return null;
  }
  if (!isRecord(payload)) {
    return null;
  }
  if (typeof payload.job_id !== "string" || payload.job_id === "") {
    return null;
  }
  if (typeof payload.seq !== "number" || !Number.isFinite(payload.seq)) {
    return null;
  }
  if (typeof payload.line !== "string") {
    return null;
  }
  return {
    job_id: payload.job_id,
    seq: Math.floor(payload.seq),
    stream: typeof payload.stream === "string" ? payload.stream : undefined,
    line: payload.line,
  };
}

export function describeToolchainError(error: unknown, locale?: string | null): string {
  const errors = catalogFor(locale).errors;
  if (!isApiError(error)) {
    return errors.unexpected;
  }
  const apiError = error as ApiError;
  if (apiError.status === 0 || apiError.status === null) {
    return errors.sessionExpired;
  }
  const detail = stripErrorPrefix(apiError.message);
  if (apiError.status === 404) {
    return detail || errors.notFound;
  }
  if (apiError.status === 409) {
    return detail || errors.conflict;
  }
  if (apiError.status === 502) {
    return detail || errors.agentUnavailable;
  }
  return detail || errors.unexpected;
}
