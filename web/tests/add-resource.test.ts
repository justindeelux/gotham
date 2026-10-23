/* global document, Element, HTMLElement, HTMLInputElement: readonly */
import { DOMWrapper, flushPromises, mount } from "@vue/test-utils";
import { NEmpty, NMessageProvider, NRadio, NSelect } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { computed, defineComponent, h, nextTick, reactive, ref } from "vue";

import { useCreateDatabaseWizard } from "@/features/databases/composables/useCreateDatabaseWizard";
import { ENGINES } from "@/features/databases/utils/databaseEngines";
import CreateDatabaseWizard from "@/features/databases/components/CreateDatabaseWizard.vue";
import CreateAppWizard from "@/features/applications/components/CreateAppWizard.vue";
import WizardSourceStep from "@/features/applications/components/WizardSourceStep.vue";
import { provideCreateWizard, useCreateAppWizard } from "@/features/applications/composables/useCreateAppWizard";
import TemplateWizard from "@/features/templates/components/TemplateWizard.vue";
import ResourcePicker from "@/features/add-resource/components/ResourcePicker.vue";
import addResourceEn from "@/features/add-resource/locales/en";
import addResourceVi from "@/features/add-resource/locales/vi";
import { applicationBrand, brandFor, initialsOf, templateBrand } from "@/features/add-resource/utils/brands";
import { cardMark } from "@/features/templates/utils/templateMark";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import { useServersStore } from "@/features/servers/stores/servers";
import { useTemplatesStore } from "@/features/templates/stores/templates";
import { useGitHubAppStore } from "@/features/applications/stores/githubApp";
import { useProvidersStore } from "@/features/applications/stores/providers";

registerDiscoveredCatalogs();

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
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
  listProjectsMock.mockResolvedValue([]);
  getProjectMock.mockResolvedValue({ project: null, environments: [] });
});

function shell(child: object) {
  return mount({
    render: () => h(NMessageProvider, null, { default: () => h(child as never) }),
  });
}

describe("templateBrand", () => {
  it("uses the vendored brand glyph on the brand hue", () => {
    expect(templateBrand("nextcloud", "Nextcloud")).toEqual({
      logo: "nextcloud",
      letters: "N",
      color: "#0082c9",
    });
    expect(templateBrand("nextcloud", "Nextcloud").letters).toBe(cardMark("Nextcloud"));
    expect(templateBrand("uptime-kuma", "Uptime Kuma").logo).toBe("uptimekuma");
  });

  it("falls back to the accent hue for unknown template slugs", () => {
    expect(templateBrand("future-tool", "Cool Tool")).toEqual({
      logo: null,
      letters: "C",
      color: "#5865f2",
    });
  });
});

describe("brandFor", () => {
  it("resolves known engine and application keys to their brand mark", () => {
    expect(brandFor("postgres", "PostgreSQL")).toEqual({
      logo: "postgresql",
      letters: "PO",
      color: "#4169e1",
    });
    expect(brandFor("application", "Application")).toEqual({
      logo: null,
      letters: "AP",
      color: "#5865f2",
    });
  });

  it("falls back to initials on the accent hue for unknown keys", () => {
    expect(brandFor("some-future-engine", "Cool Tool")).toEqual({
      logo: null,
      letters: "CT",
      color: "#5865f2",
    });
    expect(brandFor("", "")).toEqual({ logo: null, letters: "?", color: "#5865f2" });
  });
});

describe("applicationBrand", () => {
  it("maps every source type to its approved glyph", () => {
    const logos = {
      git_public: "git",
      git_private: "git",
      github_app: "github",
      gitlab_app: "gitlab",
      dockerfile: "docker",
      image: "docker",
      compose: "docker",
    };
    for (const [source, logo] of Object.entries(logos)) {
      expect(applicationBrand(source, "git", "shop").logo, source).toBe(logo);
    }
  });

  it("derives a legacy row's identity from its provider", () => {
    expect(applicationBrand("", "github", "shop").logo).toBe("github");
    expect(applicationBrand("", "gitlab", "shop").logo).toBe("gitlab");
    expect(applicationBrand("", "", "shop").logo).toBe("git");
  });
});

