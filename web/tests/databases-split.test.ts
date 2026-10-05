// Unit tests for the pure helpers extracted by the JUS-24 databases split.
// They pin the exact behaviour the monoliths had inline. (PE-5 removed the
// flat DatabasesPage with its table, KPI row and empty state; the pure
// utils below stay and are still pinned here.)
import { describe, expect, it } from "vitest";

import {
  connectionScheme,
  dbContainerName,
  maskConnectionPassword,
} from "../src/features/databases/utils/databaseConnection";
import {
  effectiveVersionFor,
  engineByValue,
  ENGINES,
  imagePreviewFor,
  versionOptionsFor,
} from "../src/features/databases/utils/databaseEngines";
import {
  engineBreakdown,
  engineLabel,
  filterCounts,
  matchesFilter,
  matchesSearch,
} from "../src/features/databases/utils/databaseFilters";
import { statusTagType } from "../src/features/databases/utils/backupStatus";

function row(overrides = {}) {
  return {
    id: "db-1",
    name: "pg-orders",
    environment_id: "env-1",
    environment_name: "production",
    project_id: "proj-1",
    project_name: "shop",
    engine: "postgres",
    version: "16-alpine",
    status: "running",
    server_id: "srv-1",
    server_name: "node",
    container_id: "c-1",
    public_port: 0,
    volume: "vol-1",
    created_at: "2026-09-01T12:00:00.000Z",
    updated_at: "2026-09-01T12:00:00.000Z",
    ...overrides,
  };
}

describe("databaseEngines", () => {
  it("falls back to PostgreSQL for unknown engines", () => {
    expect(engineByValue("nope").value).toBe("postgres");
    expect(engineByValue("redis").port).toBe(6379);
  });

  it("applies the engine default version and builds image previews", () => {
    const engine = engineByValue("postgres");
    expect(effectiveVersionFor(engine, "")).toBe("16-alpine");
    expect(effectiveVersionFor(engine, "15-alpine")).toBe("15-alpine");
    expect(imagePreviewFor(engine, "")).toBe("postgres:16-alpine");
    expect(versionOptionsFor(engine)[0]).toEqual({
      label: "postgres:16-alpine",
      value: "16-alpine",
    });
    expect(ENGINES.length).toBe(5);
  });
});

describe("databaseConnection", () => {
  it("maps engines to URL schemes", () => {
    expect(connectionScheme("postgres")).toBe("postgresql");
    expect(connectionScheme("redis")).toBe("redis");
    expect(connectionScheme("custom")).toBe("custom");
  });

  it("mirrors the backend container-name rule", () => {
    expect(dbContainerName("pg-orders", "abcdef12-xxx")).toBe(
      "gotham-db-pg-orders-abcdef12",
    );
    expect(dbContainerName("UPPER spaced!", "12345678-xxx")).toBe(
      "gotham-db-upper-spaced-12345678",
    );
    expect(dbContainerName("!!!", "12345678-xxx")).toBe("gotham-db-12345678");
  });

  it("masks the password for display", () => {
    expect(
      maskConnectionPassword("postgresql://u:secret@host:5432/db"),
    ).toBe("postgresql://u:••••••••@host:5432/db");
  });
});

describe("databaseFilters", () => {
  const running = row();
  const stopped = row({ id: "db-2", status: "stopped" });
  const publicDb = row({ id: "db-3", public_port: 15432 });

  it("matches the status chips", () => {
    expect(matchesFilter(running, "all")).toBe(true);
    expect(matchesFilter(running, "running")).toBe(true);
    expect(matchesFilter(stopped, "running")).toBe(false);
    expect(matchesFilter(stopped, "stopped")).toBe(true);
    expect(matchesFilter(publicDb, "public")).toBe(true);
    expect(matchesFilter(running, "public")).toBe(false);
  });

  it("matches the search query across name, engine and node", () => {
    const names = (id: string) => (id === "srv-1" ? "node-1" : id);
    expect(matchesSearch(running, "", names)).toBe(true);
    expect(matchesSearch(running, "orders", names)).toBe(true);
    expect(matchesSearch(running, "POSTGRES", names)).toBe(true);
    expect(matchesSearch(running, "node-1", names)).toBe(true);
    expect(matchesSearch(running, "mysql", names)).toBe(false);
  });

  it("labels engines and summarizes the population", () => {
    expect(engineLabel(running)).toBe("postgres:16-alpine");
    expect(engineLabel(row({ version: "" }))).toBe("postgres");
    expect(engineBreakdown([])).toBe("None yet");
    expect(engineBreakdown([running, stopped, publicDb])).toBe("3 postgres");
    expect(filterCounts([running, stopped, publicDb])).toEqual({
      all: 3,
      running: 2,
      stopped: 1,
      public: 1,
    });
  });
});

describe("statusTagType", () => {
  it("maps backup and restore statuses", () => {
    expect(statusTagType("completed")).toBe("success");
    expect(statusTagType("failed")).toBe("error");
    expect(statusTagType("running")).toBe("warning");
  });
});
