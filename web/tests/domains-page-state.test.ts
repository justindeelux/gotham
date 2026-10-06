// Page-instance state regression tests for the domains split (JUS-24 fix 1).
//
// The dialog drafts, open flags and statsReady must be created per page mount
// and dropped on unmount: a typed credential must never survive a route
// change, dialogs must not re-open, and KPI tiles must show a dash until the
// first load settles. Teleport is stubbed so dialog content renders inline.
import { NButton, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/domains/api/proxy", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/domains/api/proxy")>();
  return {
    ...actual,
    listDNSProviders: vi.fn(),
    listCertificates: vi.fn(),
    listRedirects: vi.fn(),
    createDNSProvider: vi.fn(),
    updateDNSProvider: vi.fn(),
    createRedirect: vi.fn(),
  };
});
vi.mock("@/features/applications", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/applications")>();
  return { ...actual, listApplications: vi.fn() };
});

import {
  createRedirect,
  listCertificates,
  listDNSProviders,
  listRedirects,
  updateDNSProvider,
} from "@/features/domains/api/proxy";
import domainsEn from "@/features/domains/locales/en";
import domainsVi from "@/features/domains/locales/vi";
import { listApplications as listApps } from "@/features/applications";
import DomainsPage from "@/features/domains/pages/DomainsPage.vue";
import { provideCertificates } from "@/features/domains/composables/useCertificates";
import { provideDomainsOverview } from "@/features/domains/composables/useDomainsOverview";
import { provideProviders } from "@/features/domains/composables/useProviders";
import type { ProvidersState } from "@/features/domains/composables/useProviders";
import { provideRedirects } from "@/features/domains/composables/useRedirects";
import type { RedirectsState } from "@/features/domains/composables/useRedirects";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

/** Catalogs render through the real composer: register before mounting. */
beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { domains: domainsEn });
  i18n.global.mergeLocaleMessage("vi", { domains: domainsVi });
  syncComposerLocale("en");
});

function never<T>(): Promise<T> {
  return new Promise<T>(() => undefined);
}

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

function testRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div/>" } },
      { name: "projects", path: "/projects", component: { template: "<div/>" } },
    ],
  });
  router.push("/");
  return router;
}

async function mountPage() {
  const router = testRouter();
  await router.isReady();
  const wrapper = mount(shell(DomainsPage), {
    global: { plugins: [router, i18n], stubs: { teleport: true } },
  });
  await nextTick();
  await flushPromises();
  await nextTick();
  return wrapper;
}

