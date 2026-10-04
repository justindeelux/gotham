import { z } from "zod";

/**
 * Shared field primitives. A primitive lives here only when two or more
 * features need the identical shape (servers + databases port range is the
 * first case); single-feature shapes stay in features/<m>/schemas/<name>.ts
 * next to their message catalog. Convention per feature module: the schemas
 * file exports zod schemas plus inferred types
 * (`export type Connection = z.infer<typeof connectionSchema>`).
 * Messages here are generic fallbacks; migrated features keep their EXACT
 * existing strings via their own catalog (see messages.ts).
 */

/** portSchema covers every 1-65535 TCP port field (servers, databases). */
export const portSchema = z
  .number()
  .int()
  .min(1, "Port must be between 1 and 65535")
  .max(65535, "Port must be between 1 and 65535");

/** nonEmptyString trims and rejects blank input with the caller's message. */
export function nonEmptyString(message: string): z.ZodString {
  return z.string().trim().min(1, message);
}
