import { existsSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import catalog from "@/features/templates/locales/catalog.vi";

const templatesDir = resolve(dirname(fileURLToPath(import.meta.url)), "../..", "templates");

const templateSlugs = readdirSync(templatesDir, { withFileTypes: true })
  .filter(
    (entry) =>
      entry.isDirectory() && existsSync(`${templatesDir}/${entry.name}/template.yaml`),
  )
  .map((entry) => entry.name)
  .sort();

describe("Vietnamese catalog", () => {
  it("reads the builtin template directories on disk", () => {
    expect(templateSlugs.length).toBeGreaterThan(0);
  });

  it("has exactly one entry per builtin template", () => {
    expect(Object.keys(catalog).sort()).toEqual(templateSlugs);
  });

  it("keeps every description short and non-empty", () => {
    const bad = Object.entries(catalog)
      .filter(([, text]) => text.trim() === "" || text.length > 160)
      .map(([slug]) => slug);
    expect(bad).toEqual([]);
  });
});
