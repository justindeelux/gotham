import { z } from "zod";

export interface ParseWithOptions {
  /** context names the envelope in the warn log (e.g. "TemplateListEnvelope"). */
  context?: string;
  /**
   * strict throws on parse failure instead of returning the raw payload.
   * For unit tests only; pages always run warn-only so server/client skew
   * never breaks a render.
   */
  strict?: boolean;
}

/**
 * parseWith validates an API response envelope at the axios boundary.
 * Warn-only by default: logs the zod issues with context and returns the raw
 * payload cast, so a newer server shape degrades to a warning, not a blank
 * page. Escalation to surfacing errors waits for one release of telemetry.
 */
export function parseWith<T>(
  schema: z.ZodType<T>,
  data: unknown,
  opts?: ParseWithOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) {
    return result.data;
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
