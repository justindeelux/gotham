import { z } from "zod";

export const RUNTIME_MONITOR_MIN_GRACE_SECONDS = 30;
export const RUNTIME_MONITOR_MAX_GRACE_SECONDS = 600;
export const RUNTIME_MONITOR_DEFAULT_GRACE_SECONDS = 120;

export const runtimeMonitorGraceSchema = z
  .number()
  .int()
  .min(RUNTIME_MONITOR_MIN_GRACE_SECONDS)
  .max(RUNTIME_MONITOR_MAX_GRACE_SECONDS);

export function isRuntimeMonitorGraceValid(value: unknown): boolean {
  return runtimeMonitorGraceSchema.safeParse(value).success;
}
