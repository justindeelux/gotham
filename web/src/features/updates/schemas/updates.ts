import type { FormRules } from "naive-ui";
import { z } from "zod";

import { intInRange } from "@/shared/validation/primitives";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

/**
 * Update schemas (JUS-93). Response schemas validate the API envelopes
 * (warn-only through parseWith); field schemas back the schedule form and
 * carry namespaced i18n keys as messages. Bounds mirror Schedule.Validate in
 * internal/updates/schedule.go.
 */
export const updateMessages = {
  intervalRange: "updates.validation.intervalRange",
  timeFormat: "updates.validation.timeFormat",
  autoApplyNeedsCheck: "updates.validation.autoApplyNeedsCheck",
} as const;

export const channelSchema = z.enum(["stable", "beta"]);
export const frequencySchema = z.enum(["interval", "daily", "weekly"]);
export type UpdateChannel = z.infer<typeof channelSchema>;
export type UpdateFrequency = z.infer<typeof frequencySchema>;

export const intervalHoursSchema = intInRange(updateMessages.intervalRange, { min: 1, max: 720 });
export const atTimeSchema = z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, updateMessages.timeFormat);

const lastUpdateSchema = z.object({
  result: z.string(),
  version: z.string().optional(),
  detail: z.string().optional(),
  at: z.string().optional(),
});
export type LastUpdate = z.infer<typeof lastUpdateSchema>;

export const checkSchema = z.object({
  current: z.string(),
  available: z.boolean(),
  version: z.string().optional(),
  channel: z.string().optional(),
  notes: z.string().optional(),
  published_at: z.string().optional(),
  last_update: lastUpdateSchema.optional(),
});
export type UpdateCheck = z.infer<typeof checkSchema>;

export const scheduleSchema = z.object({
  check_enabled: z.boolean(),
  auto_apply: z.boolean(),
  channel: channelSchema,
  frequency: frequencySchema,
  interval_minutes: z.number(),
  at_time: z.string(),
  weekday: z.number(),
});
export type UpdateSchedule = z.infer<typeof scheduleSchema>;

export const scheduleStateSchema = z.object({
  schedule: scheduleSchema,
  timezone: z.string(),
  next_run_at: z.string().optional(),
  last_checked_at: z.string().optional(),
  last_check_error: z.string().optional(),
  backoff: z.object({ version: z.string(), until: z.string() }).optional(),
});
export type ScheduleState = z.infer<typeof scheduleStateSchema>;

/** scheduleRules validates the editable form fields (interval hours, time). */
export function scheduleRules(): FormRules {
  return {
    intervalHours: [ruleFrom(intervalHoursSchema)],
    atTime: [ruleFrom(atTimeSchema)],
  };
}
