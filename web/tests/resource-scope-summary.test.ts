// PE-8 (JUS-37): the Change panel must not render empty error alerts.
// options.projectsError / options.environmentsError are refs inside a plain
// object, so reading them without .value is always truthy and interpolates
// to nothing: every screen using ResourceScopeSummary showed two empty red
// alerts. These tests fail on the old code (2 alerts with no text) and
// pass on the fixed code.
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ResourceScopeSummary from "../src/features/projects/components/ResourceScopeSummary.vue";

const { listProjectsMock, getProjectMock } = vi.hoisted(() => ({
  listProjectsMock: vi.fn(),
  getProjectMock: vi.fn(),
}));

vi.mock("@/features/projects/api/projects", () => ({
  listProjects: listProjectsMock,
  getProject: getProjectMock,
  describeProjectError: (error: unknown) =>
    error instanceof Error ? error.message : "Request failed",
}));

beforeEach(() => {
  setActivePinia(createPinia());
  vi.restoreAllMocks();
  listProjectsMock.mockResolvedValue([]);
  getProjectMock.mockResolvedValue({ project: null, environments: [] });
});

function openChange() {
  // Empty scope shows the Change panel immediately.
  return mount(ResourceScopeSummary, {
    props: { projectId: "", environmentId: "" },
  });
}

describe("ResourceScopeSummary error alerts", () => {
  it("renders no alert when there are no errors", async () => {
    const wrapper = openChange();
    await flushPromises();
    expect(wrapper.findAll(".n-alert").length).toBe(0);
    wrapper.unmount();
  });

  it("renders exactly one alert with the projects error", async () => {
    listProjectsMock.mockRejectedValueOnce(new Error("projects are down"));
    const wrapper = openChange();
    await flushPromises();
    const alerts = wrapper.findAll(".n-alert");
    expect(alerts.length).toBe(1);
    expect(alerts[0]!.text()).toContain("projects are down");
    wrapper.unmount();
  });

  it("renders exactly one alert with the environments error", async () => {
    getProjectMock.mockRejectedValueOnce(new Error("environments are down"));
    const wrapper = mount(ResourceScopeSummary, {
      props: { projectId: "proj-1", environmentId: "" },
    });
    await flushPromises();
    const alerts = wrapper.findAll(".n-alert");
    expect(alerts.length).toBe(1);
    expect(alerts[0]!.text()).toContain("environments are down");
    wrapper.unmount();
  });
});