describe("initialsOf", () => {
  it("derives one- or two-letter marks from display names", () => {
    expect(initialsOf("n8n")).toBe("N8");
    expect(initialsOf("Uptime Kuma")).toBe("UK");
    expect(initialsOf("")).toBe("?");
  });
});

describe("add-resource catalog", () => {
  it("keeps en/vi parity", () => {
    expect(checkCatalogParity(addResourceEn, addResourceVi)).toEqual([]);
  });

  it("describes every supported database engine with a nonempty string", () => {
    expect(ENGINES.length).toBeGreaterThan(0);
    const descriptions = addResourceEn.engines as unknown as Record<string, unknown>;
    for (const engine of ENGINES) {
      const description = descriptions[engine.value];
      expect(
        typeof description === "string" && description.length > 0,
        `missing description for ${engine.value}`,
      ).toBe(true);
    }
  });
});

describe("database wizard engine preselect", () => {
  it("starts on the passed engine and keeps it across reset", async () => {
    const show = ref(false);
    const engine = ref("redis");
    let form: { engine: string; version: string } | null = null;
    const Harness = defineComponent({
      setup() {
        const wizard = useCreateDatabaseWizard({
          show,
          engine,
          projectId: "proj-a",
          environmentId: "env-a",
          onCreated: () => undefined,
          onUpdateShow: () => undefined,
        });
        form = wizard.form;
        return () => h("div");
      },
    });
    const serversStore = useServersStore();
    vi.spyOn(serversStore, "fetchServers").mockResolvedValue(undefined);
    const wrapper = shell(Harness);
    expect(form!.engine).toBe("redis");
    expect(form!.version).toBe("7.2-alpine");

    show.value = true;
    await nextTick();
    await flushPromises();
    expect(form!.engine).toBe("redis");

    show.value = false;
    await nextTick();
    await flushPromises();
    expect(form!.engine).toBe("redis");
    expect(form!.version).toBe("7.2-alpine");
    wrapper.unmount();
  });

  it("falls back to postgres for an unknown engine", () => {
    const show = ref(false);
    let form: { engine: string } | null = null;
    const Harness = defineComponent({
      setup() {
        const wizard = useCreateDatabaseWizard({
          show,
          engine: "nope",
          projectId: "proj-a",
          environmentId: "env-a",
          onCreated: () => undefined,
          onUpdateShow: () => undefined,
        });
        form = wizard.form;
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    expect(form!.engine).toBe("postgres");
    wrapper.unmount();
  });
});

const TEMPLATES = [
  { slug: "wordpress", name: "WordPress", icon: "wordpress", description: "wp" },
  { slug: "nextcloud", name: "Nextcloud", icon: "nextcloud", description: "nc" },
];

async function mountPage(templates: typeof TEMPLATES, show = ref(true)) {
  const templatesStore = useTemplatesStore();
  vi.spyOn(templatesStore, "fetchTemplates").mockResolvedValue(undefined);
  templatesStore.templates = templates as never;
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "home", component: { template: "<div />" } },
      { path: "/git-sources", name: "git-sources", component: { template: "<div />" } },
    ],
  });
  await router.push("/");
  await router.isReady();
  const Parent = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () =>
          h(ResourcePicker as never, {
            projectId: "proj-1", environmentId: "env-1", show: show.value,
          }),
      }),
  });
  const wrapper = mount(Parent, {
    attachTo: globalThis.document.body,
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  await nextTick();
  return wrapper;
}

async function selectTab(wrapper: Awaited<ReturnType<typeof mountPage>>, name: string) {
  const tab = wrapper.findAll('[role="tab"]').find((item) => item.text() === name)!;
  await tab.trigger("click");
  await flushPromises();
}

describe("ResourcePicker", () => {
  it("resets the tab and service filter when the parent modal reopens", async () => {
    const show = ref(false);
    const wrapper = await mountPage(TEMPLATES, show);
    show.value = true;
    await nextTick();
    await selectTab(wrapper, "Service");
    await wrapper.find("input").setValue("WordPress");
    await selectTab(wrapper, "Database");

    show.value = false;
    await nextTick();
    show.value = true;
    await nextTick();

    expect(wrapper.find('[role="tab"][aria-selected="true"]').text()).toBe("Application");
    await selectTab(wrapper, "Service");
    expect((wrapper.find("input").element as HTMLInputElement).value).toBe("");
    expect(wrapper.findAll("button[data-template]")).toHaveLength(TEMPLATES.length);
    wrapper.unmount();
  });

  it("keeps the current tab and filter when Escape closes only a child wizard", async () => {
    const show = ref(true);
    const wrapper = await mountPage(TEMPLATES, show);
    await selectTab(wrapper, "Service");
    await wrapper.find("input").setValue("WordPress");
    await selectTab(wrapper, "Database");
    await wrapper.find('button[data-engine="postgres"]').trigger("click");
    await flushPromises();
    expect(wrapper.findComponent(CreateDatabaseWizard).props("show")).toBe(true);

    document.dispatchEvent(new globalThis.KeyboardEvent("keydown", {
      key: "Escape", code: "Escape", bubbles: true,
    }));
    await flushPromises();
    await nextTick();

    expect(wrapper.findComponent(CreateDatabaseWizard).props("show")).toBe(false);
    expect(show.value).toBe(true);
    expect(wrapper.find('[role="tab"][aria-selected="true"]').text()).toBe("Database");
    await selectTab(wrapper, "Service");
    expect((wrapper.find("input").element as HTMLInputElement).value).toBe("WordPress");
    wrapper.unmount();
  });

  it("switches resource tabs by keyboard and links the active panel", async () => {
    const wrapper = await mountPage(TEMPLATES);
    const tabs = wrapper.findAll('[role="tab"]');
    expect(tabs.map((tab) => tab.text())).toEqual(["Application", "Service", "Database"]);
    (tabs[0].element as HTMLElement).focus();
    for (const [key, index] of [["ArrowRight", 1], ["End", 2], ["Home", 0], ["ArrowLeft", 2]] as const) {
      await new DOMWrapper(document.activeElement!).trigger("keydown", { key });
      await nextTick();
      expect(document.activeElement).toBe(tabs[index].element);
      expect(tabs[index].attributes("aria-selected")).toBe("true");
      expect(wrapper.find('[role="tabpanel"]').attributes("aria-labelledby")).toBe(
        tabs[index].attributes("id"),
      );
    }
    wrapper.unmount();
  });

  it("opens a service template with scope and preserves its filter across tabs", async () => {
    const wrapper = await mountPage(TEMPLATES);
    expect(wrapper.find("button[data-template]").exists()).toBe(false);
    await selectTab(wrapper, "Service");
    await wrapper.find("input").setValue("WordPress");
    await selectTab(wrapper, "Database");
    await selectTab(wrapper, "Service");
    expect(wrapper.findAll("button[data-template]")).toHaveLength(1);
    await wrapper.find('button[data-template="wordpress"]').trigger("click");
    const wizard = wrapper.findComponent(TemplateWizard);
    expect(wizard.props()).toMatchObject({
      show: true, slug: "wordpress", projectId: "proj-1", environmentId: "env-1",
    });
    wrapper.unmount();
  });

  it("offers compose import from the Service tab", async () => {
    const wrapper = await mountPage(TEMPLATES);
    await selectTab(wrapper, "Service");
    await wrapper.find('a[href="#"]').trigger("click");
    expect(wrapper.findComponent(ResourcePicker).emitted("composeImport")).toEqual([[]]);
    wrapper.unmount();
  });

  it("passes the project and environment scope to the wizards", async () => {
    const wrapper = await mountPage(TEMPLATES);
    const wizard = wrapper.findComponent(CreateDatabaseWizard);
    expect(wizard.props("projectId")).toBe("proj-1");
    expect(wizard.props("environmentId")).toBe("env-1");
    wrapper.unmount();
  });

  it("moves focus inside a group with the arrow keys", async () => {
    const wrapper = await mountPage(TEMPLATES);
    await selectTab(wrapper, "Database");
    const cards = wrapper.findAll('button[data-engine]');
    expect(cards.length).toBe(ENGINES.length);
    const grids = wrapper.findAll(".res-grid");
    const databaseGrid = grids[grids.length - 1];
    (cards[0].element as HTMLElement).focus();
    await databaseGrid.trigger("keydown", { key: "ArrowRight" });
    expect(document.activeElement).toBe(cards[1].element);
    await databaseGrid.trigger("keydown", { key: "ArrowLeft" });
    expect(document.activeElement).toBe(cards[0].element);
    wrapper.unmount();
  });

  it("opens the database wizard with the card engine preselected", async () => {
    const wrapper = await mountPage(TEMPLATES);
    await selectTab(wrapper, "Database");
    await wrapper.find('button[data-engine="redis"]').trigger("click");
    const wizard = wrapper.findComponent(CreateDatabaseWizard);
    expect(wizard.props("show")).toBe(true);
    expect(wizard.props("engine")).toBe("redis");
    wrapper.unmount();
  });

  it("branches the empty copy on catalog-empty versus filter-no-match", async () => {
    const emptyCatalog = await mountPage([]);
    await selectTab(emptyCatalog, "Service");
    expect(emptyCatalog.findComponent(NEmpty).props("description")).toBe(
      "No templates in the catalog.",
    );
    emptyCatalog.unmount();

    const filtered = await mountPage(TEMPLATES);
    await selectTab(filtered, "Service");
    await filtered.find("input").setValue("zzz-no-match");
    await nextTick();
    expect(filtered.findComponent(NEmpty).props("description")).toBe(
      "No service matches this filter.",
    );
    filtered.unmount();
  });

  it("renders one card per application source type", async () => {
    const wrapper = await mountPage(TEMPLATES);
    const cards = wrapper.findAll("button[data-source]");
    expect(cards.map((card) => card.attributes("data-source"))).toEqual([
      "git_public",
      "git_private",
      "github_app",
      "gitlab_app",
      "dockerfile",
      "image",
      "compose",
    ]);
    expect(cards[0].text()).toContain("Public git repository");
    expect(cards[2].text()).toContain("GitHub (connected)");
    expect(cards[6].text()).toContain("Docker Compose");
    wrapper.unmount();
  });

  it("opens the app wizard with the picked source type preselected", async () => {
    const wrapper = await mountPage(TEMPLATES);
    await wrapper.find('button[data-source="dockerfile"]').trigger("click");
    const wizard = wrapper.findComponent(CreateAppWizard);
    expect(wizard.props("show")).toBe(true);
    expect(wizard.props("sourceType")).toBe("dockerfile");
    wrapper.unmount();
  });

  it("drops the implementation-speak copy, keeping headings and the filter", async () => {
    const wrapper = await mountPage(TEMPLATES);
    const copy = wrapper.text();
    for (const label of ["Application", "Service", "Database"]) {
      expect(copy).toContain(label);
    }
    for (const dropped of [
      "wizard · git repository",
      "source wizard",
      "one card per template",
      "one card per engine",
      "Tab moves between cards",
    ]) {
      expect(copy).not.toContain(dropped);
    }
    await selectTab(wrapper, "Service");
    expect(wrapper.find("input").exists()).toBe(true);
    wrapper.unmount();
  });
});

function mountSourceStep(locked: boolean) {
  const form = reactive({
    sourceType: locked ? "dockerfile" : "git_public",
    publicCloneUrl: "",
    dockerfileContent: "",
    buildArgs: [],
    branch: "",
    name: "",
  });
  const Harness = defineComponent({
    setup() {
      provideCreateWizard({
        form,
        sourceTypeLocked: computed(() => locked),
        lockedSourceLabel: computed(() => "Dockerfile"),
        closeWizard: () => undefined,
        sourceTypeOptions: computed(() => []),
        isPublicRepo: computed(() => !locked),
        isDockerfile: computed(() => locked),
        isImage: computed(() => false),
        isProviderFlow: computed(() => false),
        isPrivateRepo: computed(() => false),
        isCompose: computed(() => false),
        isComposePaste: computed(() => false),
        isGitHubAppFlow: computed(() => false),
        sourceError: computed(() => ""),
        branchOptions: computed(() => []),
      } as never);
      return () => h(WizardSourceStep);
    },
  });
  return mount(Harness, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n] },
  });
}

