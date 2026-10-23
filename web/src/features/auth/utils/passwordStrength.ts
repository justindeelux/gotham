import type { AuthMessages } from "@/features/auth/locales/en";
import enCatalog from "@/features/auth/locales/en";
import viCatalog from "@/features/auth/locales/vi";

export type StrengthLocale = "en" | "vi";

const strengthTables: Record<StrengthLocale, AuthMessages["strength"]> = {
  en: enCatalog.strength,
  vi: viCatalog.strength,
};

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

export function strengthOf(password: string): number {
  return password ? Math.max(1, scorePassword(password)) : 0;
}

export function strengthLabelOf(score: number, locale: StrengthLocale = "en"): string {
  const table = strengthTables[locale] ?? strengthTables.en;
  const level = Math.min(Math.max(Math.trunc(score), 0), 4) as 0 | 1 | 2 | 3 | 4;
  return table[`level${level}`] ?? table.level0;
}

export function strengthKindOf(score: number): string {
  return score >= 4 ? "on" : score === 3 ? "mid" : "weak";
}

export function meetsPasswordPolicy(value: string): boolean {
  return value.length >= 10 && countCharClasses(value) >= 2;
}
