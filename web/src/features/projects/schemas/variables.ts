import { z } from "zod";

import { activeLocale, i18n } from "@/shared/i18n";
import { parseWith } from "@/shared/validation/parse";

/**
 * Shared-variable schemas and pure helpers (PE-6, Linear JUS-35). Response
 * shapes mirror the API contract in
 * docs/plans/13-projects-environments.md section 6 exactly:
 *
 *   Variable {key, value?: string, secret: boolean}  // value omitted for secrets
 *   GET  /projects/{id}/variables, /environments/{id}/variables → {variables}
 *   PUT  .../variables `{variables: [{key, value, secret}]}` replaces the whole
 *   set; an omitted `value` for an existing secret key keeps its sealed value.
 *
 * Backend rules pinned here (see `internal/projects/variables.go`):
 * key `^[A-Za-z_][A-Za-z0-9_]*$`, at most 128 keys per scope, keys at most 128
 * chars, values reject NUL, secrets are write-only (never returned), and a
 * plain-to-secret row without a value is a 400.
 */

/** maxSharedVariables caps one PUT body per the contract. */
export const maxSharedVariables = 128;

/** maxSharedVariableKeyLength caps one key, like application env keys. */
export const maxSharedVariableKeyLength = 128;

/** sharedVariableKeyPattern is the contract's key rule. */
export const sharedVariableKeyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Lone surrogates never survive the JSON round trip as valid UTF-8. */
const loneSurrogatePattern = /[\uD800-\uDFFF]/;

/** sharedVariableKeySchema gates one key: 1-128 chars, shell identifier. */
export const sharedVariableKeySchema = z
  .string()
  .min(1, "projects.validation.keyRequired")
  .max(maxSharedVariableKeyLength, "projects.validation.keyMaxLength")
  .regex(sharedVariableKeyPattern, "projects.validation.keyPattern");

/** sharedVariableValueSchema gates one value: no NUL, valid UTF-8. */
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

/** isSharedVariableKeyValid is the single source for the key submit gating. */
export function isSharedVariableKeyValid(value: unknown): boolean {
  return sharedVariableKeySchema.safeParse(value).success;
}

/** isSharedVariableValueValid is the single source for the value gating. */
export function isSharedVariableValueValid(value: unknown): boolean {
  return sharedVariableValueSchema.safeParse(value).success;
}

/** sharedVariableSchema mirrors Variable in the contract. */
export const sharedVariableSchema = z.object({
  key: z.string(),
  value: z.string().optional(),
  secret: z.boolean(),
});

/** sharedVariablesEnvelopeSchema mirrors GET/PUT .../variables. */
export const sharedVariablesEnvelopeSchema = z.object({
  variables: z.array(sharedVariableSchema),
});

/** SharedVariable is one row as the API returns it (secrets carry no value). */
export interface SharedVariable {
  key: string;
  value?: string;
  secret: boolean;
}

/** SharedVariableWrite is one row of a PUT body (value omitted keeps a secret). */
export interface SharedVariableWrite {
  key: string;
  value?: string;
  secret: boolean;
}

/** VariableDraft is one editor row: secrets edit into `value`, empty keeps. */
export interface VariableDraft {
  key: string;
  value: string;
  secret: boolean;
}

/** SharedVariableOrigin names where an inherited row comes from. */
export type SharedVariableOrigin = "project" | "environment";

/** InheritedVariable is a read-only row with its origin for display. */
export interface InheritedVariable extends SharedVariable {
  origin: SharedVariableOrigin;
}

/** inheritedOriginLabel renders the origin tag ("from project"). */
export function inheritedOriginLabel(origin: SharedVariableOrigin): string {
  // Reads the active locale so template callers refresh on a language
  // switch; the wire origin value itself is never translated.
  void activeLocale.value;
  return String(
    i18n.global.t(
      origin === "project"
        ? "projects.variables.originProject"
        : "projects.variables.originEnvironment",
    ),
  );
}

