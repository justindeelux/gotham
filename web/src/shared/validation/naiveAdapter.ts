import type { FormItemRule } from "naive-ui";
import { z } from "zod";

import { resolveValidationMessage } from "@/shared/i18n";
import type { ValidationMessageParams } from "@/shared/i18n";

export interface RuleFromOptions {
  when?: () => boolean;
  required?: boolean;
  params?: ValidationMessageParams;
}

export function ruleFrom<T>(
  schema: z.ZodType<T, z.ZodTypeDef, unknown>,
  opts?: RuleFromOptions,
): FormItemRule {
  return {
    ...(opts?.required === true ? { required: true as const } : {}),
    validator: (_rule: unknown, value: unknown): boolean | Error => {
      if (opts?.when && !opts.when()) {
        return true;
      }
      const result = schema.safeParse(value);
      if (result.success) {
        return true;
      }
      return new Error(firstIssueMessage(result.error, opts?.params));
    },
  };
}

export function rulesFor<T extends Record<string, z.ZodType<unknown, z.ZodTypeDef, unknown>>>(
  shape: T,
  opts?: Partial<Record<keyof T, RuleFromOptions>>,
): Record<keyof T, FormItemRule> {
  const rules = {} as Record<keyof T, FormItemRule>;
  for (const key of Object.keys(shape) as Array<keyof T>) {
    rules[key] = ruleFrom(shape[key], opts?.[key]);
  }
  return rules;
}

export function fieldErrors<T>(
  schema: z.ZodType<T, z.ZodTypeDef, unknown>,
  value: unknown,
  params?: ValidationMessageParams,
): string[] {
  const result = schema.safeParse(value);
  if (result.success) {
    return [];
  }
  return result.error.issues.map((issue) =>
    resolveValidationMessage(issue.message, params),
  );
}

export function firstIssueMessage(
  error: z.ZodError,
  params?: ValidationMessageParams,
): string {
  const message = error.issues[0]?.message ?? "Invalid value";
  return resolveValidationMessage(message, params);
}
