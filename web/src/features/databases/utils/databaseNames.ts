/** Backend name rule from internal/databases/service.go (namePattern). */
export const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;

/** isValidDatabaseName applies the backend name rule to a raw input. */
export function isValidDatabaseName(name: string): boolean {
  return NAME_PATTERN.test(name.trim());
}
