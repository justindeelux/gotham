import { z } from "zod";

import { cleanupRunItemSchema } from "./cleanup";

export const cleanupScheduleSchema = z.object({
  id: z.string(),
  name: z.string(),
  server_ids: z.array(z.string()),
  categories: z.array(z.string()),
  cron: z.string(),
  min_age_hours: z.number(),
  enabled: z.boolean(),
  next_run_at: z.string(),
  last_run_at: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const cleanupScheduleListSchema = z.object({
  schedules: z.array(cleanupScheduleSchema).optional(),
});

export const cleanupScheduleEnvelopeSchema = z.object({
  schedule: cleanupScheduleSchema,
});

export const cleanupRunSchema = z.object({
  id: z.string(),
  server_id: z.string(),
  trigger: z.string(),
  schedule_id: z.string().optional(),
  schedule_name: z.string().optional(),
  actor: z.string().optional(),
  started_at: z.string(),
  finished_at: z.string(),
  reclaimed_bytes: z.number(),
  status: z.string(),
  reason: z.string().optional(),
  categories: z.record(z.string(), z.number()).optional(),
});

export const cleanupRunListSchema = z.object({
  runs: z.array(cleanupRunSchema).optional(),
});

export const cleanupRunDetailSchema = z.object({
  run: cleanupRunSchema.extend({
    items: z.array(cleanupRunItemSchema).optional(),
  }),
});

export const cleanupNextRunSchema = z.object({
  next_run_at: z.string(),
});

export const cleanupExclusionSchema = z.object({
  id: z.string(),
  server_id: z.string().optional(),
  category: z.string(),
  match: z.string(),
  note: z.string().optional(),
  created_at: z.string(),
});

export const cleanupExclusionListSchema = z.object({
  exclusions: z.array(cleanupExclusionSchema).optional(),
});

export const cleanupExclusionEnvelopeSchema = z.object({
  exclusion: cleanupExclusionSchema,
});

// Dangerous categories are manual-only in every path (GOTHAM-160 tier rule).
export const dangerousCategories = ["orphaned_volumes"] as const;

export const scheduleFormSchema = z
  .object({
    name: z.string().trim().min(1).max(80),
    server_ids: z.array(z.string()).min(1).max(50),
    categories: z.array(z.string()).min(1),
    cron: z.string().trim().min(1),
    min_age_hours: z.number().int().min(1).max(8760),
    enabled: z.boolean(),
  })
  .superRefine((value, ctx) => {
    if (value.categories.some((category) => isDangerousCategory(category))) {
      ctx.addIssue({
        code: "custom",
        path: ["categories"],
        message: "dangerous",
      });
    }
  });

export function isDangerousCategory(category: string): boolean {
  return (dangerousCategories as readonly string[]).includes(category);
}

export type CleanupSchedule = z.infer<typeof cleanupScheduleSchema>;
export type CleanupRun = z.infer<typeof cleanupRunSchema>;
export type CleanupRunDetail = z.infer<typeof cleanupRunDetailSchema>["run"];
export type CleanupExclusion = z.infer<typeof cleanupExclusionSchema>;
export type ScheduleForm = z.infer<typeof scheduleFormSchema>;
