// Page-instance state regression tests for the projects surface (PE-4,
// JUS-33). The search query, dialog drafts, open flags and submit guards
// must be created per page mount and dropped on unmount: a typed project
// name must never survive a route change and dialogs must not re-open.
// Viewers see the list but no create button. Teleport is stubbed so dialog
// content renders inline.
import { NButton, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { afterEach, beforeEach } from "vitest";

import projectsEn from "@/features/projects/locales/en";
import projectsVi from "@/features/projects/locales/vi";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/features/projects/api/projects", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/projects/api/projects")>();
  return {
    ...actual,
    listProjects: vi.fn(),
    createProject: vi.fn(),
    getProject: vi.fn(),
    renameProject: vi.fn(),
    deleteProject: vi.fn(),
    createEnvironment: vi.fn(),
    renameEnvironment: vi.fn(),
    deleteEnvironment: vi.fn(),
  };
});
vi.mock("@/features/teams/api/teams", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/teams/api/teams")>();
  return { ...actual, listTeams: vi.fn() };
});

import {
  createProject,
  getProject,
  listProjects,
} from "@/features/projects/api/projects";
import type {
  Environment,
  Project,
} from "@/features/projects/api/projects";
import ProjectDetailPage from "@/features/projects/pages/ProjectDetailPage.vue";
import ProjectsPage from "@/features/projects/pages/ProjectsPage.vue";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { listTeams } from "@/features/teams/api/teams";
import { useTeamsStore } from "@/features/teams";

const counts = { applications: 3, services: 1, databases: 2 };
const emptyCounts = { applications: 0, services: 0, databases: 0 };

// Mounted pages resolve copy through the projects catalog: merge it and run
// English by default so the pinned copy below keeps proving behavior.
beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  i18n.global.mergeLocaleMessage("vi", { projects: projectsVi });
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  setLocale("en", null);
});

function projectRow(overrides: Partial<Project> = {}): Project {
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

function environmentRow(overrides: Partial<Environment> = {}): Environment {
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

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

function testRouter(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/projects", name: "projects", component: { render: () => h("div") } },
      {
        path: "/projects/:projectId",
        name: "project-detail",
        component: { render: () => h("div") } },
      {
        path: "/projects/:projectId/environments/:environmentId",
        name: "environment-detail",
        component: { render: () => h("div") },
      },
    ],
  });
  router.push(path);
  return router;
}

