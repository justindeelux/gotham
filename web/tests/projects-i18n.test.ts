// I18N-3 (JUS-44) focused checks: projects catalog parity plus the live
// language-switch behavior the package promises — inline 409s stay inline
// and translate, other failures stay in alerts, counts/kinds/problems
// switch, and dirty drafts plus the selected scope survive the switch.
/* global HTMLInputElement: readonly */
import { NButton, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  describeProjectError,
  isNameTakenError,
  resourceSummary,
} from "@/features/projects/api/projects";
import { useNameConflict } from "@/features/projects/composables/useNameConflict";
import ProjectsPage from "@/features/projects/pages/ProjectsPage.vue";
import projectsEn from "@/features/projects/locales/en";
import projectsVi from "@/features/projects/locales/vi";
import {
  validateVariableDrafts,
  inheritedOriginLabel,
} from "@/features/projects/schemas/variables";
import { unusableServerHint } from "@/features/projects/utils/serverOptions";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/features/projects/api/projects", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/projects/api/projects")>();
  return { ...actual, listProjects: vi.fn(), createProject: vi.fn() };
});
vi.mock("@/features/teams/api/teams", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/teams/api/teams")>();
  return { ...actual, listTeams: vi.fn() };
});

import { listProjects } from "@/features/projects/api/projects";
import { listTeams } from "@/features/teams/api/teams";
import { useTeamsStore } from "@/features/teams";

beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  i18n.global.mergeLocaleMessage("vi", { projects: projectsVi });
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
});

afterEach(() => {
  setLocale("en", null);
});

function taken409(): unknown {
  return Object.assign(new Error("projects: project name already exists"), {
    status: 409,
    cause: null,
  });
}

describe("projects catalog parity", () => {
  it("ships equal en/vi leaf keys, params and compiler-clean syntax", () => {
    expect(checkCatalogParity(projectsEn, projectsVi)).toEqual([]);
  });
});

describe("name-taken routing across locales", () => {
  it("keeps the raw 409 classifier locale-independent and inline", () => {
    expect(isNameTakenError(taken409())).toBe(true);
    setLocale("vi", null);
    expect(isNameTakenError(taken409())).toBe(true);
    expect(
      isNameTakenError({ status: 409, message: "gone", cause: null }),
    ).toBe(false);
  });

  it("translates the inline conflict reactively without touching the raw error", () => {
    const conflict = useNameConflict();
    expect(conflict.take(taken409())).toBe(true);
    expect(conflict.feedback.value).toBe("This name is already taken.");
    expect(conflict.status.value).toBe("error");
    setLocale("vi", null);
    expect(conflict.feedback.value).toBe("Tên này đã được sử dụng.");
    setLocale("en", null);
    expect(conflict.feedback.value).toBe("This name is already taken.");
    conflict.clear();
    expect(conflict.feedback.value).toBeUndefined();
  });

  it("leaves non-conflicts for the dialog alert", () => {
    const conflict = useNameConflict();
    expect(conflict.take(new Error("boom"))).toBe(false);
    expect(conflict.feedback.value).toBeUndefined();
  });
});

describe("error summaries keep raw diagnostics", () => {
  it("passes actionable server text through in both locales", () => {
    const refusal = {
      status: 409,
      message: "projects: project still has resources (including previews)",
      cause: null,
    };
    // The contract prefix is stripped (as before); the actionable refusal
    // itself is identical in both locales.
    expect(describeProjectError(refusal)).toBe(
      "project still has resources (including previews)",
    );
    setLocale("vi", null);
    expect(describeProjectError(refusal)).toBe(
      "project still has resources (including previews)",
    );
  });

  it("translates curated fallbacks while English stays byte-identical", () => {
    expect(describeProjectError({ status: 401, message: "", cause: null })).toBe(
      "Your session expired. Please sign in again.",
    );
    expect(describeProjectError(null)).toBe(
      "Something went wrong. Please try again.",
    );
    setLocale("vi", null);
    expect(describeProjectError({ status: 401, message: "", cause: null })).toBe(
      "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    );
    expect(describeProjectError(null)).toBe(
      "Đã xảy ra lỗi. Vui lòng thử lại.",
    );
  });
});

describe("counts, kinds and variable copy switch", () => {
  it("renders resource counts in both locales", () => {
    const counts = { applications: 5, services: 1, databases: 0 };
    expect(resourceSummary(counts)).toBe("5 applications · 1 service · 0 databases");
    setLocale("vi", null);
    expect(resourceSummary(counts)).toBe("5 ứng dụng · 1 dịch vụ · 0 cơ sở dữ liệu");
  });

  it("translates variable problems with the offending key interpolated", () => {
    const drafts = [{ key: "FRESH", value: "", secret: true }];
    expect(validateVariableDrafts(drafts, new Set())).toEqual([
      'Secret "FRESH" needs a value.',
    ]);
    setLocale("vi", null);
    expect(validateVariableDrafts(drafts, new Set())).toEqual([
      'Khóa bí mật "FRESH" cần có giá trị.',
    ]);
  });

  it("translates origin labels and picker hints", () => {
    expect(inheritedOriginLabel("project")).toBe("from project");
    setLocale("vi", null);
    expect(inheritedOriginLabel("project")).toBe("từ dự án");
    expect(inheritedOriginLabel("environment")).toBe("từ môi trường");
  });

  it("keeps server names raw inside translated picker hints", () => {
    const servers = [
      { id: "s1", name: "dark-03", status: "offline" },
    ] as never;
    setLocale("vi", null);
    expect(unusableServerHint(servers)).toBe("dark-03 ngoại tuyến");
    setLocale("en", null);
    expect(unusableServerHint(servers)).toBe("dark-03 is offline");
  });
});

describe("drafts survive a language switch", () => {
  it("keeps the typed create draft while labels switch", async () => {
    const teams = useTeamsStore();
    const team = {
      id: "team-1",
      name: "Acme",
      is_personal: false,
      role: "owner",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-01T00:00:00Z",
    };
    vi.mocked(listTeams).mockResolvedValue([team] as never);
    teams.teams = [team] as never;
    teams.activeTeamId = "team-1";
    vi.mocked(listProjects).mockResolvedValue([]);

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/projects", name: "projects", component: { render: () => h("div") } },
        { path: "/projects/:projectId", name: "project-detail", component: { render: () => h("div") } },
      ],
    });
    router.push("/projects");
    await router.isReady();
    const shell = defineComponent({
      render() {
        return h(NMessageProvider, null, { default: () => h(ProjectsPage as never) });
      },
    });
    const wrapper = mount(shell, {
      global: { plugins: [router], stubs: { teleport: true } },
    });
    await nextTick();
    await flushPromises();
    await nextTick();

    const open = wrapper
      .findAllComponents(NButton)
      .find((button) => button.text() === "New project");
    expect(open).toBeDefined();
    await open!.trigger("click");
    await flushPromises();
    const input = wrapper.find(".n-modal .n-form-item input");
    await input.setValue("storefront");
    expect((input.element as HTMLInputElement).value).toBe("storefront");

    setLocale("vi", null);
    await nextTick();
    await flushPromises();

    // The draft survives; the dialog and page chrome translated.
    expect((wrapper.find(".n-modal .n-form-item input").element as HTMLInputElement).value).toBe(
      "storefront",
    );
    expect(wrapper.find(".n-modal").exists()).toBe(true);
    expect(wrapper.text()).toContain("Dự án mới");
    expect(wrapper.text()).toContain("Duy nhất trong nhóm");
    wrapper.unmount();
  });
});
