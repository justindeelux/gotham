/**
 * Register password strength, following docs/design/login.html. Shared by
 * RegisterPage and the PasswordStrengthMeter component so the meter, the
 * label, and the submit validation agree on one definition. Labels resolve
 * from the auth feature catalog for the given locale (callers pass the
 * active locale); the util itself stays dependency-free so unit tests and
 * build scripts can import it without the vue-i18n runtime.
 */
import type { AuthMessages } from "@/features/auth/locales/en";
import enCatalog from "@/features/auth/locales/en";
import viCatalog from "@/features/auth/locales/vi";

/** StrengthLocale selects the meter label language. */
export type StrengthLocale = "en" | "vi";

const strengthTables: Record<StrengthLocale, AuthMessages["strength"]> = {
  en: enCatalog.strength,
  vi: viCatalog.strength,
};

/** countCharClasses counts the character classes present (lower, upper, digit, symbol). */
export function countCharClasses(value: string): number {
  let classes = 0;
  if (/[a-z]/.test(value)) {
    classes += 1;
  }
  if (/[A-Z]/.test(value)) {
    classes += 1;
  }
  if (/[0-9]/.test(value)) {
    classes += 1;
  }
  if (/[^A-Za-z0-9]/.test(value)) {
    classes += 1;
  }
  return classes;
}

/** scorePassword rates the password 0-4 following docs/design/login.html. */
export function scorePassword(value: string): number {
  let score = 0;
  if (value.length >= 10) {
    score += 1;
  }
  if (value.length >= 14) {
    score += 1;
  }
  if (/[a-z]/.test(value) && /[A-Z]/.test(value)) {
    score += 1;
  }
  if (/[0-9]/.test(value)) {
    score += 1;
  }
  if (/[^A-Za-z0-9]/.test(value)) {
    score += 1;
  }
  return Math.min(4, score);
}

/** strengthOf maps a password to its 0-4 meter score (0 when empty). */
export function strengthOf(password: string): number {
  return password ? Math.max(1, scorePassword(password)) : 0;
}

/** strengthLabelOf names a 0-4 meter score in the given locale. */
export function strengthLabelOf(score: number, locale: StrengthLocale = "en"): string {
  const table = strengthTables[locale] ?? strengthTables.en;
  const level = Math.min(Math.max(Math.trunc(score), 0), 4) as 0 | 1 | 2 | 3 | 4;
  return table[`level${level}`] ?? table.level0;
}

/** strengthKindOf picks the meter bar class for a 0-4 score. */
export function strengthKindOf(score: number): string {
  return score >= 4 ? "on" : score === 3 ? "mid" : "weak";
}

/** meetsPasswordPolicy is the submit gate: 10+ chars with 2+ classes. */
export function meetsPasswordPolicy(value: string): boolean {
  return value.length >= 10 && countCharClasses(value) >= 2;
}
