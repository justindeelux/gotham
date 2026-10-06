import { z } from "zod";

import type { TemplateField, TemplateValues } from "@/features/templates/api/templates";
import { activeLocale, i18n } from "@/shared/i18n";
import type { ValidationMessageParams } from "@/shared/i18n";

/**
 * Template form schemas (V5). Each builder mirrors `checkTemplateValue` in
 * `api/templates.ts`, which itself mirrors `Field.check` in
 * `internal/templates/render.go`. Schemas store namespaced message keys
 * (see templateMessages); checkTemplateField resolves them to display text
 * at invocation time, so English output stays identical to the historic
 * literals while Vietnamese renders from the templates catalog.
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

/** Message catalog: namespaced keys resolved at invocation time (I18N-7).
 * Parameterized entries store the key only; checkTemplateField derives the
 * interpolation params from the field under validation, so visible feedback
 * refreshes on a language switch while English output stays byte-identical
 * to the previous literals. */
export const templateMessages = {
  required: "templates.validation.required",
  pattern: "templates.validation.pattern",
  wholeNumber: "templates.validation.wholeNumber",
  bool: "templates.validation.bool",
  maxLength: "templates.validation.maxLength",
  minNumber: "templates.validation.minNumber",
  maxNumber: "templates.validation.maxNumber",
  selectOptions: "templates.validation.selectOptions",
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
            message: templateMessages.maxLength,
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
            message: templateMessages.minNumber,
          });
          return;
        }
        if (field.max !== undefined && parsed > field.max) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: templateMessages.maxNumber,
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
          message: templateMessages.selectOptions,
        });
      });
    default:
      return z.string();
  }
}

/**
 * templateMessageParams derives the interpolation params for a stored
 * validation key from the field under validation. The schema stores the key
 * only, so the values (limits, options) always come from the live field —
 * never from a translated string — and switching languages re-renders the
 * same constraint in the new locale.
 */
export function templateMessageParams(
  field: TemplateField,
  message: string,
): ValidationMessageParams | undefined {
  switch (message) {
    case templateMessages.maxLength:
      return field.max_length !== undefined ? { max: field.max_length } : undefined;
    case templateMessages.minNumber:
      return field.min !== undefined ? { min: field.min } : undefined;
    case templateMessages.maxNumber:
      return field.max !== undefined ? { max: field.max } : undefined;
    case templateMessages.selectOptions:
      return { options: (field.options ?? []).join(", ") };
    default:
      return undefined;
  }
}

/**
 * resolveTemplateMessage renders one stored schema message in the active
 * locale at invocation time. Namespaced keys resolve through the composer
 * (with field-derived params when they take any); anything else passes
 * through unchanged so provider diagnostics stay byte-identical.
 */
export function resolveTemplateMessage(field: TemplateField, message: string): string {
  // Tracks the locale when called during render or inside a computed, so
  // visible feedback refreshes on a language switch.
  void activeLocale.value;
  if (i18n.global.te(message)) {
    return String(i18n.global.t(message, templateMessageParams(field, message) ?? {}));
  }
  return message;
}

/**
 * checkTemplateField validates one field value, returning the first message.
 * Backs `checkTemplateValue` in `api/templates.ts`.
 */
export function checkTemplateField(field: TemplateField, value: string): string | null {
  const result = templateFieldSchema(field).safeParse(value);
  if (result.success) {
    return null;
  }
  const message = result.error.issues[0]?.message ?? "Invalid value";
  return resolveTemplateMessage(field, message);
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
