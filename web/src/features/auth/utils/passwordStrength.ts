/**
 * Register password strength, following docs/design/login.html. Shared by
 * RegisterPage and the PasswordStrengthMeter component so the meter, the
 * label, and the submit validation agree on one definition.
 */

export const passwordStrengthLabels = [
  "Not entered",
  "Very weak",
  "Weak",
  "Fair",
  "Strong",
];

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

/** strengthLabelOf names a 0-4 meter score. */
export function strengthLabelOf(score: number): string {
  return passwordStrengthLabels[score] ?? passwordStrengthLabels[0];
}

/** strengthKindOf picks the meter bar class for a 0-4 score. */
export function strengthKindOf(score: number): string {
  return score >= 4 ? "on" : score === 3 ? "mid" : "weak";
}

/** meetsPasswordPolicy is the submit gate: 10+ chars with 2+ classes. */
export function meetsPasswordPolicy(value: string): boolean {
  return value.length >= 10 && countCharClasses(value) >= 2;
}