/** stateHarness mounts a bare provider for direct composable tests. */
async function providersHarness(): Promise<{ wrapper: VueWrapper; state: ProvidersState }> {
  let state!: ProvidersState;
  const Inner = defineComponent({
    setup() {
      state = provideProviders();
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({ render: () => h(NMessageProvider, null, { default: () => h(Inner) }) }),
  );
  await flushPromises();
  return { wrapper, state };
}

async function redirectsHarness(): Promise<{ wrapper: VueWrapper; state: RedirectsState }> {
  let state!: RedirectsState;
  const Inner = defineComponent({
    setup() {
      state = provideRedirects();
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({ render: () => h(NMessageProvider, null, { default: () => h(Inner) }) }),
  );
  await flushPromises();
  return { wrapper, state };
}

/** clickButton clicks the first naive button with the exact label. */
async function clickButton(wrapper: VueWrapper, label: string): Promise<void> {
  const buttons = wrapper.findAllComponents(NButton);
  const target = buttons.find((button) => button.text() === label);
  expect(target, `expected a button labelled ${label}`).toBeDefined();
  await target!.trigger("click");
  await flushPromises();
  await nextTick();
}

const storedProvider = {
  id: "prov-1",
  provider: "cloudflare",
  name: "Production Cloudflare",
  zones: ["example.com"],
  enabled: true,
  credentials_set: true,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

describe("domains page instance state", () => {
  it("drops a typed credential and closes the dialog on remount", async () => {
    setActivePinia(createPinia());
    vi.mocked(listDNSProviders).mockResolvedValue([]);
    vi.mocked(listCertificates).mockResolvedValue([]);
    vi.mocked(listRedirects).mockResolvedValue([]);
    vi.mocked(listApps).mockResolvedValue([]);

    let wrapper = await mountPage();
    await clickButton(wrapper, "Add DNS provider");
    expect(wrapper.html()).toContain("Provider type");
    const credential = wrapper.find(".field-credential input");
    expect(credential.exists()).toBe(true);
    await credential.setValue("SECRET-TOKEN");
    expect((credential.element as unknown as { value: string }).value).toBe("SECRET-TOKEN");
    wrapper.unmount();

    // A fresh mount reopens with a blank draft: the token is gone.
    wrapper = await mountPage();
    expect(wrapper.html()).not.toContain("field-credential");
    await clickButton(wrapper, "Add DNS provider");
    const reopened = wrapper.find(".field-credential input");
    expect(reopened.exists()).toBe(true);
    expect((reopened.element as unknown as { value: string }).value).toBe("");
    wrapper.unmount();
  });

  it("starts the certificate dialog and the KPI tiles clean on remount", async () => {
    setActivePinia(createPinia());
    vi.mocked(listDNSProviders).mockReturnValue(never());
    vi.mocked(listCertificates).mockReturnValue(never());
    vi.mocked(listRedirects).mockReturnValue(never());
    vi.mocked(listApps).mockReturnValue(never());

    let wrapper = await mountPage();
    // Tiles show a dash while the first load is pending.
    expect(wrapper.html()).toContain("stat-value num\">—");
    await clickButton(wrapper, "Add certificate");
    expect(wrapper.html()).toContain("Add certificate");
    wrapper.unmount();

    wrapper = await mountPage();
    expect(wrapper.html()).not.toContain("field-application");
    expect(wrapper.html()).toContain("stat-value num\">—");
    wrapper.unmount();
  });
});

describe("useProviders", () => {
  it("omits an untouched credential on update and sends a typed one", async () => {
    setActivePinia(createPinia());
    vi.mocked(updateDNSProvider).mockResolvedValue(storedProvider as never);
    vi.mocked(listDNSProviders).mockResolvedValue([]);

    const { wrapper, state } = await providersHarness();
    state.openProviderEdit(storedProvider as never);
    await state.handleSaveProvider();
    expect(vi.mocked(updateDNSProvider)).toHaveBeenCalledWith(
      "prov-1",
      expect.not.objectContaining({ credential: expect.anything() }),
    );

    state.openProviderEdit(storedProvider as never);
    state.providerForm.value.credential = "ROTATED-TOKEN";
    await state.handleSaveProvider();
    expect(vi.mocked(updateDNSProvider)).toHaveBeenCalledWith(
      "prov-1",
      expect.objectContaining({ credential: "ROTATED-TOKEN" }),
    );
    // A successful write drops the plaintext token from memory.
    expect(state.providerForm.value.credential).toBe("");
    expect(state.providerOpen.value).toBe(false);
    wrapper.unmount();
  });

  it("clearProviderCredential drops the typed token", async () => {
    setActivePinia(createPinia());
    const { wrapper, state } = await providersHarness();
    state.openProviderCreate();
    state.providerForm.value.credential = "SECRET-TOKEN";
    state.clearProviderCredential();
    expect(state.providerForm.value.credential).toBe("");
    wrapper.unmount();
  });
});

describe("useRedirects", () => {
  it("keeps the application after create so a second rule is quick", async () => {
    setActivePinia(createPinia());
    vi.mocked(createRedirect).mockResolvedValue({ id: "redir-1" } as never);
    vi.mocked(listRedirects).mockResolvedValue([]);

    const { wrapper, state } = await redirectsHarness();
    state.redirectForm.value = {
      application_id: "app-1",
      source_domain: "go.example.com",
      target_domain: "checkout.example.com",
      code: 302,
      preserve_path: true,
      enabled: true,
    };
    await state.handleCreateRedirect();
    expect(vi.mocked(createRedirect)).toHaveBeenCalledWith(
      expect.objectContaining({ application_id: "app-1" }),
    );
    expect(state.redirectForm.value.application_id).toBe("app-1");
    expect(state.redirectForm.value.source_domain).toBe("");
    wrapper.unmount();
  });
});

describe("useCertificates and useDomainsOverview wiring", () => {
  it("creates isolated state per provide call", async () => {
    setActivePinia(createPinia());
    const order: string[] = [];
    const First = defineComponent({
      setup() {
        provideCertificates().openCertificateCreate();
        provideDomainsOverview();
        order.push("first");
        return () => h("div");
      },
    });
    const Second = defineComponent({
      setup() {
        const certificates = provideCertificates();
        const overview = provideDomainsOverview();
        order.push(certificates.certificateOpen.value ? "leaked-open" : "closed");
        order.push(overview.statsBlocked.value ? "blocked" : "ready");
        return () => h("div");
      },
    });
    const wrapper = mount(
      defineComponent({
        render: () => h(NMessageProvider, null, { default: () => [h(First), h(Second)] }),
      }),
    );
    await flushPromises();
    expect(order).toEqual(["first", "closed", "blocked"]);
    wrapper.unmount();
  });
});
