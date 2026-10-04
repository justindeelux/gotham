import type { FormItemRule } from "naive-ui";
import { z } from "zod";

export interface RuleFromOptions {
  /**
   * when gates a conditional-required field (e.g. V3 keyName/keyId/password
   * required only for one authMode/keyMode). It reads the same reactive
   * state the current computed rules read. When it returns false the rule
   * passes without touching the schema.
   */
  when?: () => boolean;
}

/**
 * ruleFrom adapts a zod field schema to one Naive UI FormItemRule.
 * safeParse success returns true; failure returns Error(first issue message)
 * so the schema's message string is what the user sees. Trigger handling is
 * unchanged by the caller. No form is migrated yet; see docs/library-audit.md.
 */
export function ruleFrom<T>(
  schema: z.ZodType<T>,
  opts?: RuleFromOptions,
): FormItemRule {
  return {
    validator: (_rule: unknown, value: unknown): boolean | Error => {
      if (opts?.when && !opts.when()) {
        return true;
      }
      const result = schema.safeParse(value);
      if (result.success) {
        return true;
      }
      return new Error(firstIssueMessage(result.error));
    },
  };
}

/**
 * rulesFor builds one FormItemRule per field from a shape of field schemas.
 * Keys match the form model so the result spreads straight into FormRules.
 */
export function rulesFor<T extends Record<string, z.ZodType>>(
  shape: T,
  opts?: Partial<Record<keyof T, RuleFromOptions>>,
): Record<keyof T, FormItemRule> {
  const rules = {} as Record<keyof T, FormItemRule>;
  for (const key of Object.keys(shape) as Array<keyof T>) {
    rules[key] = ruleFrom(shape[key], opts?.[key]);
  }
  return rules;
}

/**
 * fieldErrors validates outside Naive UI (plain computed guards, submit
 * checks) and returns every issue message, empty on success.
 */
export function fieldErrors<T>(schema: z.ZodType<T>, value: unknown): string[] {
  const result = schema.safeParse(value);
  if (result.success) {
    return [];
  }
  return result.error.issues.map((issue) => issue.message);
}

/** firstIssueMessage keeps the single user-visible string deterministic. */
export function firstIssueMessage(error: z.ZodError): string {
  return error.issues[0]?.message ?? "Invalid value";
}
