/**
 * The API only rejects structurally impossible environment names (empty, too
 * long, or containing spaces/'='/NUL — see `validateEnvKey` in
 * `internal/deploy/applications.go`). The conventional `^[A-Z][A-Z0-9_]*$`
 * shape is a warning, never a block: the wizard must not reject a name the API
 * accepts.
 */
const RECOMMENDED_ENV_KEY = /^[A-Z][A-Z0-9_]*$/;

/** EnvKeyRow is the minimal shape shared by the wizard's env rows. */
export interface EnvKeyRow {
  key: string;
  value: string;
}

/** isRecommendedEnvKey reports whether a name follows the convention. */
export function isRecommendedEnvKey(key: string): boolean {
  return RECOMMENDED_ENV_KEY.test(key.trim());
}

/** hasEnvKeyWarnings reports named rows that deviate from the convention. */
export function hasEnvKeyWarnings(rows: EnvKeyRow[]): boolean {
  return rows.some((row) => row.key.trim() !== "" && !isRecommendedEnvKey(row.key));
}

/**
 * countDroppedEnvRows counts rows the create payload drops: a nameless row
 * that still carries a value would otherwise disappear silently.
 */
export function countDroppedEnvRows(rows: EnvKeyRow[]): number {
  return rows.filter((row) => row.key.trim() === "" && row.value.trim() !== "")
    .length;
}
