import { z } from "zod";

export const portSchema = intInRange("Port must be between 1 and 65535", {
  min: 1,
  max: 65535,
});

export const serviceNameSchema = requiredString("services.validation.nameRequired");

export const serviceNodeSchema = z.string().min(1, "services.validation.nodeRequired");

export function nonEmptyString(message: string): z.ZodString {
  return z.string().trim().min(1, message);
}

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
