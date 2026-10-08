// GS-1 (JUS-57) checks: Add-resource brand marks, catalog parity and the
// database wizard engine preselect the picker relies on.
import { beforeEach, describe, expect, it } from "vitest";

import en from "@/features/add-resource/locales/en";
import vi from "@/features/add-resource/locales/vi";
import { brandFor, initialsOf } from "@/features/add-resource/utils/brands";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import { engineByValue } from "@/features/databases/utils/databaseEngines";

describe("brandFor", () => {
  it("resolves known template and engine keys to their brand mark", () => {
    expect(brandFor("wordpress", "WordPress")).toEqual({
      letters: "W",
      color: "#21759b",
    });
    expect(brandFor("postgres", "PostgreSQL")).toEqual({
      letters: "PG",
      color: "#336791",
    });
  });

  it("falls back to initials on the accent hue for unknown keys", () => {
    expect(brandFor("some-future-template", "Cool Tool")).toEqual({
      letters: "CT",
      color: "#5865f2",
    });
    expect(brandFor("", "")).toEqual({ letters: "?", color: "#5865f2" });
  });
});

describe("initialsOf", () => {
  it("derives one- or two-letter marks from display names", () => {
    expect(initialsOf("n8n")).toBe("N8");
    expect(initialsOf("Uptime Kuma")).toBe("UK");
    expect(initialsOf("")).toBe("?");
  });
});

describe("add-resource catalog", () => {
  it("keeps en/vi parity", () => {
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });

  it("covers every supported database engine", () => {
    for (const engine of ["postgres", "mysql", "mariadb", "mongodb", "redis"]) {
      expect(engineByValue(engine).value).toBe(engine);
      expect(
        (en.engines as Record<string, string>)[engine],
        `missing description for ${engine}`,
      ).not.toBe("");
    }
  });
});

describe("database wizard engine preselect", () => {
  beforeEach(() => {
    // The picker passes the card engine straight through; unknown values
    // fall back to PostgreSQL through the shared engine catalogue.
  });

  it("resolves picker engine values through the catalogue", () => {
    expect(engineByValue("redis").defaultVersion).toBe("7.2-alpine");
    expect(engineByValue("nope").value).toBe("postgres");
  });
});
