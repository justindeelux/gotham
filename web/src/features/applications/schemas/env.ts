/**
 * Environment-name convention (V6). The API only rejects structurally
 * impossible names (empty, too long, spaces/`=`/NUL — see `validateEnvKey` in
 * `internal/deploy/applications.go`); the `^[A-Z][A-Z0-9_]*$` shape is a
 * warning, never a block. These predicates therefore stay plain non-failing
 * functions, never zod rules. EnvKeyRow is a plain interface for the same
 * reason: no runtime schema validates these rows.
 */

/** EnvKeyRow is the minimal shape shared by the env editors and the wizard. */
export interface EnvKeyRow {
  key: string;
  value: string;
}

const RECOMMENDED_ENV_KEY = /^[A-Z][A-Z0-9_]*$/;

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
  return rows.filter((row) => row.key.trim() === "" && row.value.trim() !== "").length;
}
