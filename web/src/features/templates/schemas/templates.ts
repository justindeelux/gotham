import { z } from "zod";

import type { TemplateField, TemplateValues } from "@/features/templates/api/templates";
import { activeLocale, i18n } from "@/shared/i18n";
import type { ValidationMessageParams } from "@/shared/i18n";

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

function matchesPattern(pattern: string, value: string): boolean {
  try {
    return new RegExp(`^(?:${pattern})$`).test(value);
  } catch {
    return true;
  }
}

export function templateFieldSchema(field: TemplateField): z.ZodType<string, string> {
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
        error: templateMessages.bool,
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

export function resolveTemplateMessage(field: TemplateField, message: string): string {
  void activeLocale.value;
  if (i18n.global.te(message)) {
    return String(i18n.global.t(message, templateMessageParams(field, message) ?? {}));
  }
  return message;
}

export function checkTemplateField(field: TemplateField, value: string): string | null {
  const result = templateFieldSchema(field).safeParse(value);
  if (result.success) {
    return null;
  }
  const message = result.error.issues[0]?.message ?? "Invalid value";
  return resolveTemplateMessage(field, message);
}

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