/** seedTeams presets the teams store; role picks writer vs viewer. */
function seedTeams(role: "owner" | "read_only" = "owner"): void {
  const team = {
    id: "team-1",
    name: "Acme",
    is_personal: false,
    role,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
  // The pages call ensureTeams on mount, which refetches: the mock answers
  // the same team so the seeded role (and selection) survives the load.
  vi.mocked(listTeams).mockResolvedValue([team] as never);
  const teams = useTeamsStore();
  teams.teams = [team] as never;
  teams.activeTeamId = "team-1";
}

/** mountList mounts the list page with a router and a seeded team. */
async function mountList(path = "/projects") {
  const router = testRouter(path);
  await router.isReady();
  const wrapper = mount(shell(ProjectsPage), {
    global: { plugins: [router], stubs: { teleport: true } },
  });
  await nextTick();
  await flushPromises();
  await nextTick();
  return wrapper;
}

/** mountDetail mounts the detail page for one project id. */
async function mountDetail(id: string) {
  const router = testRouter(`/projects/${id}`);
  await router.isReady();
  const wrapper = mount(shell(ProjectDetailPage), {
    global: { plugins: [router], stubs: { teleport: true } },
  });
  await nextTick();
  await flushPromises();
  await nextTick();
  return { wrapper, router };
}

/** clickButton clicks the first naive button with the exact label. */
async function clickButton(wrapper: VueWrapper, label: string): Promise<void> {
  const buttons = wrapper.findAllComponents(NButton);
  const target = buttons.find((button) => button.text() === label);
  expect(target, `expected a button labelled ${label}`).toBeDefined();
  await target!.trigger("click");
  await flushPromises();
  await nextTick();
}

/** buttonLabels lists every rendered naive button label. */
function buttonLabels(wrapper: VueWrapper): string[] {
  return wrapper.findAllComponents(NButton).map((button) => button.text());
}

describe("projects list page states", () => {
  it("renders cards, then drops the search query on remount", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listProjects).mockResolvedValue([projectRow()]);

    let wrapper = await mountList();
    expect(wrapper.text()).toContain("storefront");
    expect(wrapper.text()).toContain("3 applications");

    const search = wrapper.find('.toolbar .n-input input');
    await search.setValue("storefront");
    await nextTick();
    expect(wrapper.text()).toContain("storefront");

    wrapper.unmount();
    wrapper = await mountList();
    const fresh = wrapper.find('.toolbar .n-input input');
    expect((fresh.element as unknown as { value: string }).value).toBe("");
    expect(wrapper.text()).toContain("storefront");
    wrapper.unmount();
  });

  it("filters by name and shows the no-match state", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listProjects).mockResolvedValue([
      projectRow(),
      projectRow({
        id: "33333333-3333-4333-8333-333333333333",
        name: "internal-tools",
        description: "Docs site",
      }),
    ]);

    const wrapper = await mountList();
    expect(wrapper.text()).toContain("internal-tools");
    await wrapper.find('.toolbar .n-input input').setValue("store");
    await nextTick();
    expect(wrapper.text()).toContain("storefront");
    expect(wrapper.text()).not.toContain("internal-tools");
    await wrapper.find('.toolbar .n-input input').setValue("nope");
    await nextTick();
    expect(wrapper.text()).toContain("No project matches this search.");
    wrapper.unmount();
  });

  it("shows the empty state with a create button", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listProjects).mockResolvedValue([]);

    const wrapper = await mountList();
    expect(wrapper.text()).toContain("Create your first project");
    expect(buttonLabels(wrapper)).toContain("New project");
    wrapper.unmount();
  });

  it("shows the load error with a retry", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listProjects).mockRejectedValue(
      Object.assign(new Error("boom"), { status: 500, cause: null }),
    );

    const wrapper = await mountList();
    expect(wrapper.text()).toContain("boom");
    expect(buttonLabels(wrapper)).toContain("Retry");
    wrapper.unmount();
  });

  it("hides every create button from viewers", async () => {
    setActivePinia(createPinia());
    seedTeams("read_only");
    vi.mocked(listProjects).mockResolvedValue([projectRow()]);

    const wrapper = await mountList();
    expect(wrapper.text()).toContain("storefront");
    expect(buttonLabels(wrapper)).not.toContain("New project");
    wrapper.unmount();
  });

  it("creates a project only with a valid name and a free guard", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listProjects).mockResolvedValue([]);
    vi.mocked(createProject).mockResolvedValue({
      project: projectRow(),
      environments: [],
    });

    const wrapper = await mountList();
    await clickButton(wrapper, "New project");
    expect(wrapper.text()).toContain("production");
    // Empty name: the submit stays disabled, no request fires.
    const submits = wrapper
      .findAllComponents(NButton)
      .filter((button) => button.text() === "Create project");
    expect(submits.length).toBeGreaterThan(0);
    expect(submits[0]!.attributes("disabled")).toBeDefined();
    expect(vi.mocked(createProject)).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("names every create input programmatically", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listProjects).mockResolvedValue([]);

    const wrapper = await mountList();
    await clickButton(wrapper, "New project");
    // Visible labels are not linked by Naive UI, so each input carries its
    // own accessible name matching the visible label.
    expect(wrapper.find("#project-create-name").attributes("aria-label")).toBe(
      "Project name",
    );
    expect(
      wrapper.find("#project-create-description").attributes("aria-label"),
    ).toBe("Project description");
    wrapper.unmount();
  });

  it("renders a taken name inline on the field, not the dialog alert", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listProjects).mockResolvedValue([]);
    vi.mocked(createProject).mockRejectedValue(
      Object.assign(new Error("project name already exists"), {
        status: 409,
        cause: null,
      }),
    );

    const wrapper = await mountList();
    await clickButton(wrapper, "New project");
    await wrapper.find(".n-modal .n-form-item input").setValue("storefront");
    await clickButton(wrapper, "Create project");
    // The 409 renders on the name field and the dialog stays open. The raw
    // refusal stays in the conflict state; the field shows the curated
    // message in the active locale.
    const feedback = wrapper.find(".n-modal .n-form-item-feedback__line");
    expect(feedback.exists()).toBe(true);
    expect(feedback.text()).toContain("This name is already taken.");
    expect(wrapper.text()).toContain("New project");
    // Editing the field clears the inline conflict.
    await wrapper.find(".n-modal .n-form-item input").setValue("storefront-2");
    await nextTick();
    expect(
      wrapper.find(".n-modal .n-form-item-feedback__line").exists(),
    ).toBe(false);
    wrapper.unmount();
  });
});

