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
export const portSchema = intInRange("Port must be between 1 and 65535", {
  min: 1,
  max: 65535,
});

/** nonEmptyString trims and rejects blank input with the caller's message. */
export function nonEmptyString(message: string): z.ZodString {
  return z.string().trim().min(1, message);
}

/**
 * requiredString is a catalog-safe required text field: undefined, null, ""
 * and whitespace all report the caller's message, never a zod default.
 * See messages.ts for why the bare chain is not enough.
 */
export function requiredString(message: string): z.ZodString {
  return z
    .string({ required_error: message, invalid_type_error: message })
    .trim()
    .min(1, message);
}

export interface IntRange {
  min?: number;
  max?: number;
}

/**
 * intInRange is a catalog-safe integer field for NInputNumber values (which
 * emit null when cleared and NaN for unparseable input): null, NaN,
 * non-integers and out-of-range values all report the caller's message.
 */
export function intInRange(message: string, range?: IntRange): z.ZodNumber {
  let schema = z.number({
    required_error: message,
    invalid_type_error: message,
  });
  if (range?.min !== undefined) {
    schema = schema.min(range.min, message);
  }
  if (range?.max !== undefined) {
    schema = schema.max(range.max, message);
  }
  return schema.int(message);
}
