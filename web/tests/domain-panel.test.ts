// JUS-233 Domains tab behavior: compose service pre-fill, edit payloads,
// protocol selection, clearing the service select (emits null), many domains
// and the non-compose layout.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, nextTick, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/applications/api/applications")>();
  return {
    ...actual,
    listDomains: vi.fn(),
    addDomain: vi.fn(),
    updateDomain: vi.fn(),
    removeDomain: vi.fn(),
    setPrimaryDomain: vi.fn(),
    getApplication: vi.fn(),
  };
});

vi.mock("@/features/domains/api/proxy", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/domains/api/proxy")>();
  return {
    ...actual,
    listCertificates: vi.fn(),
    listDNSProviders: vi.fn(),
  };
});

import {
  addDomain,
  getApplication,
  listDomains,
  updateDomain,
  type Application,
  type ApplicationDomain,
} from "@/features/applications/api/applications";
import { useApplicationDomains } from "@/features/applications/composables/useApplicationDomains";
import ApplicationDomainPanel from "@/features/applications/components/ApplicationDomainPanel.vue";
import { listCertificates, listDNSProviders } from "@/features/domains/api/proxy";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const composeContent = `services:
  web:
    image: example.com/web:1
    ports:
      - "3000"
  api:
    image: example.com/api:1
    ports:
      - "8000"
  worker:
    image: example.com/worker:1
`;

function composeApp(overrides: Partial<Application> = {}): Application {
  return {
    id: "app-1",
    name: "Shop",
    source_type: "compose",
    compose_content: composeContent,
    compose_service: "web",
    primary_domain: "shop.example.com",
    ...overrides,
  } as Application;
}

function row(id: string, domain: string, port = 3000): ApplicationDomain {
  return {
    id,
    application_id: "app-1",
    domain,
    is_primary: false,
    disabled: false,
    port,
    service: "",
    protocol: "http",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
  vi.clearAllMocks();
  vi.mocked(listDomains).mockResolvedValue([]);
  vi.mocked(listCertificates).mockResolvedValue([]);
  vi.mocked(listDNSProviders).mockResolvedValue([]);
  vi.mocked(getApplication).mockImplementation(async () => composeApp());
  vi.mocked(addDomain).mockImplementation(async (_id, input) => row("new", input.domain, input.port));
  vi.mocked(updateDomain).mockImplementation(async (_id, id, input) =>
    row(id, input.domain, input.port),
  );
});

function harness(app: Application) {
  const appRef = ref(app);
  let domains: ReturnType<typeof useApplicationDomains> | null = null;
  const Inner = defineComponent({
    setup() {
      domains = useApplicationDomains(appRef);
      return () => h("div");
    },
  });
  const wrapper = mount({
    render: () => h(NMessageProvider, null, { default: () => h(Inner) }),
  });
  return { wrapper, appRef, domains: () => domains! };
}

describe("Domains tab behavior", () => {
  it("pre-fills a selected compose service's port and keeps it editable", async () => {
    const { wrapper, domains } = harness(composeApp());
    await flushPromises();
    const d = domains();

    d.handleServiceChange("api");
    expect(d.draft.service).toBe("api");
    expect(d.draft.port).toBe(8000);
    d.draft.port = 9000;
    d.draft.domain = "api.example.com";
    await d.handleSave();

    expect(addDomain).toHaveBeenCalledWith(
      "app-1",
      expect.objectContaining({ domain: "api.example.com", port: 9000, service: "api" }),
    );
    wrapper.unmount();
  });

  it("clears the port when a service declares none and handles a cleared select", async () => {
    const { wrapper, domains } = harness(composeApp());
    await flushPromises();
    const d = domains();

    d.handleServiceChange("worker");
    expect(d.draft.port).toBeNull();

    // clearable NSelect emits null; Save must not throw and sends the empty
    // release-default service.
    d.handleServiceChange(null);
    expect(d.draft.service).toBe("");
    d.draft.domain = "default.example.com";
    d.draft.port = 3000;
    await expect(d.handleSave()).resolves.toBeUndefined();
    expect(addDomain).toHaveBeenCalledWith(
      "app-1",
      expect.objectContaining({ domain: "default.example.com", service: "" }),
    );
    wrapper.unmount();
  });

  it("loads a row into the form and sends the edited routing target", async () => {
    const existing = row("dom-2", "www.example.com", 8080);
    vi.mocked(listDomains).mockResolvedValue([existing]);
    const { wrapper, domains } = harness(composeApp());
    await flushPromises();
    const d = domains();

    d.startEdit(existing);
    expect(d.editingId.value).toBe("dom-2");
    expect(d.draft.domain).toBe("www.example.com");
    expect(d.draft.port).toBe(8080);
    d.draft.domain = "docs.example.com";
    d.draft.protocol = "https";
    d.draft.port = 9001;
    await d.handleSave();

    expect(updateDomain).toHaveBeenCalledWith(
      "app-1",
      "dom-2",
      expect.objectContaining({ domain: "docs.example.com", port: 9001, protocol: "https" }),
    );
    expect(d.editingId.value).toBeNull();
    wrapper.unmount();
  });

  it("renders many domains and hides the service select for non-compose apps", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "one.example.com", 3000),
      row("d2", "two.example.com", 8080),
      row("d3", "three.example.com", 9000),
    ]);
    const composable = harness(composeApp());
    await flushPromises();
    composable.wrapper.unmount();

    const Panel = defineComponent({
      render: () =>
        h(NMessageProvider, null, {
          default: () => h(ApplicationDomainPanel, { application: composeApp() }),
        }),
    });
    const composed = mount(Panel, { global: { plugins: [i18n] } });
    await flushPromises();
    await nextTick();
    expect(composed.findAll(".domain-row")).toHaveLength(3);
    expect(composed.findAll(".domain-service")).toHaveLength(1);
    composed.unmount();

    const plain = mount(
      defineComponent({
        render: () =>
          h(NMessageProvider, null, {
            default: () =>
              h(ApplicationDomainPanel, {
                application: composeApp({ source_type: "git_public", compose_content: "" }),
              }),
          }),
      }),
      { global: { plugins: [i18n] } },
    );
    await flushPromises();
    expect(plain.findAll(".domain-service")).toHaveLength(0);
    plain.unmount();
  });
});
