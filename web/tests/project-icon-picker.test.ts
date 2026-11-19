/* global File: readonly */
import { NButton } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import ProjectIconPicker from "@/features/projects/components/ProjectIconPicker.vue";
import type { Project } from "@/features/projects/api/projects";
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
    setProjectIcon: vi.fn(),
    clearProjectIcon: vi.fn(),
    uploadProjectIcon: vi.fn(),
    getProjectIcon: vi.fn(),
  };
});
import {
  clearProjectIcon,
  setProjectIcon,
  uploadProjectIcon,
} from "@/features/projects/api/projects";

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    name: "storefront",
    description: "",
    icon: "",
    icon_version: 1,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    environment_count: 1,
    resource_counts: { applications: 0, services: 0, databases: 0 },
    ...overrides,
  };
}

function mountPicker(icon = "") {
  return mount(ProjectIconPicker, {
    props: { project: project({ icon }) },
    global: { plugins: [i18n] },
  });
}

beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  i18n.global.mergeLocaleMessage("vi", { projects: projectsVi });
  resetLocaleState();
  syncComposerLocale("en");
  vi.mocked(setProjectIcon).mockReset();
  vi.mocked(clearProjectIcon).mockReset();
  vi.mocked(uploadProjectIcon).mockReset();
});

afterEach(() => {
  setLocale("en", null);
});

describe("project icon picker", () => {
  it("marks the current preset and selects another one", async () => {
    const updated = project({ icon: "preset:box", icon_version: 2 });
    vi.mocked(setProjectIcon).mockResolvedValue(updated);
    const wrapper = mountPicker("preset:rocket");

    const presets = wrapper.findAll(".project-icon-picker__preset");
    expect(presets).toHaveLength(12);
    expect(presets[0].attributes("aria-pressed")).toBe("true");
    expect(presets[1].attributes("aria-pressed")).toBe("false");

    await presets[1].trigger("click");
    await flushPromises();

    expect(setProjectIcon).toHaveBeenCalledWith(updated.id, "preset:box");
    expect(wrapper.emitted("updated")?.[0]?.[0]).toEqual(updated);

    await wrapper.setProps({ project: updated });
    expect(
      wrapper.findAll(".project-icon-picker__preset")[1].attributes("aria-pressed"),
    ).toBe("true");
  });

  it("uploads a chosen image", async () => {
    const updated = project({ icon: "upload", icon_version: 3 });
    vi.mocked(uploadProjectIcon).mockResolvedValue(updated);
    const wrapper = mountPicker();
    const file = new File([new Uint8Array([137, 80, 78, 71])], "icon.png", {
      type: "image/png",
    });
    const input = wrapper.find('input[type="file"]');
    Object.defineProperty(input.element, "files", {
      value: [file],
      configurable: true,
    });

    await input.trigger("change");
    await flushPromises();

    expect(uploadProjectIcon).toHaveBeenCalledWith(updated.id, file);
    expect(wrapper.emitted("updated")?.[0]?.[0]).toEqual(updated);
  });

  it("removes the icon and disables remove when there is none", async () => {
    const cleared = project({ icon: "", icon_version: 4 });
    vi.mocked(clearProjectIcon).mockResolvedValue(cleared);
    const wrapper = mountPicker("preset:star");
    const remove = wrapper
      .findAllComponents(NButton)
      .find((button) => button.text() === "Remove icon");

    await remove!.trigger("click");
    await flushPromises();
    expect(clearProjectIcon).toHaveBeenCalledWith(cleared.id);
    expect(wrapper.emitted("updated")?.[0]?.[0]).toEqual(cleared);

    const empty = mountPicker();
    const disabled = empty
      .findAllComponents(NButton)
      .find((button) => button.text() === "Remove icon");
    expect(disabled!.attributes("disabled")).toBeDefined();
  });

  it("shows the server refusal inline", async () => {
    vi.mocked(setProjectIcon).mockRejectedValue({
      status: 400,
      message: "projects: icon must be a PNG, JPEG or GIF image",
      cause: null,
    });
    const wrapper = mountPicker();

    await wrapper.findAll(".project-icon-picker__preset")[0].trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("icon must be a PNG, JPEG or GIF image");
    expect(wrapper.emitted("updated")).toBeUndefined();
  });

  it("switches every label to Vietnamese", async () => {
    const wrapper = mountPicker();
    expect(wrapper.text()).toContain("Upload image");
    expect(wrapper.text()).toContain("Remove icon");

    setLocale("vi", null);
    await flushPromises();

    expect(wrapper.text()).toContain("Tải ảnh lên");
    expect(wrapper.text()).toContain("Xóa biểu tượng");
    expect(wrapper.text()).toContain("Biểu tượng dự án");
    expect(wrapper.text()).not.toContain("projects.icon.");
    const first = wrapper.findAll(".project-icon-picker__preset")[0];
    expect(first.attributes("aria-label")).toBe("Tên lửa");
  });
});