describe("app wizard source preselect", () => {
  it("starts on the preselected type with the selector locked", () => {
    const show = ref(false);
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(show, (() => undefined) as never, {
          projectId: "proj-a",
          environmentId: "env-a",
          sourceType: "dockerfile",
        });
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    expect(wiz!.form.sourceType).toBe("dockerfile");
    expect(wiz!.sourceTypeLocked.value).toBe(true);
    expect(wiz!.lockedSourceLabel.value).toBe("Dockerfile");
    wrapper.unmount();
  });

  it("keeps the free selector without a preselect", () => {
    const show = ref(false);
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(show, (() => undefined) as never, {
          projectId: "proj-a",
          environmentId: "env-a",
        });
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    expect(wiz!.form.sourceType).toBe("git_public");
    expect(wiz!.sourceTypeLocked.value).toBe(false);
    wrapper.unmount();
  });

  it("falls back to the free selector for an unknown preselect", () => {
    const show = ref(false);
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(show, (() => undefined) as never, {
          projectId: "proj-a",
          environmentId: "env-a",
          sourceType: "nope",
        });
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    expect(wiz!.form.sourceType).toBe("git_public");
    expect(wiz!.sourceTypeLocked.value).toBe(false);
    wrapper.unmount();
  });

  it("hides the source select behind a summary when preselected", () => {
    const wrapper = mountSourceStep(true);
    const step = wrapper.findComponent(WizardSourceStep);
    expect(step.text()).toContain("Source: Dockerfile");
    expect(step.findAllComponents(NSelect).length).toBe(0);
    wrapper.unmount();
  });

  it("keeps the source select without a preselect", () => {
    const wrapper = mountSourceStep(false);
    const step = wrapper.findComponent(WizardSourceStep);
    expect(step.text()).toContain("Source type");
    expect(step.findAllComponents(NSelect).length).toBeGreaterThan(0);
    wrapper.unmount();
  });
});

