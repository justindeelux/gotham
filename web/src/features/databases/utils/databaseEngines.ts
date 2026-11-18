export interface EngineMeta {
  value: string;
  label: string;
  repo: string;
  defaultVersion: string;
  versions: string[];
  port: number;
  homepage: string;
}

export const ENGINES: EngineMeta[] = [
  {
    value: "postgres",
    label: "PostgreSQL",
    repo: "postgres",
    defaultVersion: "16-alpine",
    versions: ["16-alpine", "15-alpine", "14-alpine"],
    port: 5432,
    homepage: "https://www.postgresql.org",
  },
  {
    value: "mysql",
    label: "MySQL",
    repo: "mysql",
    defaultVersion: "8.4",
    versions: ["8.4", "8.0"],
    port: 3306,
    homepage: "https://www.mysql.com",
  },
  {
    value: "mariadb",
    label: "MariaDB",
    repo: "mariadb",
    defaultVersion: "11.4",
    versions: ["11.4", "11", "10.11"],
    port: 3306,
    homepage: "https://mariadb.org",
  },
  {
    value: "mongodb",
    label: "MongoDB",
    repo: "mongo",
    defaultVersion: "7.0",
    versions: ["7.0", "6.0"],
    port: 27017,
    homepage: "https://www.mongodb.com",
  },
  {
    value: "redis",
    label: "Redis",
    repo: "redis",
    defaultVersion: "7.2-alpine",
    versions: ["7.2-alpine", "7.0-alpine"],
    port: 6379,
    homepage: "https://redis.io",
  },
];

export function engineByValue(value: string): EngineMeta {
  return ENGINES.find((item) => item.value === value) ?? ENGINES[0];
}

export function versionOptionsFor(
  engine: EngineMeta,
): Array<{ label: string; value: string }> {
  return engine.versions.map((tag) => ({
    label: `${engine.repo}:${tag}`,
    value: tag,
  }));
}

export function effectiveVersionFor(
  engine: EngineMeta,
  version: string,
): string {
  return version || engine.defaultVersion;
}

export function imagePreviewFor(engine: EngineMeta, version: string): string {
  return `${engine.repo}:${effectiveVersionFor(engine, version)}`;
}
