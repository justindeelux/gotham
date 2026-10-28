import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  incidentEnvelopeSchema,
  incidentPageSchema,
} from "@/features/incidents/schemas/incidents";
import type {
  IncidentCoverage,
  IncidentLifecycle,
  IncidentSeverity,
} from "@/features/incidents/schemas/incidents";

export type { IncidentCoverage, IncidentLifecycle, IncidentSeverity };

export interface Incident {
  id: string;
  detector: string;
  scope: string;
  scope_id?: string;
  node_id?: string;
  project_id?: string;
  resource_type?: string;
  resource_id?: string;
  resource_name?: string;
  severity: IncidentSeverity;
  lifecycle: IncidentLifecycle;
  coverage: IncidentCoverage;
  first_observed_at: string;
  last_observed_at: string;
  last_valid_at?: string;
  confirmed_at?: string;
  resolved_at?: string;
  close_reason?: string;
  acknowledged_at?: string;
  acknowledged_by?: string;
  snoozed_until?: string;
  delivery_state: string;
  delivery_hold?: string;
  delivery_event?: string;
  delivery_attempts: number;
  delivery_error?: string;
  evidence?: string;
  grouped_under?: string;
  created_at: string;
  updated_at: string;
}

export interface IncidentPage {
  incidents: Incident[];
  next_cursor?: string;
  active_count: number;
}

export type IncidentStatusFilter = "active" | "resolved";

export interface ListIncidentOptions {
  status?: IncidentStatusFilter;
  severity?: IncidentSeverity;
  resourceType?: string;
  limit?: number;
  cursor?: string;
}

export const incidentPageLimit = 20;
export const resolvedIncidentLimit = 5;

export function incidentErrorStatus(error: unknown): number | null {
  if (typeof error !== "object" || error === null || !("status" in error)) {
    return null;
  }
  const status = (error as { status?: unknown }).status;
  return typeof status === "number" ? status : null;
}

export async function listIncidents(
  options: ListIncidentOptions = {},
): Promise<IncidentPage> {
  const params: Record<string, string | number> = {
    limit: options.limit ?? incidentPageLimit,
  };
  if (options.status) {
    params.status = options.status;
  }
  if (options.severity) {
    params.severity = options.severity;
  }
  if (options.resourceType) {
    params.resource_type = options.resourceType;
  }
  if (options.cursor) {
    params.cursor = options.cursor;
  }
  const { data } = await http.get("/incidents", { params });
  return parseWith(incidentPageSchema, data, { context: "IncidentPage" });
}

export async function acknowledgeIncident(id: string): Promise<Incident> {
  const { data } = await http.post(`/incidents/${id}/acknowledge`);
  return parseWith(incidentEnvelopeSchema, data, {
    context: "IncidentEnvelope",
  }).incident;
}

export async function snoozeIncident(
  id: string,
  until: string,
): Promise<Incident> {
  const { data } = await http.post(`/incidents/${id}/snooze`, { until });
  return parseWith(incidentEnvelopeSchema, data, {
    context: "IncidentEnvelope",
  }).incident;
}

export function snoozeUntil(hours: number, from = Date.now()): string {
  return new Date(from + hours * 3_600_000).toISOString();
}
