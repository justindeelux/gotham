import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
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
    updateApplication: vi.fn(),
  };
});

vi.mock("@/features/domains/api/proxy", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/domains/api/proxy")>();
  return {
    ...actual,
    listCertificates: vi.fn(),
    listDNSProviders: vi.fn(),
    deleteCertificate: vi.fn(),
  };
});

import {
  addDomain,
  getApplication,
  listDomains,
  removeDomain,
  setPrimaryDomain,
  type ApplicationDomain,
  type DomainInput,
} from "@/features/applications/api/applications";
import {
  deleteCertificate,
  listCertificates,
  listDNSProviders,
} from "@/features/domains/api/proxy";
import DomainEditor from "@/features/applications/components/DomainEditor.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
  vi.clearAllMocks();
});

const appId = "app-1";

function domainRow(
  id: string,
  domain: string,
  isPrimary: boolean,
  port = 3000,
): ApplicationDomain {
  return {
    id,
    application_id: appId,
    domain,
    is_primary: isPrimary,
    disabled: false,
    port,
    service: "",
    protocol: "http",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function testRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div/>" } },
      { name: "domains", path: "/domains", component: { template: "<div/>" } },
    ],
  });
  router.push("/");
  return router;
}

describe("domain mutations work through the table and modal", () => {
  it("add/remove/make-primary succeed without errors", async () => {
    let rows: ApplicationDomain[] = [domainRow("dom-1", "app.example.com", true)];
    let primary = "app.example.com";
    const appRef = ref({
      id: appId,
      name: "Shop",
      source_type: "git_public",
      primary_domain: primary,
    });

    vi.mocked(listDomains).mockImplementation(async () => [...rows]);
    vi.mocked(addDomain).mockImplementation(async (_id, input: DomainInput) => {
      const row = domainRow(`dom-${rows.length + 1}`, input.domain, false, input.port);
      rows = [...rows, row];
      return row;
    });
    vi.mocked(removeDomain).mockImplementation(async (_id, domainId) => {
      rows = rows.filter((row) => row.id !== domainId);
    });
    vi.mocked(setPrimaryDomain).mockImplementation(async (_id, domainId) => {
      const target = rows.find((row) => row.id === domainId);
      if (target) {
        primary = target.domain;
        rows = rows.map((row) => ({ ...row, is_primary: row.id === domainId }));
      }
    });
    vi.mocked(getApplication).mockImplementation(async () => {
      appRef.value.primary_domain = primary;
      return { ...appRef.value };
    });
    vi.mocked(listCertificates).mockResolvedValue([
      {
        id: "cert-1",
        application_id: appId,
        domain: "www.example.com",
        enabled: true,
        challenge: "http-01",
        dns_provider_id: "",
        wildcard: false,
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-02T00:00:00Z",
      },
    ]);
    vi.mocked(listDNSProviders).mockResolvedValue([]);
    vi.mocked(deleteCertificate).mockResolvedValue(undefined);

    const router = testRouter();
    await router.isReady();
    const Root = defineComponent({
      setup() {
        return () =>
          h(NMessageProvider, null, {
            default: () => h(DomainEditor, { application: appRef.value }),
          });
      },
    });
    const wrapper = mount(Root, {
      global: { plugins: [router, i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    await nextTick();

    const errorsShown = () =>
      wrapper.text().includes("Something went wrong") ||
      wrapper.text().includes("Cannot read properties");

    await wrapper.find(".domain-add").trigger("click");
    await nextTick();
    await wrapper
      .find('input[aria-label="Domain hostname"]')
      .setValue("www.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();
    await nextTick();

    expect(errorsShown()).toBe(false);
    expect(wrapper.text()).toContain("Domain attached.");
    expect(wrapper.text()).toContain("www.example.com");
    expect(vi.mocked(addDomain)).toHaveBeenCalledWith(
      appId,
      expect.objectContaining({ domain: "www.example.com", port: 0 }),
    );

    const wwwRow = () =>
      wrapper
        .findAll(".domain-row")
        .find((row) => row.text().includes("www.example.com") && row.text().includes("Remove"));
    const removeButton = wwwRow()!
      .findAll("button")
      .find((button) => button.text() === "Remove");
    await removeButton!.trigger("click");
    await flushPromises();
    await nextTick();
    const confirmButton = wrapper
      .findAll("button")
      .find((button) => button.text() === "Confirm");
    await confirmButton!.trigger("click");
    await flushPromises();
    await nextTick();

    expect(errorsShown()).toBe(false);
    expect(wrapper.text()).toContain("Domain detached.");
    expect(vi.mocked(deleteCertificate)).toHaveBeenCalledWith("cert-1");
    expect(
      wrapper.findAll(".domain-row").some((row) => row.text().includes("www.example.com")),
    ).toBe(false);

    await wrapper.find(".domain-add").trigger("click");
    await nextTick();
    await wrapper
      .find('input[aria-label="Domain hostname"]')
      .setValue("cdn.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();
    await nextTick();

    const cdnRow = wrapper
      .findAll(".domain-row")
      .find((row) => row.text().includes("cdn.example.com"));
    await cdnRow!
      .findAll("button")
      .find((button) => button.text() === "Make primary")!
      .trigger("click");
    await flushPromises();
    await nextTick();

    expect(errorsShown()).toBe(false);
    expect(wrapper.text()).toContain("Primary domain changed.");
    expect(appRef.value.primary_domain).toBe("cdn.example.com");
  });
});
