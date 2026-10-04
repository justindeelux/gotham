/** Engine default port, used to build the internal connection string. */
export const enginePorts: Record<string, number> = {
  postgres: 5432,
  mysql: 3306,
  mariadb: 3306,
  mongodb: 27017,
  redis: 6379,
};

/** URL scheme for each engine's connection string. */
export function connectionScheme(engine: string): string {
  switch (engine) {
    case "postgres":
      return "postgresql";
    case "mysql":
      return "mysql";
    case "mariadb":
      return "mariadb";
    case "mongodb":
      return "mongodb";
    case "redis":
      return "redis";
    default:
      return engine;
  }
}

/**
 * dbContainerName mirrors the backend's containerName rule
 * (internal/databases/spec.go): lowercased, non-Docker characters collapsed
 * to dashes, truncated to 32 chars, suffixed with the id head. The API
 * exposes no container-name field, so the internal DSN must be derived — a
 * guessed `gotham-db-<name>` would not resolve.
 */
export function dbContainerName(name: string, id: string): string {
  const sanitized = name
    .toLowerCase()
    .replace(/[^a-z0-9_.-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 32)
    .replace(/^-+|-+$/g, "");
  const suffix = id.slice(0, 8);
  return sanitized === "" ? `gotham-db-${suffix}` : `gotham-db-${sanitized}-${suffix}`;
}

/** maskConnectionPassword hides the password of a DSN for on-screen rendering. */
export function maskConnectionPassword(connectionString: string): string {
  return connectionString.replace(/:[^:@/]+@/, ":••••••••@");
}
