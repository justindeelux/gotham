import { z } from "zod";

import type { MonitoringRules } from "@/features/monitoring/api/monitoring";

const rangeMessage = "monitoring.validation.range";
const durationMessage = "monitoring.validation.duration";
const orderMessage = "monitoring.validation.order";

const percent = z
  .number({ error: rangeMessage })
  .refine((value) => Number.isFinite(value) && value > 0 && value <= 100, {
    message: rangeMessage,
  });

const duration = z
  .number({ error: durationMessage })
  .int(durationMessage)
  .min(10, durationMessage)
  .max(3600, durationMessage);

export const ruleSchema = z.object({
  enabled: z.boolean(),
  warning: percent,
  clear: percent,
  confirm_seconds: duration,
  clear_seconds: duration,
});

export const filesystemRuleSchema = z.object({
  enabled: z.boolean(),
  warning: percent,
  critical: percent,
  downgrade: percent,
  clear: percent,
  confirm_seconds: duration,
  critical_seconds: duration,
  downgrade_seconds: duration,
  clear_seconds: duration,
});

export const monitoringRulesSchema = z
  .object({
    cpu: ruleSchema,
    memory: ruleSchema,
    space: filesystemRuleSchema,
    inode: filesystemRuleSchema,
  })
  .superRefine((value, ctx) => {
    for (const key of ["cpu", "memory"] as const) {
      if (value[key].clear >= value[key].warning) {
        ctx.addIssue({ code: "custom", path: [key, "clear"], message: orderMessage });
      }
    }
    for (const key of ["space", "inode"] as const) {
      const rule = value[key];
      const ordered =
        rule.clear < rule.downgrade &&
        rule.downgrade <= rule.warning &&
        rule.warning < rule.critical;
      if (!ordered) {
        ctx.addIssue({ code: "custom", path: [key, "critical"], message: orderMessage });
      }
    }
  });

export function isValidRules(rules: MonitoringRules): boolean {
  return monitoringRulesSchema.safeParse(rules).success;
}
