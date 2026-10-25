import { describe, expect, it, vi } from "vitest";

/* global console: readonly */

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() }
}));

import { http } from "@/shared/api/http";
import {
  createEnvironment,
  createProject,
  deleteEnvironment,
  deleteProject,
  getProject,
  isNameTakenError,
  listProjects,
  renameEnvironment,
  renameProject,
} from "@/features/projects/api/projects";

const counts = { applications: 1, services: 0, databases: 0 };

function project(overrides = {}) {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    name: "storefront",
    description: "Online shop",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
    environment_count: 1,
    resource_counts: counts,
    ...overrides,
  };
}

function environment(overrides = {}) {
  return {
    id: "22222222-2222-4222-8222-222222222222",
    project_id: "11111111-1111-4111-8111-111111111111",
    name: "production",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    resource_counts: counts,
    ...overrides,
  };
}

describe("projects api paths, bodies and headers", () => {
  it("lists projects", async () => {
    vi.mocked(http.get).mockResolvedValue({ data: { projects: [project()] } });
    const projects = await listProjects();
    expect(http.get).toHaveBeenCalledWith("/projects");
    expect(projects).toHaveLength(1);
    expect(projects[0]!.name).toBe("storefront");
  });

  it("creates with name and description, returning project and environments", async () => {
    const payload = { project: project(), environments: [environment()] };
    vi.mocked(http.post).mockResolvedValue({ data: payload });
    const created = await createProject({
      name: "storefront",
      description: "Online shop",
    });
    expect(http.post).toHaveBeenCalledWith(
      "/projects",
      { name: "storefront", description: "Online shop" },
    );
    expect(created.project.name).toBe("storefront");
    expect(created.environments).toHaveLength(1);
  });

  it("reads and patches one project", async () => {
    vi.mocked(http.get).mockResolvedValue({
      data: { project: project(), environments: [environment()] },
    });
    const detail = await getProject(project().id);
    expect(http.get).toHaveBeenCalledWith(`/projects/${project().id}`);
    expect(detail.environments).toHaveLength(1);

    vi.mocked(http.patch).mockResolvedValue({
      data: { project: project({ name: "renamed" }) },
    });
    const renamed = await renameProject(project().id, {
      name: "renamed",
    });
    expect(http.patch).toHaveBeenCalledWith(
      `/projects/${project().id}`,
      { name: "renamed" },
    );
    expect(renamed.name).toBe("renamed");
  });

  it("deletes a project by id", async () => {
    vi.mocked(http.delete).mockResolvedValue({ data: {} });
    await deleteProject(project().id);
    expect(http.delete).toHaveBeenCalledWith(`/projects/${project().id}`);
  });

  it("writes environments under the nested routes", async () => {
    vi.mocked(http.post).mockResolvedValue({
      data: { environment: environment({ name: "staging" }) },
    });
    const created = await createEnvironment(project().id, {
      name: "staging",
    });
    expect(http.post).toHaveBeenCalledWith(
      `/projects/${project().id}/environments`,
      { name: "staging" },
    );
    expect(created.name).toBe("staging");

    vi.mocked(http.patch).mockResolvedValue({
      data: { environment: environment({ name: "stage" }) },
    });
    const renamed = await renameEnvironment(environment().id, {
      name: "stage",
    });
    expect(http.patch).toHaveBeenCalledWith(
      `/environments/${environment().id}`,
      { name: "stage" },
    );
    expect(renamed.name).toBe("stage");

    await deleteEnvironment(environment().id);
    expect(http.delete).toHaveBeenCalledWith(
      `/environments/${environment().id}`,
    );
  });
});

describe("mutation envelopes parse warn-only", () => {
  it("keeps newer server fields instead of breaking", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    try {
      vi.mocked(http.patch).mockResolvedValue({
        data: { project: { ...project(), future_field: "kept" } },
      });
      const renamed = await renameProject(project().id, {
        name: "renamed",
      });
      expect((renamed as unknown as Record<string, unknown>).future_field).toBe(
        "kept",
      );
      expect(warn).not.toHaveBeenCalled();
    } finally {
      warn.mockRestore();
    }
  });

  it("warns and returns raw on a malformed mutation answer", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    try {
      vi.mocked(http.post).mockResolvedValue({ data: { bogus: true } });
      const created = await createProject({ name: "x" });
      expect(warn).toHaveBeenCalled();
      expect(created).toEqual({ bogus: true });
    } finally {
      warn.mockRestore();
    }
  });
});

describe("isNameTakenError", () => {
  it("matches only the 409 duplicate-name refusal", () => {
    expect(
      isNameTakenError({
        status: 409,
        message: "projects: project name already exists",
        cause: null,
      }),
    ).toBe(true);
    expect(
      isNameTakenError({
        status: 409,
        message: "projects: project still has resources",
        cause: null,
      }),
    ).toBe(false);
    expect(
      isNameTakenError({ status: 400, message: "already exists", cause: null }),
    ).toBe(false);
    expect(isNameTakenError(new Error("already exists"))).toBe(false);
    expect(isNameTakenError(null)).toBe(false);
  });
});
