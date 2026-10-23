export const enginePorts: Record<string, number> = {
  postgres: 5432,
  mysql: 3306,
  mariadb: 3306,
  mongodb: 27017,
  redis: 6379,
};

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

export function maskConnectionPassword(connectionString: string): string {
  return connectionString.replace(/:[^:@/]+@/, ":••••••••@");
}