/**
 * parseSharedVariables validates a GET/PUT .../variables payload
 * (warn-only, like every other envelope).
 */
export function parseSharedVariables(
  data: unknown,
): z.infer<typeof sharedVariablesEnvelopeSchema> {
  return parseWith(sharedVariablesEnvelopeSchema, data, {
    context: "SharedVariablesEnvelope",
  });
}

/**
 * findDuplicateVariableKey names the first repeated key in a draft, or null.
 * Case-sensitive like the backend's uniqueness check.
 */
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

/**
 * validateVariableDrafts lists every user-facing problem in a draft, empty
 * when the draft may be saved. `existingSecretKeys` holds the keys that are
 * currently stored as secrets: a secret row with an empty value is only valid
 * when its key is in that set (the save then omits the value and keeps the
 * sealed ciphertext); a new secret without a value would be a 400
 * (`secret "X" has no value`), so it is blocked up front.
 */
export function validateVariableDrafts(
  drafts: VariableDraft[],
  existingSecretKeys: ReadonlySet<string>,
): string[] {
  // Every problem resolves through the active locale at invocation time so
  // visible feedback refreshes on a language switch; English output stays
  // byte-identical to the previous literals.
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
      // Secret-to-plain with an empty value would silently clear the stored
      // ciphertext (the backend allows it): require an explicit value.
      problems.push(
        text("projects.variables.problems.clearWarning", { key: row.key }),
      );
    }
  }
  return problems;
}

/**
 * buildVariablesPayload maps an editor draft onto a PUT body. A secret row
 * whose value is still empty and whose key is a stored secret omits the value
 * (the backend keeps its sealed ciphertext); every other row carries its
 * value verbatim, including empty plain values.
 */
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

/**
 * toVariableDrafts maps a loaded set onto editor rows: plain values edit in
 * place, secrets start empty (write-only: the stored value is never shown).
 */
export function toVariableDrafts(variables: SharedVariable[]): VariableDraft[] {
  return variables.map((row) => ({
    key: row.key,
    value: row.secret ? "" : (row.value ?? ""),
    secret: row.secret,
  }));
}

/** existingSecretKeysOf collects the keys currently stored as secrets. */
export function existingSecretKeysOf(variables: SharedVariable[]): Set<string> {
  const keys = new Set<string>();
  for (const row of variables) {
    if (row.secret) {
      keys.add(row.key);
    }
  }
  return keys;
}

/**
 * isOverriddenBy reports whether an application-level draft shadows an
 * inherited key, so the editor can mark the inherited row "overridden".
 * Case-sensitive like the deploy merge. Takes any key-carrying rows, so both
 * the shared-variables drafts and the application EnvVar rows qualify.
 */
export function isOverriddenBy(
  key: string,
  appDraft: Array<{ key: string }>,
): boolean {
  return appDraft.some((row) => row.key !== "" && row.key === key);
}

/**
 * isShadowedByEnvironment reports whether a project-level key is shadowed by
 * the environment set, so an inherited table can mark the project row
 * "overridden" even when the application overrides neither. Case-sensitive
 * like the deploy merge.
 */
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

/**
 * rowKeyFromServerError extracts a quoted row key from a backend 400 (e.g.
 * `secret "ESEC" has no value`, `duplicate variable key "A"`), so the editor
 * can render it inline on the matching row. Null when no key is named.
 */
export function rowKeyFromServerError(message: string): string | null {
  const match = /"([^"]+)"/.exec(message);
  return match?.[1] ?? null;
}

/**
 * secretWithoutValueKey extracts the key from a `secret "X" has no value`
 * 400. After such a refusal the stored ciphertext is gone (concurrent
 * delete), so the editor drops the key from its stored set and asks for a
 * value instead of retrying the keep path forever.
 */
export function secretWithoutValueKey(message: string): string | null {
  const match = /secret "([^"]+)" has no value/.exec(message);
  return match?.[1] ?? null;
}
