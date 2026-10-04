export interface EngineMeta {
  value: string;
  label: string;
  repo: string;
  defaultVersion: string;
  versions: string[];
  port: number;
}

/**
 * Engine catalogue mirroring internal/databases (*Engine.Image/PortSpec).
 * The version list offers known-good tags; any tag matching the backend
 * version pattern is accepted via the custom input is not needed — the
 * select covers the defaults and recent majors.
 */
export const ENGINES: EngineMeta[] = [
  {
    value: "postgres",
    label: "PostgreSQL",
    repo: "postgres",
    defaultVersion: "16-alpine",
    versions: ["16-alpine", "15-alpine", "14-alpine"],
    port: 5432,
  },
  {
    value: "mysql",
    label: "MySQL",
    repo: "mysql",
    defaultVersion: "8.4",
    versions: ["8.4", "8.0"],
    port: 3306,
  },
  {
    value: "mariadb",
    label: "MariaDB",
    repo: "mariadb",
    defaultVersion: "11.4",
    versions: ["11.4", "11", "10.11"],
    port: 3306,
  },
  {
    value: "mongodb",
    label: "MongoDB",
    repo: "mongo",
    defaultVersion: "7.0",
    versions: ["7.0", "6.0"],
    port: 27017,
  },
  {
    value: "redis",
    label: "Redis",
    repo: "redis",
    defaultVersion: "7.2-alpine",
    versions: ["7.2-alpine", "7.0-alpine"],
    port: 6379,
  },
];

/** engineByValue resolves the catalogue entry, falling back to PostgreSQL. */
export function engineByValue(value: string): EngineMeta {
  return ENGINES.find((item) => item.value === value) ?? ENGINES[0];
}

/** versionOptionsFor lists the image-tag choices of one engine. */
export function versionOptionsFor(
  engine: EngineMeta,
): Array<{ label: string; value: string }> {
  return engine.versions.map((tag) => ({
    label: `${engine.repo}:${tag}`,
    value: tag,
  }));
}

/** effectiveVersionFor applies the engine default when no tag is picked. */
export function effectiveVersionFor(
  engine: EngineMeta,
  version: string,
): string {
  return version || engine.defaultVersion;
}

/** imagePreviewFor renders the repo:tag preview of one engine choice. */
export function imagePreviewFor(engine: EngineMeta, version: string): string {
  return `${engine.repo}:${effectiveVersionFor(engine, version)}`;
}
