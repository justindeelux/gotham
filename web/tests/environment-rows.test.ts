// JUS-87 environment rows: the name cell links to the resource detail
// page with the link stretched over the row (no Open button, no JS row
// handler). Native table semantics stay intact: rows carry no role or
// tabindex, each row holds exactly one link, and the link target matches
// the row the composable built.
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ref } from "vue";

import projectsEn from "@/features/projects/locales/en";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";
import EnvironmentPage from "@/features/projects/pages/EnvironmentPage.vue";

const { rows } = vi.hoisted(() => ({
  rows: [
    {
      kind: "application",
      id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      name: "storefront",
      subtitle: "medusajs/medusa",
      serverName: "prod-01",
      statusText: "running",
      statusTag: "success",
      preview: false,
      to: {
        name: "application-detail",
        params: {
          projectId: "11111111-1111-4111-8111-111111111111",
          environmentId: "22222222-2222-4222-8222-222222222222",
          id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
        },
      },
    },
    {
      kind: "service",
      id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
      name: "plausible",
      subtitle: "Template service",
      serverName: "prod-01",
      statusText: "running",
      statusTag: "success",
      preview: false,
      to: {
        name: "service-detail",
        params: {
          projectId: "11111111-1111-4111-8111-111111111111",
          environmentId: "22222222-2222-4222-8222-222222222222",
          id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
        },
      },
    },
  ],
}));

// The page template reads straight through both composables: serve canned
// refs so the mount stays hermetic (no HTTP, no stores, no router).
vi.mock("@/features/projects/composables/useEnvironmentPage", () => ({
  useEnvironmentPage: () => ({
    projectId: ref("11111111-1111-4111-8111-111111111111"),
    environmentId: ref("22222222-2222-4222-8222-222222222222"),
    loading: ref(false),
    error: ref(null),
    notFound: ref(false),
    resources: ref({
      project: { id: "11111111-1111-4111-8111-111111111111", name: "storefront" },
      environment: { id: "22222222-2222-4222-8222-222222222222", name: "production" },
    }),
    siblings: ref([]),
    tab: ref("all"),
    showPreviews: ref(false),
    search: ref(""),
    addOpen: ref(false),
    importOpen: ref(false),
    rows: ref(rows),
    visibleRows: ref(rows),
    counts: ref({ all: 2, applications: 1, services: 1, databases: 0 }),
    canWrite: ref(true),
    reload: vi.fn(),
  }),
}));

vi.mock("@/features/projects/composables/useSharedVariables", () => ({
  useSharedVariables: () => ({
    draft: ref([]),
    loading: ref(false),
    loadError: ref(null),
    saving: ref(false),
    saveError: ref(null),
    saveDisabled: ref(true),
    storedSecrets: ref([]),
    problems: ref([]),
    retry: vi.fn(),
    save: vi.fn(),
  }),
}));

beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
});

/** mountRows renders the table with every child component stubbed. */
function mountRows() {
  return mount(EnvironmentPage, {
    global: {
      plugins: [i18n],
      stubs: {
        // Render a real anchor so the row keeps exactly one focusable
        // link; `to` stays assertable through the stubbed component.
        RouterLink: { name: "RouterLink", template: "<a><slot /></a>", props: ["to"] },
        ProjectBreadcrumb: true,
        SharedVariablesEditor: true,
        ResourcePicker: true,
        ImportComposeDialog: true,
        NSpin: true,
        NSpace: true,
        NEmpty: true,
        NAlert: true,
        NButton: true,
        NCard: true,
        NInput: true,
        NSelect: true,
        NSwitch: true,
        NTag: true,
        NText: true,
        NModal: true,
      },
    },
  });
}

describe("environment resource rows", () => {
  it("links each row to its resource through the stretched name link", () => {
    const wrapper = mountRows();
    const links = wrapper.findAll("tbody tr.resource-row a.resource-link");
    expect(links).toHaveLength(rows.length);
    links.forEach((link, index) => {
      const component = wrapper
        .findAllComponents({ name: "RouterLink" })
        .at(index);
      expect(component?.props("to")).toEqual(rows[index].to);
    });
    wrapper.unmount();
  });

  it("keeps native row semantics with no Open button", () => {
    const wrapper = mountRows();
    const tableRows = wrapper.findAll("tbody tr.resource-row");
    expect(tableRows).toHaveLength(rows.length);
    for (const row of tableRows) {
      expect(row.attributes("role")).toBeUndefined();
      expect(row.attributes("tabindex")).toBeUndefined();
      // The name link is the only interactive element in the row, so
      // nothing inside it can hijack navigation.
      expect(row.findAll("a")).toHaveLength(1);
      expect(row.findAll("button")).toHaveLength(0);
    }
    expect(wrapper.find(".resource-table").text()).not.toContain("Open");
    wrapper.unmount();
  });
});
