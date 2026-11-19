import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { defineComponent } from "vue";

import type { Project } from "@/features/projects/api/projects";
import ProjectCard from "@/features/projects/components/ProjectCard.vue";
import projectsEn from "@/features/projects/locales/en";
import projectsVi from "@/features/projects/locales/vi";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

const RouterLinkStub = defineComponent({
  props: ["to"],
  template: `<a href="#" @click.prevent="() => undefined"><slot /></a>`,
});

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    name: "storefront",
    description: "",
    icon: "",
    icon_version: 0,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    environment_count: 2,
    resource_counts: { applications: 3, services: 1, databases: 2 },
    ...overrides,
  };
}

function mountCard(overrides: Partial<Project> = {}, showStats = true): VueWrapper {
  return mount(ProjectCard, {
    props: { project: project(overrides), showStats },
    global: {
      plugins: [i18n],
      stubs: { RouterLink: RouterLinkStub },
    },
  });
}

beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  i18n.global.mergeLocaleMessage("vi", { projects: projectsVi });
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  setLocale("en", null);
});

describe("project card", () => {
  it("keeps the whole card one link with the project name as its label", () => {
    const wrapper = mountCard();
    const link = wrapper.find(".project-card-link");
    expect(link.exists()).toBe(true);
    expect(link.attributes("aria-label")).toBe("Open project storefront");
    expect(wrapper.findAll("a")).toHaveLength(1);
    expect(link.text()).toContain("storefront");
    wrapper.unmount();
  });

  it("reserves the description block without a placeholder", () => {
    const withDescription = mountCard({
      description: "Online shop and its backing stores",
    });
    const withoutDescription = mountCard({ description: "" });
    expect(withDescription.find(".project-card__desc").text()).toBe(
      "Online shop and its backing stores",
    );
    expect(withoutDescription.find(".project-card__desc").exists()).toBe(true);
    expect(withoutDescription.find(".project-card__desc").text()).toBe("");
    withDescription.unmount();
    withoutDescription.unmount();
  });

  it("keeps the stats row in the layout but empty when hidden", async () => {
    const shown = mountCard();
    expect(shown.find(".project-card__stats").text()).toBe(
      "2 environments · 6 resources",
    );
    const hidden = mountCard({}, false);
    const stats = hidden.find(".project-card__stats");
    expect(stats.exists()).toBe(true);
    expect(stats.text()).toBe("");
    shown.unmount();
    hidden.unmount();
  });

  it("translates the stats row", async () => {
    const wrapper = mountCard({ environment_count: 1 });
    setLocale("vi", null);
    await flushPromises();
    expect(wrapper.find(".project-card__stats").text()).toBe(
      "1 môi trường · 6 tài nguyên",
    );
    wrapper.unmount();
  });
});