async function mountDatabaseWizard(engine: string) {
  const serversStore = useServersStore();
  vi.spyOn(serversStore, "fetchServers").mockResolvedValue(undefined);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/", name: "home", component: { template: "<div />" } }],
  });
  await router.push("/");
  await router.isReady();
  const Parent = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () =>
          h(CreateDatabaseWizard as never, {
            show: true,
            projectId: "proj-1",
            environmentId: "env-1",
            engine,
          }),
      }),
  });
  const wrapper = mount(Parent, {
    attachTo: globalThis.document.body,
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  await nextTick();
  return wrapper;
}

describe("database wizard engine preselect", () => {
  it("locks the picked engine", () => {
    const show = ref(false);
    let wiz: ReturnType<typeof useCreateDatabaseWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateDatabaseWizard({
          show,
          engine: "postgres",
          projectId: "proj-a",
          environmentId: "env-a",
          onCreated: () => undefined,
          onUpdateShow: () => undefined,
        });
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    expect(wiz!.engineLocked.value).toBe(true);
    wrapper.unmount();
  });

  it("keeps the engine radio group without a preselect", async () => {
    const wrapper = await mountDatabaseWizard("");
    expect(wrapper.findAllComponents(NRadio).length).toBe(ENGINES.length);
    wrapper.unmount();
  });

  it("shows no engine select for a picked engine", async () => {
    const wrapper = await mountDatabaseWizard("postgres");
    expect(wrapper.findAllComponents(NRadio).length).toBe(0);
    expect(globalThis.document.body.textContent ?? "").toContain("Engine: PostgreSQL");
    wrapper.unmount();
  });

  it("keeps the radio group for an unknown engine value", () => {
    const show = ref(false);
    let wiz: ReturnType<typeof useCreateDatabaseWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateDatabaseWizard({
          show,
          engine: "nope",
          projectId: "proj-a",
          environmentId: "env-a",
          onCreated: () => undefined,
          onUpdateShow: () => undefined,
        });
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    expect(wiz!.engineLocked.value).toBe(false);
    wrapper.unmount();
  });
});

describe("picker marks and wizard overlay", () => {
  it("gives every source an explicit mark distinct from the engines", () => {
    const sources = [
      "git_public",
      "git_private",
      "github_app",
      "gitlab_app",
      "dockerfile",
      "image",
      "compose",
    ].map((value) => brandFor(value, "").logo);
    expect(sources).toEqual([
      "git",
      "git",
      "github",
      "gitlab",
      "docker",
      "docker",
      "docker",
    ]);
    const engines = ENGINES.map((engine) => brandFor(engine.value, engine.label).logo);
    for (const logo of engines) {
      expect(sources, `${logo} must not double as a source mark`).not.toContain(logo);
    }
  });

  it("reports wizardOpen while a wizard covers the picker", async () => {
    const wrapper = await mountPage(TEMPLATES);
    const picker = wrapper.findComponent(ResourcePicker);
    await wrapper.find('button[data-source="dockerfile"]').trigger("click");
    expect(picker.emitted("wizardOpen")).toEqual([[true]]);
    await wrapper.findComponent(WizardSourceStep).find(".preselected button").trigger("click");
    await flushPromises();
    await nextTick();
    expect(picker.emitted("wizardOpen")).toEqual([[true], [false]]);
    wrapper.unmount();
  });
});

describe("preselect fix round 1", () => {
  function stubNetwork() {
    vi.spyOn(useGitHubAppStore(), "fetchApps").mockResolvedValue(undefined);
    vi.spyOn(useProvidersStore(), "fetchProviders").mockResolvedValue(undefined);
    vi.spyOn(useServersStore(), "fetchServers").mockResolvedValue(undefined);
  }

  it("Change in the app summary closes the wizard back to the picker", async () => {
    const wrapper = await mountPage(TEMPLATES);
    await wrapper.find('button[data-source="dockerfile"]').trigger("click");
    await flushPromises();
    const step = wrapper.findComponent(WizardSourceStep);
    expect(step.text()).toContain("Source: Dockerfile");
    expect(step.findAllComponents(NSelect).length).toBe(0);
    await step.find(".preselected button").trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.findComponent(CreateAppWizard).props("show")).toBe(false);
    expect(wrapper.find('button[data-source="dockerfile"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("Change in the database summary closes the wizard back to the picker", async () => {
    const wrapper = await mountPage(TEMPLATES);
    await selectTab(wrapper, "Database");
    await wrapper.find('button[data-engine="postgres"]').trigger("click");
    await flushPromises();
    await nextTick();
    const change = globalThis.document.body.querySelector(".preselected button");
    expect(change?.textContent ?? "").toContain("Change");
    expect(globalThis.document.body.textContent ?? "").toContain("Engine: PostgreSQL");
    await new DOMWrapper(change as Element).trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.findComponent(CreateDatabaseWizard).props("show")).toBe(false);
    expect(wrapper.find('button[data-engine="postgres"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("reopening the same GitHub card refetches the connections", async () => {
    stubNetwork();
    const fetchApps = vi.spyOn(useGitHubAppStore(), "fetchApps").mockResolvedValue(undefined);
    const wrapper = await mountPage(TEMPLATES);
    await wrapper.find('button[data-source="github_app"]').trigger("click");
    await flushPromises();
    await nextTick();
    const afterFirst = fetchApps.mock.calls.length;
    expect(afterFirst).toBeGreaterThan(0);
    await wrapper.findComponent(WizardSourceStep).find(".preselected button").trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.findComponent(CreateAppWizard).props("show")).toBe(false);
    await wrapper.find('button[data-source="github_app"]').trigger("click");
    await flushPromises();
    await nextTick();
    expect(fetchApps.mock.calls.length).toBe(afterFirst + 1);
    wrapper.unmount();
  });

  it.each(["github_app", "gitlab_app"])(
    "preselected %s still renders the empty-connections alert",
    async (source) => {
      stubNetwork();
      const wrapper = await mountPage(TEMPLATES);
      await wrapper.find(`button[data-source="${source}"]`).trigger("click");
      await flushPromises();
      await nextTick();
      expect(wrapper.findComponent(WizardSourceStep).text()).toContain(
        "No connections yet",
      );
      wrapper.unmount();
    },
  );
});
