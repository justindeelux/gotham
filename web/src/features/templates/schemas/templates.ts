import { z } from "zod";

import type { TemplateField, TemplateValues } from "@/features/templates/api/templates";
import { firstIssueMessage } from "@/shared/validation/naiveAdapter";

/**
 * Template form schemas (V5). Each builder mirrors `checkTemplateValue` in
 * `api/templates.ts`, which itself mirrors `Field.check` in
 * `internal/templates/render.go`. Message strings are preserved verbatim.
 *
 * Deliberate regex exceptions: the field `pattern` is anchored exactly like
 * the server (`^(?:pattern)$`) with uncompilable-pattern-passes, because the
 * server rejects bad patterns at catalog load and the client must not block
 * on a pattern from a newer API. The text/secret/number/select checks stay
 * `superRefine` (not composed plain rules) because each field runs several
 * ordered checks with early exit and the first issue must be the exact
 * historic message — splitting them would reorder messages. Only the bool
 * check is a single rule and uses plain `z.enum` with an error map.
 *
 * Documented client/server differences (preserved, server authoritative):
 * bool accepts only exact "true"/"false" (server trims and lowercases);
 * number rejects "+5" (server ParseInt accepts it); an empty value for a
 * required select reports "This field is required." (server reports
 * "must be one of"); values over 1024 bytes are server-only errors.
 */

/** Message catalog: exact strings from the hand-written checks. */
export const templateMessages = {
  required: "This field is required.",
  pattern: "Does not match the required format.",
  wholeNumber: "Must be a whole number.",
  bool: "Must be true or false.",
  maxLength: (max: number): string => `Must be at most ${max} characters.`,
  minNumber: (min: number): string => `Must be at least ${min}.`,
  maxNumber: (max: number): string => `Must be at most ${max}.`,
  selectOptions: (options: string[]): string => `Must be one of: ${options.join(", ")}.`,
} as const;

/** matchesPattern anchors the field pattern exactly like the server does. */
function matchesPattern(pattern: string, value: string): boolean {
  try {
    return new RegExp(`^(?:${pattern})$`).test(value);
  } catch {
    // The server rejects an uncompilable pattern at catalog load, so this can
    // only mean the pattern arrived through a newer API; do not block on it.
    return true;
  }
}

/**
 * templateFieldSchema builds the value schema for one template field.
 * Checks run in the same order as the hand-written code so the first issue
 * is the same message.
 */
export function templateFieldSchema(field: TemplateField): z.ZodType<string, z.ZodTypeDef, string> {
  switch (field.type) {
    case "text":
    case "secret":
      return z.string().superRefine((value, ctx) => {
        if (field.required && value.trim() === "") {
          ctx.addIssue({ code: z.ZodIssueCode.custom, message: templateMessages.required });
          return;
        }
        if (field.max_length !== undefined && [...value].length > field.max_length) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: templateMessages.maxLength(field.max_length),
          });
          return;
        }
        if (field.pattern !== undefined && !matchesPattern(field.pattern, value)) {
          ctx.addIssue({ code: z.ZodIssueCode.custom, message: templateMessages.pattern });
        }
      });
    case "number":
      return z.string().superRefine((value, ctx) => {
        const trimmed = value.trim();
        if (trimmed === "") {
          if (field.required) {
            ctx.addIssue({ code: z.ZodIssueCode.custom, message: templateMessages.required });
          }
          return;
        }
        if (!/^-?\d+$/.test(trimmed)) {
          ctx.addIssue({ code: z.ZodIssueCode.custom, message: templateMessages.wholeNumber });
          return;
        }
        const parsed = Number(trimmed);
        if (field.min !== undefined && parsed < field.min) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: templateMessages.minNumber(field.min),
          });
          return;
        }
        if (field.max !== undefined && parsed > field.max) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: templateMessages.maxNumber(field.max),
          });
        }
      });
    case "bool":
      return z.enum(["true", "false"], {
        errorMap: () => ({ message: templateMessages.bool }),
      });
    case "select":
      return z.string().superRefine((value, ctx) => {
        if ((field.options ?? []).includes(value.trim())) {
          return;
        }
        if (field.required && value.trim() === "") {
          ctx.addIssue({ code: z.ZodIssueCode.custom, message: templateMessages.required });
          return;
        }
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: templateMessages.selectOptions(field.options ?? []),
        });
      });
    default:
      return z.string();
  }
}

/**
 * checkTemplateField validates one field value, returning the first message.
 * Backs `checkTemplateValue` in `api/templates.ts`.
 */
export function checkTemplateField(field: TemplateField, value: string): string | null {
  const result = templateFieldSchema(field).safeParse(value);
  return result.success ? null : firstIssueMessage(result.error);
}

/**
 * validateTemplateFields returns the first message of every invalid field.
 * Backs `validateTemplateValues` in `api/templates.ts`, which also drives the
 * wizard Next gate, so gating and messages share one source of truth.
 */
export function validateTemplateFields(
  fields: TemplateField[],
  values: TemplateValues,
): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const field of fields) {
    const message = checkTemplateField(field, values[field.key] ?? "");
    if (message !== null) {
      errors[field.key] = message;
    }
  }
  return errors;
}
