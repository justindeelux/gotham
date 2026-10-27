import { z } from "zod";

export const runtimeMonitorGraceSchema = z
  .number()
  .int()
  .min(30)
  .max(600);

export function isRuntimeMonitorGraceValid(value: unknown): boolean {
  return runtimeMonitorGraceSchema.safeParse(value).success;
}