describe("project detail page states", () => {
  const id = "11111111-1111-4111-8111-111111111111";

  it("renders the breadcrumb, environments and the variables placeholder", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow()],
    });

    const { wrapper } = await mountDetail(id);
    expect(wrapper.text()).toContain("storefront");
    expect(wrapper.text()).toContain("Projects");
    expect(wrapper.text()).toContain("production");
    expect(wrapper.text()).toContain("Shared variables");
    // PE-5: Open links to the environment page.
    const open = wrapper
      .findAllComponents(NButton)
      .find((button) => button.text() === "Open");
    expect(open?.attributes("disabled")).toBeUndefined();
    const link = wrapper.find(
      'a[href="/projects/11111111-1111-4111-8111-111111111111/environments/22222222-2222-4222-8222-222222222222"]',
    );
    expect(link.exists()).toBe(true);
    wrapper.unmount();
  });

  it("explains the blocked project delete in an openable dialog", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow()],
    });

    const { wrapper } = await mountDetail(id);
    // The opener stays enabled so keyboard and touch reach the explanation.
    await clickButton(wrapper, "Delete project");
    expect(wrapper.text()).toContain("Move or delete every resource");
    const confirm = wrapper
      .find(".n-modal")
      .findAllComponents(NButton)
      .find((button) => button.text() === "Delete project");
    expect(confirm, "expected a dialog confirm").toBeDefined();
    expect(confirm!.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("explains the blocked environment delete in an openable dialog", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow()],
    });

    const { wrapper } = await mountDetail(id);
    // Row Delete stays enabled; the dialog carries the visible block reason.
    await clickButton(wrapper, "Delete");
    expect(wrapper.text()).toContain("Move or delete them first");
    wrapper.unmount();
  });

  it("names every detail dialog input programmatically", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow(emptyCounts)],
    });

    const { wrapper } = await mountDetail(id);
    const renames = () =>
      wrapper
        .findAllComponents(NButton)
        .filter((button) => button.text() === "Rename");

    // Project rename (first Rename button, in the page head).
    await renames()[0]!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.find("#project-rename-name").attributes("aria-label")).toBe(
      "Project name",
    );
    await clickButton(wrapper, "Cancel");

    // Environment create.
    await clickButton(wrapper, "Add environment");
    expect(
      wrapper.find("#environment-create-name").attributes("aria-label"),
    ).toBe("Environment name");
    await clickButton(wrapper, "Cancel");

    // Environment rename (second Rename button, in the table row).
    await renames()[1]!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(
      wrapper.find("#environment-rename-name").attributes("aria-label"),
    ).toBe("Environment name");
    wrapper.unmount();
  });

  it("submits renames on keydown Enter, never on keyup", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow(emptyCounts)],
    });
    const { renameProject } = await import("@/features/projects/api/projects");
    vi.mocked(renameProject).mockResolvedValue(projectRow({ name: "renamed" }));

    const { wrapper } = await mountDetail(id);
    await clickButton(wrapper, "Rename");
    const input = wrapper.find(".n-modal input");
    expect(input.exists()).toBe(true);
    // The keystroke that opened the dialog ends here: keyup must not submit.
    await input.trigger("keyup.enter");
    await flushPromises();
    expect(vi.mocked(renameProject)).not.toHaveBeenCalled();
    // An explicit Enter inside the form submits once.
    await input.trigger("keydown.enter");
    await flushPromises();
    await nextTick();
    expect(vi.mocked(renameProject)).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("shows the detail error with a retry", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(getProject).mockRejectedValue(
      Object.assign(new Error("gone"), { status: 404, cause: null }),
    );

    const { wrapper } = await mountDetail(id);
    expect(wrapper.text()).toContain("gone");
    expect(buttonLabels(wrapper)).toContain("Retry");
    wrapper.unmount();
  });

  it("hides rename and delete from viewers", async () => {
    setActivePinia(createPinia());
    seedTeams("read_only");
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow(emptyCounts)],
    });

    const { wrapper } = await mountDetail(id);
    expect(wrapper.text()).toContain("storefront");
    expect(buttonLabels(wrapper)).not.toContain("Rename");
    expect(buttonLabels(wrapper)).not.toContain("Delete project");
    expect(buttonLabels(wrapper)).not.toContain("Add environment");
    wrapper.unmount();
  });

  it("drops dialog drafts on remount", async () => {
    setActivePinia(createPinia());
    seedTeams();
    const store = useProjectsStore();
    store.detail = projectRow();
    store.environments = [environmentRow(emptyCounts)];
    store.loaded = true;
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow(emptyCounts)],
    });

    let { wrapper } = await mountDetail(id);
    await clickButton(wrapper, "Add environment");
    const name = wrapper.find('.n-modal input');
    await name.setValue("staging");
    wrapper.unmount();

    wrapper = (await mountDetail(id)).wrapper;
    await clickButton(wrapper, "Add environment");
    const fresh = wrapper.find('.n-modal input');
    expect((fresh.element as unknown as { value: string }).value).toBe("");
    wrapper.unmount();
  });

  it("submits environment dialogs on keydown Enter, never on keyup", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(getProject).mockResolvedValue({
      project: projectRow(),
      environments: [environmentRow(emptyCounts)],
    });
    const { createEnvironment, renameEnvironment } = await import(
      "@/features/projects/api/projects"
    );
    vi.mocked(createEnvironment).mockResolvedValue(environmentRow());
    vi.mocked(renameEnvironment).mockResolvedValue(environmentRow());

    const { wrapper } = await mountDetail(id);
    // Environment create: keyup submits nothing, keydown submits once.
    await clickButton(wrapper, "Add environment");
    const createInput = wrapper.find(".n-modal input");
    await createInput.setValue("staging");
    await createInput.trigger("keyup.enter");
    await flushPromises();
    expect(vi.mocked(createEnvironment)).not.toHaveBeenCalled();
    await createInput.trigger("keydown.enter");
    await flushPromises();
    await nextTick();
    expect(vi.mocked(createEnvironment)).toHaveBeenCalledTimes(1);

    // Environment rename: keyup submits nothing, keydown submits once.
    const rowRename = wrapper
      .findAllComponents(NButton)
      .filter((button) => button.text() === "Rename")[1]!;
    await rowRename.trigger("click");
    await flushPromises();
    await nextTick();
    const renameInput = wrapper.find(".n-modal input");
    await renameInput.trigger("keyup.enter");
    await flushPromises();
    expect(vi.mocked(renameEnvironment)).not.toHaveBeenCalled();
    await renameInput.trigger("keydown.enter");
    await flushPromises();
    await nextTick();
    expect(vi.mocked(renameEnvironment)).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("keeps the busy guard across a route change mid-request", async () => {
    setActivePinia(createPinia());
    seedTeams();
    const otherId = "33333333-3333-4333-8333-333333333333";
    vi.mocked(getProject).mockImplementation(async (_teamId: string, pid: string) => ({
      project: projectRow({ id: pid, name: pid === id ? "storefront" : "other" }),
      environments: [],
    }));
    const { renameProject } = await import("@/features/projects/api/projects");
    let resolveRename!: (_value: Project) => void;
    vi.mocked(renameProject).mockReturnValue(
      new Promise<Project>((resolvePromise) => {
        resolveRename = resolvePromise;
      }),
    );

    const { wrapper, router } = await mountDetail(id);
    await clickButton(wrapper, "Rename");
    const input = wrapper.find(".n-modal input");
    await input.setValue("renamed");
    // First submit starts the request.
    await input.trigger("keydown.enter");
    await flushPromises();
    expect(vi.mocked(renameProject)).toHaveBeenCalledTimes(1);
    // Moving to another project resets dialogs but must not release the
    // in-flight guard: submitting the new project's dialog stays blocked.
    await router.push(`/projects/${otherId}`);
    await flushPromises();
    await nextTick();
    await clickButton(wrapper, "Rename");
    const otherInput = wrapper.find(".n-modal input");
    await otherInput.setValue("renamed-b");
    await otherInput.trigger("keydown.enter");
    await flushPromises();
    expect(vi.mocked(renameProject)).toHaveBeenCalledTimes(1);
    resolveRename(projectRow({ id, name: "renamed" }));
    await flushPromises();
    await nextTick();
    expect(vi.mocked(renameProject)).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("reloads when the route moves to another project", async () => {    setActivePinia(createPinia());
    seedTeams();
    const otherId = "33333333-3333-4333-8333-333333333333";
    vi.mocked(getProject).mockImplementation(async (_teamId: string, pid: string) => ({
      project: projectRow({ id: pid, name: pid === id ? "storefront" : "other" }),
      environments: [],
    }));

    const { wrapper, router } = await mountDetail(id);
    expect(wrapper.text()).toContain("storefront");
    await router.push(`/projects/${otherId}`);
    await flushPromises();
    await nextTick();
    expect(wrapper.text()).toContain("other");
    expect(vi.mocked(getProject)).toHaveBeenCalledWith("team-1", otherId);
    wrapper.unmount();
  });

  it("late rename success does not close another project's dialog", async () => {
    setActivePinia(createPinia());
    seedTeams();
    const otherId = "33333333-3333-4333-8333-333333333333";
    vi.mocked(getProject).mockImplementation(async (_teamId: string, pid: string) => ({
      project: projectRow({ id: pid, name: pid === id ? "storefront" : "other" }),
      environments: [],
    }));
    const { renameProject } = await import("@/features/projects/api/projects");
    let resolveRename!: (_value: Project) => void;
    vi.mocked(renameProject).mockReturnValue(
      new Promise<Project>((resolvePromise) => {
        resolveRename = resolvePromise;
      }),
    );

    const { wrapper, router } = await mountDetail(id);
    await clickButton(wrapper, "Rename");
    await wrapper.find(".n-modal input").setValue("renamed");
    await wrapper.find(".n-modal input").trigger("keydown.enter");
    await flushPromises();
    expect(vi.mocked(renameProject)).toHaveBeenCalledTimes(1);
    // Move to B and open its rename dialog before A resolves.
    await router.push(`/projects/${otherId}`);
    await flushPromises();
    await nextTick();
    await clickButton(wrapper, "Rename");
    resolveRename(projectRow({ id, name: "renamed" }));
    await flushPromises();
    await nextTick();
    // B's dialog stays open with B's draft, not A's result.
    expect(wrapper.find(".n-modal").exists()).toBe(true);
    expect(
      (wrapper.find(".n-modal input").element as unknown as { value: string })
        .value,
    ).toBe("other");
    wrapper.unmount();
  });

  it("late delete success does not navigate away from another project", async () => {
    setActivePinia(createPinia());
    seedTeams();
    const otherId = "33333333-3333-4333-8333-333333333333";
    vi.mocked(getProject).mockImplementation(async (_teamId: string, pid: string) => ({
      project: projectRow({ id: pid, name: pid === id ? "storefront" : "other" }),
      environments: [],
    }));
    const { deleteProject } = await import("@/features/projects/api/projects");
    let resolveDelete!: (_value: void) => void;
    vi.mocked(deleteProject).mockReturnValue(
      new Promise<void>((resolvePromise) => {
        resolveDelete = resolvePromise;
      }),
    );

    const { wrapper, router } = await mountDetail(id);
    await clickButton(wrapper, "Delete project");
    const confirm = wrapper
      .find(".n-modal")
      .findAllComponents(NButton)
      .find((button) => button.text() === "Delete project")!;
    await confirm.trigger("click");
    await flushPromises();
    expect(vi.mocked(deleteProject)).toHaveBeenCalledTimes(1);
    // Move to B before A resolves: the success must not leave B.
    await router.push(`/projects/${otherId}`);
    await flushPromises();
    await nextTick();
    resolveDelete();
    await flushPromises();
    await nextTick();
    expect(router.currentRoute.value.params.projectId).toBe(otherId);
    expect(wrapper.text()).toContain("other");
    wrapper.unmount();
  });
});
