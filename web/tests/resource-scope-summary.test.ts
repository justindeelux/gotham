import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { afterEach } from "vitest";

import ResourceScopeSummary from "../src/features/projects/components/ResourceScopeSummary.vue";
import projectsEn from "../src/features/projects/locales/en";
import projectsVi from "../src/features/projects/locales/vi";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "../src/shared/i18n";

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
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  i18n.global.mergeLocaleMessage("vi", { projects: projectsVi });
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
  vi.restoreAllMocks();
  listProjectsMock.mockResolvedValue([]);
  getProjectMock.mockResolvedValue({ project: null, environments: [] });
});

afterEach(() => {
  setLocale("en", null);
});

function openChange() {
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
