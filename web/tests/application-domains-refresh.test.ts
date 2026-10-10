// Regression test for the JUS-89 fix-round-2 live bug: bumpDomainsRefresh()
// called inject() after an await, where no component instance is active, so
// every alias mutation reported success as
// "Something went wrong ... Cannot read properties of undefined" and the
// certificate panel never reloaded.
//
// This mounts the real DomainEditor tree (alias panel + certificate panel)
// with the real refresh helper and only the HTTP layer mocked, then drives
// add/remove/make-primary through the UI: success must show no error and a
// toast, and the certificate panel must reload (the re-record banner
// appears/disappears without a page reload).
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
  };
});

import {
  addDomain,
  getApplication,
  listDomains,
  removeDomain,
  setPrimaryDomain,
  type ApplicationDomain,
} from "@/features/applications/api/applications";
import {
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
): ApplicationDomain {
  return {
    id,
    application_id: appId,
    domain,
    is_primary: isPrimary,
    disabled: false,
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

describe("alias mutations refresh the certificate panel", () => {
  it("add/remove/make-primary succeed without errors and move the banner", async () => {
    // One intent recorded for www, which starts detached: the banner shows.
    // Adding www attaches it (banner hides); removing it detaches it again
    // (banner returns) — both without a page reload.
    let rows: ApplicationDomain[] = [domainRow("dom-1", "app.example.com", true)];
    let mirror = "app.example.com";
    const appRef = ref({
      id: appId,
      name: "Shop",
      base_domain: "app.example.com",
    });

    vi.mocked(listDomains).mockImplementation(async () => [...rows]);
    vi.mocked(addDomain).mockImplementation(async (_id, domain) => {
      const row = domainRow(`dom-${rows.length + 1}`, domain, false);
      rows = [...rows, row];
      return row;
    });
    vi.mocked(removeDomain).mockImplementation(async (_id, domainId) => {
      rows = rows.filter((row) => row.id !== domainId);
    });
    vi.mocked(setPrimaryDomain).mockImplementation(async (_id, domainId) => {
      const target = rows.find((row) => row.id === domainId);
      if (target) {
        mirror = target.domain;
        rows = rows.map((row) => ({ ...row, is_primary: row.id === domainId }));
      }
    });
    vi.mocked(getApplication).mockImplementation(async () => {
      appRef.value.base_domain = mirror;
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

    const bannerShown = () => wrapper.text().includes("Saving it again");
    const errorsShown = () =>
      wrapper.text().includes("Something went wrong") ||
      wrapper.text().includes("Cannot read properties");

    // www is detached: the re-record banner is up.
    expect(bannerShown()).toBe(true);

    // ── add ──────────────────────────────────────────────────────────
    await wrapper.find('input[placeholder="www.example.com"]').setValue("www.example.com");
    const addButton = wrapper
      .findAll("button")
      .find((button) => button.text() === "Add domain");
    expect(addButton, "expected an Add domain button").toBeDefined();
    await addButton!.trigger("click");
    await flushPromises();
    await nextTick();

    expect(errorsShown()).toBe(false);
    expect(wrapper.text()).toContain("Domain attached.");
    expect(wrapper.text()).toContain("www.example.com");
    // The tick reloaded the attached list: www is attached, banner hides.
    expect(bannerShown()).toBe(false);

    // ── remove ───────────────────────────────────────────────────────
    const wwwRow = wrapper
      .findAll(".alias-row")
      .find((row) => row.text().includes("www.example.com"));
    expect(wwwRow, "expected the www alias row").toBeDefined();
    const removeButton = wwwRow!
      .findAll("button")
      .find((button) => button.text() === "Remove");
    expect(removeButton, "expected a Remove button").toBeDefined();
    await removeButton!.trigger("click");
    await flushPromises();
    await nextTick();
    const confirmButton = wrapper
      .findAll("button")
      .find((button) => button.text() === "Confirm");
    expect(confirmButton, "expected a Confirm button").toBeDefined();
    await confirmButton!.trigger("click");
    await flushPromises();
    await nextTick();

    expect(errorsShown()).toBe(false);
    expect(wrapper.text()).toContain("Domain detached.");
    // The tick reloaded again: www is detached, banner returns.
    expect(bannerShown()).toBe(true);

    // ── make primary ─────────────────────────────────────────────────
    // Re-add www, then promote the other alias instead: cdn first.
    await wrapper.find('input[placeholder="www.example.com"]').setValue("cdn.example.com");
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Add domain")!
      .trigger("click");
    await flushPromises();
    await nextTick();
    expect(errorsShown()).toBe(false);

    const cdnRow = wrapper
      .findAll(".alias-row")
      .find((row) => row.text().includes("cdn.example.com"));
    expect(cdnRow, "expected the cdn alias row").toBeDefined();
    await cdnRow!
      .findAll("button")
      .find((button) => button.text() === "Make primary")!
      .trigger("click");
    await flushPromises();
    await nextTick();

    expect(errorsShown()).toBe(false);
    expect(wrapper.text()).toContain("Primary domain changed.");
    expect(
      (wrapper.find('input[placeholder="app.example.com"]').element as unknown as { value: string }).value,
    ).toBe("cdn.example.com");

    wrapper.unmount();
  });
});
