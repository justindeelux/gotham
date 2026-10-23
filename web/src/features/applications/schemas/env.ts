
export interface EnvKeyRow {
  key: string;
  value: string;
}

const RECOMMENDED_ENV_KEY = /^[A-Z][A-Z0-9_]*$/;

export function isRecommendedEnvKey(key: string): boolean {
  return RECOMMENDED_ENV_KEY.test(key.trim());
}

export function hasEnvKeyWarnings(rows: EnvKeyRow[]): boolean {
  return rows.some((row) => row.key.trim() !== "" && !isRecommendedEnvKey(row.key));
}

export function countDroppedEnvRows(rows: EnvKeyRow[]): number {
  return rows.filter((row) => row.key.trim() === "" && row.value.trim() !== "").length;
}
