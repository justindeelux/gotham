export const SECRET_REF_PREFIX = "secret:";

export function isSecretValue(value: string): boolean {
  return value.startsWith(SECRET_REF_PREFIX);
}
