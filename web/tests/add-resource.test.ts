// GS-1 (JUS-57) checks: Add-resource brand marks, catalog parity, the
// database wizard engine preselect the picker relies on, and the picker
// page behavior (keyboard navigation, card opens wizard, scope names).
/* global document, HTMLElement: readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { NEmpty, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, h, nextTick, ref } from "vue";

import { useCreateDatabaseWizard } from "@/features/databases/composables/useCreateDatabaseWizard";
import { ENGINES } from "@/features/databases/utils/databaseEngines";
import CreateDatabaseWizard from "@/features/databases/components/CreateDatabaseWizard.vue";
import ResourcePicker from "@/features/add-resource/components/ResourcePicker.vue";
import addResourceEn from "@/features/add-resource/locales/en";
import addResourceVi from "@/features/add-resource/locales/vi";
import { brandFor, initialsOf, templateBrand } from "@/features/add-resource/utils/brands";
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

registerDiscoveredCatalogs();

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

/** shell mounts a harness under the Naive message provider the wizards need. */
function shell(child: object) {
  return mount({
    render: () => h(NMessageProvider, null, { default: () => h(child as never) }),
  });
}

describe("templateBrand", () => {
  it("uses the shared gallery mark on the brand hue", () => {
    expect(templateBrand("nextcloud", "Nextcloud")).toEqual({
      letters: "N",
      color: "#0082c9",
    });
    // Same mark as the template gallery renders for this template.
    expect(templateBrand("nextcloud", "Nextcloud").letters).toBe(cardMark("Nextcloud"));
  });

  it("falls back to the accent hue for unknown template slugs", () => {
    expect(templateBrand("future-tool", "Cool Tool")).toEqual({
      letters: "C",
      color: "#5865f2",
    });
  });
});

describe("brandFor", () => {
  it("resolves known engine and application keys to their brand mark", () => {
    expect(brandFor("postgres", "PostgreSQL")).toEqual({
      letters: "PG",
      color: "#336791",
    });
    expect(brandFor("application", "Application")).toEqual({
      letters: "</>",
      color: "#5865f2",
    });
  });

  it("falls back to initials on the accent hue for unknown keys", () => {
    expect(brandFor("some-future-engine", "Cool Tool")).toEqual({
      letters: "CT",
      color: "#5865f2",
    });
    expect(brandFor("", "")).toEqual({ letters: "?", color: "#5865f2" });
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

/** mountPage mounts the picker with stubbed catalog data and a project scope. */
async function mountPage(_query: Record<string, string>, templates: typeof TEMPLATES) {
  const templatesStore = useTemplatesStore();
  vi.spyOn(templatesStore, "fetchTemplates").mockResolvedValue(undefined);
  templatesStore.templates = templates as never;
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/", name: "home", component: { template: "<div />" } }],
  });
  await router.push("/");
  await router.isReady();
  // The embedded wizards call useMessage, so the picker mounts under the
  // Naive message provider exactly as the real App shell provides it.
  const Parent = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () =>
          h(ResourcePicker as never, { projectId: "proj-1", environmentId: "env-1" }),
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

describe("ResourcePicker", () => {
  it("passes the project and environment scope to the wizards", async () => {
    const wrapper = await mountPage({}, TEMPLATES);
    const wizard = wrapper.findComponent(CreateDatabaseWizard);
    expect(wizard.props("projectId")).toBe("proj-1");
    expect(wizard.props("environmentId")).toBe("env-1");
    wrapper.unmount();
  });

  it("moves focus inside a group with the arrow keys", async () => {
    const wrapper = await mountPage({}, TEMPLATES);
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
    const wrapper = await mountPage({}, TEMPLATES);
    await wrapper.find('button[data-engine="redis"]').trigger("click");
    const wizard = wrapper.findComponent(CreateDatabaseWizard);
    expect(wizard.props("show")).toBe(true);
    expect(wizard.props("engine")).toBe("redis");
    wrapper.unmount();
  });

  it("branches the empty copy on catalog-empty versus filter-no-match", async () => {
    const emptyCatalog = await mountPage({}, []);
    expect(emptyCatalog.findComponent(NEmpty).props("description")).toBe(
      "No templates in the catalog.",
    );
    emptyCatalog.unmount();

    const filtered = await mountPage({}, TEMPLATES);
    await filtered.find("input").setValue("zzz-no-match");
    await nextTick();
    expect(filtered.findComponent(NEmpty).props("description")).toBe(
      "No service matches this filter.",
    );
    filtered.unmount();
  });
});
