// Store race tests for the projects surface (PE-4 fix round 1).
// A response that lost to a team or project switch must never write state:
// the first read resolves last and is dropped, the current read wins, and
// the spinner is released exactly once.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/projects/api/projects", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/projects/api/projects")>();
  return {
    ...actual,
    listProjects: vi.fn(),
    getProject: vi.fn(),
  };
});
vi.mock("@/features/teams/api/teams", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/teams/api/teams")>();
  return { ...actual, listTeams: vi.fn() };
});

import {
  getProject,
  listProjects,
} from "@/features/projects/api/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useTeamsStore } from "@/features/teams";

const counts = { applications: 0, services: 0, databases: 0 };

function project(id: string, name: string) {
  return {
    id,
    name,
    description: "",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    environment_count: 0,
    resource_counts: counts,
  };
}

/** deferred returns a promise with its resolve exposed. */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(listProjects).mockReset();
  vi.mocked(getProject).mockReset();
});

describe("projects store races", () => {
  it("drops a list read that lost to a team switch", async () => {
    const teams = useTeamsStore();
    const store = useProjectsStore();
    teams.activeTeamId = "team-a";

    const first = deferred<ReturnType<typeof listProjects>>();
    const second = deferred<ReturnType<typeof listProjects>>();
    vi.mocked(listProjects)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);

    const readA = store.fetchProjects();
    // The selection switches while the first read is in flight; the page
    // refetches for the new team.
    teams.activeTeamId = "team-b";
    const readB = store.fetchProjects();

    // The stale read resolves last: it must not overwrite team B's list.
    second.resolve([project("b-1", "bee")]);
    await readB;
    first.resolve([project("a-1", "aye")]);
    await readA;

    expect(store.projects.map((item) => item.name)).toEqual(["bee"]);
    expect(store.loading).toBe(false);
    expect(store.error).toBeNull();
  });

  it("drops a detail read that lost to a project switch", async () => {
    const teams = useTeamsStore();
    const store = useProjectsStore();
    teams.activeTeamId = "team-1";
    const idA = "11111111-1111-4111-8111-111111111111";
    const idB = "33333333-3333-4333-8333-333333333333";

    const first = deferred<ReturnType<typeof getProject>>();
    const second = deferred<ReturnType<typeof getProject>>();
    vi.mocked(getProject)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);

    const readA = store.fetchDetail(idA);
    const readB = store.fetchDetail(idB);

    second.resolve({ project: project(idB, "bee"), environments: [] });
    await readB;
    first.resolve({ project: project(idA, "aye"), environments: [] });
    await readA;

    expect(store.detail?.name).toBe("bee");
    expect(store.detailLoading).toBe(false);
  });
});
