import { z } from "zod";

import { fileKindSchema } from "@/features/files/schemas/files";

export const transferModeSchema = z.enum(["direct", "bundle"]);
export type TransferMode = z.infer<typeof transferModeSchema>;

export const transferPhaseSchema = z.enum([
  "preparing",
  "transferring",
  "extracting",
  "publishing",
  "cleaning_up",
]);
export type TransferPhase = z.infer<typeof transferPhaseSchema>;

export const transferEntryStatusSchema = z.enum([
  "copied",
  "conflict",
  "skipped",
  "failed",
  "cancelled",
  "uncertain",
]);
export type TransferEntryStatus = z.infer<typeof transferEntryStatusSchema>;

export const transferSummaryStatusSchema = z.enum([
  "copied",
  "partial",
  "failed",
  "cancelled",
  "uncertain",
]);
export type TransferSummaryStatus = z.infer<typeof transferSummaryStatusSchema>;

export const transferBoundsSchema = z.object({
  max_bundle_bytes: z.number().int().nonnegative(),
  max_expanded_bytes: z.number().int().nonnegative(),
  max_entries: z.number().int().nonnegative(),
  max_sources: z.number().int().nonnegative(),
  max_concurrency: z.number().int().nonnegative(),
  bundle_available: z.boolean(),
});
export type TransferBounds = z.infer<typeof transferBoundsSchema>;

export const transferPlanSourceSchema = z.object({
  name: z.string(),
  kind: fileKindSchema,
  size: z.number(),
  conflict: z.boolean(),
});
export type TransferPlanSource = z.infer<typeof transferPlanSourceSchema>;

export const transferPlanSchema = z.object({
  mode: transferModeSchema,
  bounds: transferBoundsSchema,
  sources: z.array(transferPlanSourceSchema),
  conflicts: z.array(z.string()),
  unsupported: z.array(transferPlanSourceSchema),
  available_bytes: z.number().nonnegative().optional(),
  fits: z.boolean().optional(),
});
export type TransferPlan = z.infer<typeof transferPlanSchema>;

export const transferOutcomeSchema = z.object({
  name: z.string(),
  status: transferEntryStatusSchema,
  reason: z.string().optional(),
});
export type TransferOutcome = z.infer<typeof transferOutcomeSchema>;

export const transferSkippedSchema = z.object({
  path: z.string(),
  reason: z.string(),
});
export type TransferSkipped = z.infer<typeof transferSkippedSchema>;

export const transferSummarySchema = z.object({
  transfer_id: z.string(),
  mode: transferModeSchema,
  status: transferSummaryStatusSchema,
  reason: z.string().optional(),
  copied: z.number().int().nonnegative(),
  conflicts: z.number().int().nonnegative(),
  skipped: z.number().int().nonnegative(),
  failed: z.number().int().nonnegative(),
  cancelled: z.number().int().nonnegative(),
  uncertain: z.number().int().nonnegative(),
  entries: z.array(transferOutcomeSchema),
  skipped_count: z.number().int().nonnegative(),
  skipped_samples: z.array(transferSkippedSchema).optional(),
  cleanup_failures: z.number().int().nonnegative().optional(),
});
export type TransferSummary = z.infer<typeof transferSummarySchema>;

const transferPhaseEventSchema = z.object({
  type: z.literal("phase"),
  phase: transferPhaseSchema,
  bytes: z.number().int().nonnegative().optional(),
  total_bytes: z.number().int().nonnegative().optional(),
});

const transferSummaryEventSchema = z.object({
  type: z.literal("summary"),
  status: transferSummaryStatusSchema,
  summary: transferSummarySchema,
});

export const transferEventSchema = z.discriminatedUnion("type", [
  transferPhaseEventSchema,
  transferSummaryEventSchema,
]);
export type TransferEvent = z.infer<typeof transferEventSchema>;

/** The request body shared by the preflight and the transfer routes. */
export const transferRequestSchema = z.object({
  source_root: z.string(),
  source_directory: z.string(),
  sources: z.array(z.string()).min(1),
  destination: z.object({
    server_id: z.string(),
    root: z.string(),
    path: z.string(),
  }),
});
export type TransferRequest = z.infer<typeof transferRequestSchema>;
