import type { Database } from "@/features/databases/api/databases";

/** Filter chip keys mirroring the databases.html toolbar. */
export type DatabaseFilter = "all" | "running" | "stopped" | "public";

/** matchesFilter applies the active status chip to one database. */
export function matchesFilter(
  database: Database,
  filter: DatabaseFilter,
): boolean {
  switch (filter) {
    case "running":
      return database.status === "running";
    case "stopped":
      return database.status === "stopped";
    case "public":
      return database.public_port > 0;
    case "all":
    default:
      return true;
  }
}

/** matchesSearch applies the name/engine/node query to one database. */
export function matchesSearch(
  database: Database,
  query: string,
  serverNameOf: (_serverId: string) => string,
): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  const haystacks = [
    database.name,
    database.engine,
    serverNameOf(database.server_id),
  ];
  return haystacks.some((field) => field.toLowerCase().includes(needle));
}

/** engineLabel renders engine + version + image hint. */
export function engineLabel(database: Database): string {
  return database.version
    ? `${database.engine}:${database.version}`
    : database.engine;
}

/** engineBreakdown summarizes engines for the KPI subtitle. */
export function engineBreakdown(databases: Database[]): string {
  const counts = new Map<string, number>();
  for (const item of databases) {
    counts.set(item.engine, (counts.get(item.engine) ?? 0) + 1);
  }
  if (counts.size === 0) {
    return "None yet";
  }
  return [...counts.entries()]
    .map(([engine, count]) => `${count} ${engine}`)
    .join(" · ");
}

/** filterCounts renders live counts for the filter chips; never invented. */
export function filterCounts(
  databases: Database[],
): Record<DatabaseFilter, number> {
  return {
    all: databases.length,
    running: databases.filter((item) => item.status === "running").length,
    stopped: databases.filter((item) => item.status === "stopped").length,
    public: databases.filter((item) => item.public_port > 0).length,
  };
}
