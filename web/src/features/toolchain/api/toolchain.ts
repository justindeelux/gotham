import { z } from "zod";

import { http } from "@/shared/api/http";
import type { ApiError } from "@/shared/api/http";
import { activeLocale } from "@/shared/i18n/locale";
import { parseWith } from "@/shared/validation/parse";
import { isApiError, stripErrorPrefix } from "@/features/servers";

import {
  toolchainCancelEnvelopeSchema,
  toolchainEnqueueEnvelopeSchema,
  toolchainJobDetailEnvelopeSchema,
  toolchainJobsEnvelopeSchema,
  toolchainToolsEnvelopeSchema,
} from "@/features/toolchain/schemas/toolchain";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

/** catalogFor selects the toolchain display dictionary for one locale. */
function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

/**
 * Typed client for the toolchain routes served by
 * `internal/server/toolchain_routes.go`:
 *
 *   GET  /servers/{id}/tools
 *   GET  /servers/{id}/toolchain/jobs
 *   POST /servers/{id}/toolchain/jobs
 *   GET  /servers/{id}/toolchain/jobs/{jobID}?after=N
 *   POST /servers/{id}/toolchain/jobs/{jobID}/cancel
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`).
 */

/** What an enqueue request does to each selected tool. */
export type ToolchainAction = "install" | "uninstall";

/** Lifecycle state of one toolchain job. */
export type ToolchainJobStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled";

/** One catalog entry with its detected state on a node. */
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

/** One queued install/uninstall job (never carries the log). */
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

/** One job plus a page of its log. */
export interface ToolchainJobDetail {
  job: ToolchainJob;
  log: string;
  next_seq: number;
}

/** Outcome of one enqueue request, in request order. */
export interface ToolchainEnqueueResult {
  jobs: ToolchainJob[];
  skipped: string[];
}

/** One output line of a running job, from a `toolchain_log` frame. */
export interface ToolchainLogLine {
  job_id: string;
  seq: number;
  stream?: string;
  line: string;
}

/** toolchainChannel returns the realtime room of one node's toolchain jobs. */
export function toolchainChannel(serverId: string): string {
  return `logs:${serverId}:toolchain`;
}

/** isTerminalJobStatus reports whether a job reached its final state. */
export function isTerminalJobStatus(status: ToolchainJobStatus): boolean {
  return status === "succeeded" || status === "failed" || status === "canceled";
}

function pathOf(serverId: string): string {
  return `/servers/${encodeURIComponent(serverId)}`;
}

/** listTools returns a node's tool catalog with each entry's detected state. */
export async function listTools(serverId: string): Promise<ToolchainTool[]> {
  const { data } = await http.get(`${pathOf(serverId)}/tools`);
  const envelope = parseWith(toolchainToolsEnvelopeSchema, data, {
    context: "ToolchainToolsEnvelope",
  });
  return envelope.tools;
}

/** listJobs returns a node's jobs, newest first (never the log). */
export async function listJobs(serverId: string, limit = 100): Promise<ToolchainJob[]> {
  const { data } = await http.get(`${pathOf(serverId)}/toolchain/jobs`, {
    params: { limit },
  });
  const envelope = parseWith(toolchainJobsEnvelopeSchema, data, {
    context: "ToolchainJobsEnvelope",
  });
  return envelope.jobs;
}

/** getJob returns one job plus a page of its log (`?after=` tails it). */
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

/** enqueueJobs enqueues one job per selected tool id. */
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

/** cancelJob cancels a queued job (running/terminal jobs answer 409). */
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

const jobStatuses: ReadonlySet<string> = new Set([
  "queued",
  "running",
  "succeeded",
  "failed",
  "canceled",
]);

function asJob(value: unknown): ToolchainJob | null {
  if (!isRecord(value)) {
    return null;
  }
  if (typeof value.id !== "string" || value.id === "") {
    return null;
  }
  if (typeof value.tool_id !== "string" || typeof value.server_id !== "string") {
    return null;
  }
  if (value.action !== "install" && value.action !== "uninstall") {
    return null;
  }
  if (typeof value.status !== "string" || !jobStatuses.has(value.status)) {
    return null;
  }
  if (typeof value.created_at !== "string") {
    return null;
  }
  return value as unknown as ToolchainJob;
}

/**
 * parseToolchainJobFrame decodes one `toolchain_job` realtime frame into a
 * job, or null when the frame is anything else (log lines, acks, notices).
 */
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

/**
 * parseToolchainLogFrame decodes one `toolchain_log` realtime frame into a
 * log line, or null when the frame is anything else.
 */
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

/** describeToolchainError renders a toolchain failure in the active locale. */
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
  return detail || errors.unexpected;
}

/** enqueueSchema validates the multi-select enqueue form (at least one tool). */
export const enqueueFormSchema = z.object({
  tools: z.array(z.string()).min(1),
  action: z.enum(["install", "uninstall"]),
});
