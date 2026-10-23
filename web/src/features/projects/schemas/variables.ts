import { z } from "zod";

import { activeLocale, i18n } from "@/shared/i18n";
import { parseWith } from "@/shared/validation/parse";

export const maxSharedVariables = 128;

export const maxSharedVariableKeyLength = 128;

export const sharedVariableKeyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/;

const loneSurrogatePattern = /[\uD800-\uDFFF]/;

export const sharedVariableKeySchema = z
  .string()
  .min(1, "projects.validation.keyRequired")
  .max(maxSharedVariableKeyLength, "projects.validation.keyMaxLength")
  .regex(sharedVariableKeyPattern, "projects.validation.keyPattern");

export const sharedVariableValueSchema = z
  .string()
  .refine(
    (value) => !value.includes("\0"),
    "projects.validation.valueNul",
  )
  .refine(
    (value) => !loneSurrogatePattern.test(value),
    "projects.validation.valueUtf8",
  );

export function isSharedVariableKeyValid(value: unknown): boolean {
  return sharedVariableKeySchema.safeParse(value).success;
}

export function isSharedVariableValueValid(value: unknown): boolean {
  return sharedVariableValueSchema.safeParse(value).success;
}

export const sharedVariableSchema = z.object({
  key: z.string(),
  value: z.string().optional(),
  secret: z.boolean(),
});

export const sharedVariablesEnvelopeSchema = z.object({
  variables: z.array(sharedVariableSchema),
});

export interface SharedVariable {
  key: string;
  value?: string;
  secret: boolean;
}

export interface SharedVariableWrite {
  key: string;
  value?: string;
  secret: boolean;
}

export interface VariableDraft {
  key: string;
  value: string;
  secret: boolean;
}

export type SharedVariableOrigin = "project" | "environment";

export interface InheritedVariable extends SharedVariable {
  origin: SharedVariableOrigin;
}

export function inheritedOriginLabel(origin: SharedVariableOrigin): string {
  void activeLocale.value;
  return String(
    i18n.global.t(
      origin === "project"
        ? "projects.variables.originProject"
        : "projects.variables.originEnvironment",
    ),
  );
}

export function parseSharedVariables(
  data: unknown,
): z.infer<typeof sharedVariablesEnvelopeSchema> {
  return parseWith(sharedVariablesEnvelopeSchema, data, {
    context: "SharedVariablesEnvelope",
  });
}

export function findDuplicateVariableKey(drafts: VariableDraft[]): string | null {
  const seen = new Set<string>();
  for (const row of drafts) {
    if (seen.has(row.key)) {
      return row.key;
    }
    seen.add(row.key);
  }
  return null;
}

export function validateVariableDrafts(
  drafts: VariableDraft[],
  existingSecretKeys: ReadonlySet<string>,
): string[] {
  void activeLocale.value;
  const text = (
    key: string,
    params?: Record<string, string | number>,
  ): string => String(i18n.global.t(key, params ?? {}));
  const problems: string[] = [];
  if (drafts.length > maxSharedVariables) {
    problems.push(
      text("projects.variables.problems.cap", {
        max: maxSharedVariables,
        count: drafts.length,
      }),
    );
  }
  const duplicate = findDuplicateVariableKey(drafts);
  if (duplicate !== null) {
    problems.push(text("projects.variables.problems.duplicate", { key: duplicate }));
  }
  for (const row of drafts) {
    if (!isSharedVariableKeyValid(row.key)) {
      problems.push(
        row.key === ""
          ? text("projects.variables.problems.keyMissing")
          : text("projects.variables.problems.keyInvalid", { key: row.key }),
      );
    }
    if (row.value !== "" && !isSharedVariableValueValid(row.value)) {
      problems.push(
        text("projects.variables.problems.valueNul", { key: row.key || "?" }),
      );
    }
    if (row.secret && row.value === "" && !existingSecretKeys.has(row.key)) {
      problems.push(
        text("projects.variables.problems.secretNeedsValue", { key: row.key || "?" }),
      );
    }
    if (!row.secret && row.value === "" && existingSecretKeys.has(row.key)) {
      problems.push(
        text("projects.variables.problems.clearWarning", { key: row.key }),
      );
    }
  }
  return problems;
}

export function buildVariablesPayload(
  drafts: VariableDraft[],
  existingSecretKeys: ReadonlySet<string>,
): SharedVariableWrite[] {
  return drafts.map((row) => {
    if (row.secret && row.value === "" && existingSecretKeys.has(row.key)) {
      return { key: row.key, secret: true };
    }
    return { key: row.key, value: row.value, secret: row.secret };
  });
}

export function toVariableDrafts(variables: SharedVariable[]): VariableDraft[] {
  return variables.map((row) => ({
    key: row.key,
    value: row.secret ? "" : (row.value ?? ""),
    secret: row.secret,
  }));
}

export function existingSecretKeysOf(variables: SharedVariable[]): Set<string> {
  const keys = new Set<string>();
  for (const row of variables) {
    if (row.secret) {
      keys.add(row.key);
    }
  }
  return keys;
}

export function isOverriddenBy(
  key: string,
  appDraft: Array<{ key: string }>,
): boolean {
  return appDraft.some((row) => row.key !== "" && row.key === key);
}

export function isShadowedByEnvironment(
  key: string,
  inherited: Array<{ key: string; origin: SharedVariableOrigin }>,
): boolean {
  if (key === "") {
    return false;
  }
  return inherited.some(
    (row) => row.origin === "environment" && row.key === key,
  );
}

export function rowKeyFromServerError(message: string): string | null {
  const match = /"([^"]+)"/.exec(message);
  return match?.[1] ?? null;
}

export function secretWithoutValueKey(message: string): string | null {
  const match = /secret "([^"]+)" has no value/.exec(message);
  return match?.[1] ?? null;
}
