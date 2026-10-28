import { z } from "zod";

export const incidentSeveritySchema = z.enum(["warning", "critical"]);

export const incidentLifecycleSchema = z.enum(["pending", "firing", "resolved"]);

export const incidentCoverageSchema = z.enum([
  "fresh",
  "partial",
  "stale",
  "unknown",
  "expected",
]);

export type IncidentSeverity = z.infer<typeof incidentSeveritySchema>;
export type IncidentLifecycle = z.infer<typeof incidentLifecycleSchema>;
export type IncidentCoverage = z.infer<typeof incidentCoverageSchema>;

export const incidentSchema = z.object({
  id: z.string(),
  detector: z.string(),
  scope: z.string(),
  scope_id: z.string().optional(),
  node_id: z.string().optional(),
  project_id: z.string().optional(),
  resource_type: z.string().optional(),
  resource_id: z.string().optional(),
  resource_name: z.string().optional(),
  severity: incidentSeveritySchema,
  lifecycle: incidentLifecycleSchema,
  coverage: incidentCoverageSchema,
  first_observed_at: z.string(),
  last_observed_at: z.string(),
  last_valid_at: z.string().optional(),
  confirmed_at: z.string().optional(),
  resolved_at: z.string().optional(),
  close_reason: z.string().optional(),
  acknowledged_at: z.string().optional(),
  acknowledged_by: z.string().optional(),
  snoozed_until: z.string().optional(),
  delivery_state: z.string(),
  delivery_hold: z.string().optional(),
  delivery_event: z.string().optional(),
  delivery_attempts: z.number(),
  delivery_error: z.string().optional(),
  evidence: z.string().optional(),
  grouped_under: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const incidentPageSchema = z.object({
  incidents: z.array(incidentSchema),
  next_cursor: z.string().optional(),
  active_count: z.number(),
});

export const incidentEnvelopeSchema = z.object({
  incident: incidentSchema,
});
