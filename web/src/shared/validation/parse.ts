import { z } from "zod";

export interface ParseWithOptions {
  context?: string;
  strict?: boolean;
}

export function parseWith<T>(
  schema: z.ZodType<T>,
  data: unknown,
  opts?: ParseWithOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) {
    return data as T;
  }
  const label = opts?.context ? ` (${opts.context})` : "";
  if (opts?.strict) {
    throw new Error(
      `Response failed schema${label}: ${firstSentence(result.error)}`,
    );
  }
  console.warn(`[validation] response failed schema${label}:`, result.error.issues);
  return data as T;
}

function firstSentence(error: z.ZodError): string {
  return error.issues[0]?.message ?? "unknown issue";
}
