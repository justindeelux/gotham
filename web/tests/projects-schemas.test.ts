// Schemas and pure helpers for the projects surface (PE-4, JUS-33).
// Pins the contract's validation (names 1-64 chars after trim), the list
// search, the card summary text and the error mapping.
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  describeProjectError,
  resourceSummary,
} from "@/features/projects/api/projects";
import {
  createdProjectEnvelopeSchema,
  environmentNameSchema,
  filterProjects,
  isEnvironmentNameValid,
  isProjectDescriptionValid,
  isProjectNameValid,
  parseProjectDetail,
  parseProjectList,
  projectDetailEnvelopeSchema,
  projectListEnvelopeSchema,
  projectNameSchema,
} from "@/features/projects/schemas/projects";
import projectsEn from "@/features/projects/locales/en";
import projectsVi from "@/features/projects/locales/vi";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

// Display helpers resolve through the projects catalog: merge it once and
// run English by default so the pinned copy below keeps proving behavior.
beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  i18n.global.mergeLocaleMessage("vi", { projects: projectsVi });
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  setLocale("en", null);
});

const counts = { applications: 5, services: 1, databases: 2 };

function project(overrides = {}) {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    name: "storefront",
    description: "Online shop",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
    environment_count: 2,
    resource_counts: counts,
    ...overrides,
  };
}

describe("project name gating matches the 1-64 contract rule", () => {
  it("rejects empty and overlong names", () => {
    for (const value of ["", "   ", undefined, null, 42]) {
      expect(isProjectNameValid(value)).toBe(false);
    }
    expect(isProjectNameValid("x".repeat(65))).toBe(false);
  });

  it("accepts trimmed names up to 64 chars", () => {
    for (const value of ["a", "storefront", "  Core  ", "x".repeat(64)]) {
      expect(isProjectNameValid(value)).toBe(true);
    }
  });

  it("ruleFrom sees the same boundary through the schema", () => {
    expect(projectNameSchema.safeParse("x".repeat(64)).success).toBe(true);
    expect(projectNameSchema.safeParse("x".repeat(65)).success).toBe(false);
  });
});

describe("environment name gating", () => {
  it("rejects empty and overlong names", () => {
    for (const value of ["", "   ", undefined, null]) {
      expect(isEnvironmentNameValid(value)).toBe(false);
    }
    expect(isEnvironmentNameValid("x".repeat(65))).toBe(false);
  });

  it("accepts production and staging style names", () => {
    expect(environmentNameSchema.safeParse("production").success).toBe(true);
    expect(environmentNameSchema.safeParse("staging").success).toBe(true);
  });
});

describe("project description gating", () => {
  it("allows empty and caps at 500 chars", () => {
    expect(isProjectDescriptionValid("")).toBe(true);
    expect(isProjectDescriptionValid("Online shop")).toBe(true);
    expect(isProjectDescriptionValid("x".repeat(500))).toBe(true);
    expect(isProjectDescriptionValid("x".repeat(501))).toBe(false);
    expect(isProjectDescriptionValid(undefined)).toBe(false);
  });
});

describe("filterProjects", () => {
  const rows = [
    project({ name: "storefront", description: "Online shop" }),
    project({ name: "internal-tools", description: "Docs site" }),
  ];

  it("matches name and description case-insensitively", () => {
    expect(filterProjects(rows, "store")).toHaveLength(1);
    expect(filterProjects(rows, "DOCS")).toHaveLength(1);
    expect(filterProjects(rows, "")).toHaveLength(2);
    expect(filterProjects(rows, "   ")).toHaveLength(2);
    expect(filterProjects(rows, "nope")).toHaveLength(0);
  });
});

describe("resourceSummary", () => {
  it("renders the mockup card line with singular forms", () => {
    expect(resourceSummary(counts)).toBe("5 applications · 1 service · 2 databases");
    expect(
      resourceSummary({ applications: 1, services: 1, databases: 1 }),
    ).toBe("1 application · 1 service · 1 database");
    expect(
      resourceSummary({ applications: 0, services: 0, databases: 0 }),
    ).toBe("0 applications · 0 services · 0 databases");
  });
});

describe("response envelopes match the contract", () => {
  it("accepts a list payload", () => {
    const parsed = parseProjectList({ projects: [project()] });
    expect(parsed.projects).toHaveLength(1);
    expect(projectListEnvelopeSchema.safeParse({ projects: [] }).success).toBe(true);
  });

  it("rejects a project without the contract fields", () => {
    expect(
      projectListEnvelopeSchema.safeParse({ projects: [{ id: "x" }] }).success,
    ).toBe(false);
  });

  it("accepts a detail payload with environments", () => {
    const environment = {
      id: "22222222-2222-4222-8222-222222222222",
      project_id: "11111111-1111-4111-8111-111111111111",
      name: "production",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-01T00:00:00Z",
      resource_counts: counts,
    };
    const parsed = parseProjectDetail({
      project: project(),
      environments: [environment],
    });
    expect(parsed.environments).toHaveLength(1);
    expect(
      projectDetailEnvelopeSchema.safeParse({
        project: project(),
        environments: [],
      }).success,
    ).toBe(true);
  });

  it("accepts the create answer with its production environment", () => {
    expect(
      createdProjectEnvelopeSchema.safeParse({
        project: project(),
        environments: [],
      }).success,
    ).toBe(true);
  });
});

describe("describeProjectError", () => {
  it("maps the contract statuses to actionable text", () => {
    expect(
      describeProjectError({ status: 409, message: "project still has resources", cause: null }),
    ).toBe("project still has resources");
    expect(
      describeProjectError({ status: 409, message: "", cause: null }),
    ).toContain("changed while you were editing");
    expect(
      describeProjectError({ status: 403, message: "forbidden", cause: null }),
    ).toBe("forbidden");
    expect(
      describeProjectError({ status: 404, message: "", cause: null }),
    ).toContain("removed already");
    expect(
      describeProjectError({ status: 401, message: "x", cause: null }),
    ).toContain("sign in again");
    expect(describeProjectError(new Error("boom"))).toBe(
      "Something went wrong. Please try again: boom",
    );
    expect(describeProjectError(null)).toContain("went wrong");
  });
});
