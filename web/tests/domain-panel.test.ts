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
    createCertificate: vi.fn(),
    updateCertificate: vi.fn(),
    deleteCertificate: vi.fn(),
  };
});

import {
  addDomain,
  getApplication,
  listDomains,
  removeDomain,
  updateDomain,
  type Application,
  type ApplicationDomain,
} from "@/features/applications/api/applications";
import { useApplicationDomains } from "@/features/applications/composables/useApplicationDomains";
import ApplicationDomainPanel from "@/features/applications/components/ApplicationDomainPanel.vue";
import {
  createCertificate,
  deleteCertificate,
  listCertificates,
  listDNSProviders,
  updateCertificate,
} from "@/features/domains/api/proxy";
import type { Certificate } from "@/features/domains";
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

function row(
  id: string,
  domain: string,
  port = 3000,
  overrides: Partial<ApplicationDomain> = {},
): ApplicationDomain {
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
    ...overrides,
  };
}

function apiError(status: number, message = "") {
  return { status, message, cause: null };
}

function certificate(
  id: string,
  domain: string,
  overrides: Partial<Certificate> = {},
): Certificate {
  return {
    id,
    application_id: "app-1",
    domain,
    enabled: true,
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    status: "present",
    not_after: "2026-12-31T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
    ...overrides,
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
  vi.mocked(createCertificate).mockImplementation(async (input) =>
    certificate("cert-new", input.domain ?? ""),
  );
  vi.mocked(updateCertificate).mockImplementation(async (id, input) =>
    certificate(id, input.domain ?? "shop.example.com"),
  );
  vi.mocked(deleteCertificate).mockResolvedValue(undefined);
  vi.mocked(removeDomain).mockResolvedValue(undefined);
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

function mountPanel(app: Application = composeApp()) {
  const Panel = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(ApplicationDomainPanel, { application: app }),
      }),
  });
  return mount(Panel, {
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
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

  it("loads a row into the modal and sends the edited routing target", async () => {
    const existing = row("dom-2", "www.example.com", 8080);
    vi.mocked(listDomains).mockResolvedValue([existing]);
    const { wrapper, domains } = harness(composeApp());
    await flushPromises();
    const d = domains();

    d.startEdit(existing);
    expect(d.formOpen.value).toBe(true);
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
    expect(d.formOpen.value).toBe(false);
    wrapper.unmount();
  });

  it("renders one table row per domain with routing and certificate cells", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "one.example.com", 3000, { is_primary: true }),
      row("d2", "two.example.com", 8080, { service: "api", protocol: "https" }),
      row("d3", "three.example.com", 9000),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-1", "one.example.com"),
    ]);
    const composed = mountPanel();
    await flushPromises();
    await nextTick();

    expect(composed.findAll(".domain-row")).toHaveLength(3);
    const text = composed.text();
    for (const header of ["Host", "Port", "Service", "Protocol", "Primary", "Certificate", "Actions"]) {
      expect(text).toContain(header);
    }
    const first = composed.findAll(".domain-row")[0]!;
    expect(first.text()).toContain("one.example.com");
    expect(first.text()).toContain("3000");
    expect(first.text()).toContain("present");
    expect(first.text()).toContain("primary");
    expect(first.text()).toContain("Certificate");
    expect(first.text()).toContain("Edit");
    composed.unmount();
  });

  it("opens the shared add modal and hides the compose service for plain apps", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "one.example.com", 3000),
    ]);
    const composed = mountPanel();
    await flushPromises();
    await nextTick();

    await composed.find(".domain-add").trigger("click");
    await nextTick();
    expect(composed.find('input[aria-label="Domain hostname"]').exists()).toBe(true);
    expect(composed.find(".domain-service").exists()).toBe(true);
    composed.unmount();

    vi.mocked(listDomains).mockResolvedValue([]);
    const plain = mountPanel(
      composeApp({ source_type: "git_public", compose_content: "" }),
    );
    await flushPromises();
    await nextTick();
    await plain.find(".domain-add").trigger("click");
    await nextTick();
    expect(plain.find('input[aria-label="Domain hostname"]').exists()).toBe(true);
    expect(plain.find(".domain-service").exists()).toBe(false);
    plain.unmount();
  });

  it("adds a domain through the modal", async () => {
    const wrapper = mountPanel(composeApp({ source_type: "git_public", compose_content: "" }));
    await flushPromises();
    await nextTick();

    await wrapper.find(".domain-add").trigger("click");
    await nextTick();
    await wrapper.find('input[aria-label="Domain hostname"]').setValue("new.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(addDomain).toHaveBeenCalledWith(
      "app-1",
      expect.objectContaining({ domain: "new.example.com", port: 0, protocol: "http" }),
    );
    expect(wrapper.find(".domain-submit").exists()).toBe(false);
    wrapper.unmount();
  });

  it("opens the certificate modal for the clicked row, not the primary", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "shop.example.com", 3000, { is_primary: true }),
      row("d2", "api.example.com", 8000),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-primary", "shop.example.com"),
      certificate("cert-api", "api.example.com"),
    ]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    const apiRow = wrapper.findAll(".domain-row")[1]!;
    await apiRow.find(".domain-certificate").trigger("click");
    await flushPromises();
    await nextTick();

    expect(wrapper.text()).toContain("Certificate for api.example.com");
    expect(wrapper.find('input[aria-label="Domain hostname"]').exists()).toBe(false);

    await wrapper.find(".certificate-save").trigger("click");
    await flushPromises();

    expect(updateCertificate).toHaveBeenCalledWith(
      "cert-api",
      expect.objectContaining({ enabled: true, challenge: "http-01" }),
    );
    expect(wrapper.find(".certificate-modal").exists()).toBe(false);
    expect(updateCertificate).not.toHaveBeenCalledWith(
      "cert-primary",
      expect.anything(),
    );
    wrapper.unmount();
  });

  it("creates a certificate intent for the clicked non-primary domain", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "shop.example.com", 3000, { is_primary: true }),
      row("d2", "api.example.com", 8000),
    ]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    const apiRow = wrapper.findAll(".domain-row")[1]!;
    await apiRow.find(".domain-certificate").trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.text()).toContain("Certificate for api.example.com");

    await wrapper.find(".certificate-save").trigger("click");
    await flushPromises();

    expect(createCertificate).toHaveBeenCalledWith(
      expect.objectContaining({
        application_id: "app-1",
        domain: "api.example.com",
        challenge: "http-01",
      }),
    );
    expect(createCertificate).not.toHaveBeenCalledWith(
      expect.objectContaining({ domain: "shop.example.com" }),
    );
    expect(wrapper.find(".certificate-modal").exists()).toBe(false);
    wrapper.unmount();
  });

  it("deletes the old host's intent after a rename and leaves other intents alone", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-old", "old.example.com"),
      certificate("cert-other", "other.example.com"),
    ]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    const domainRow = wrapper.find(".domain-row");
    await domainRow
      .findAll("button")
      .find((button) => button.text() === "Edit")!
      .trigger("click");
    await nextTick();
    await wrapper.find('input[aria-label="Domain hostname"]').setValue("new.example.com");
    expect(wrapper.text()).toContain(
      "removes the certificate configuration recorded for old.example.com",
    );

    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(updateDomain).toHaveBeenCalledWith(
      "app-1",
      "d1",
      expect.objectContaining({ domain: "new.example.com" }),
    );
    expect(deleteCertificate).toHaveBeenCalledTimes(1);
    expect(deleteCertificate).toHaveBeenCalledWith("cert-old");
    expect(deleteCertificate).not.toHaveBeenCalledWith("cert-other");
    wrapper.unmount();
  });

  it("surfaces a certificate cleanup failure after a rename instead of swallowing it", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-old", "old.example.com"),
    ]);
    vi.mocked(deleteCertificate).mockRejectedValue(new Error("cert cleanup down"));
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .findAll(".domain-row")[0]!
      .findAll("button")
      .find((button) => button.text() === "Edit")!
      .trigger("click");
    await nextTick();
    await wrapper.find('input[aria-label="Domain hostname"]').setValue("new.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(updateDomain).toHaveBeenCalled();
    expect(deleteCertificate).toHaveBeenCalledWith("cert-old");
    expect(wrapper.text()).toContain("cert cleanup down");
    wrapper.unmount();
  });

  it("warns in the remove confirmation and deletes the row's certificate intent", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-old", "old.example.com"),
      certificate("cert-other", "other.example.com"),
    ]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .find(".domain-row")
      .findAll("button")
      .find((button) => button.text() === "Remove")!
      .trigger("click");
    await nextTick();
    expect(wrapper.text()).toContain(
      "delete its certificate configuration",
    );

    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Confirm")!
      .trigger("click");
    await flushPromises();

    expect(removeDomain).toHaveBeenCalledWith("app-1", "d1");
    expect(deleteCertificate).toHaveBeenCalledWith("cert-old");
    expect(deleteCertificate).not.toHaveBeenCalledWith("cert-other");
    wrapper.unmount();
  });

  it("resets the certificate draft and heading when reopening on another row", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "a.example.com", 3000, { is_primary: true }),
      row("d2", "b.example.com", 8000),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-a", "a.example.com", {
        challenge: "dns-01",
        dns_provider_id: "provider-1",
        wildcard: true,
      }),
      certificate("cert-b", "b.example.com"),
    ]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    const rows = wrapper.findAll(".domain-row");
    await rows[0]!.find(".domain-certificate").trigger("click");
    await flushPromises();
    expect(wrapper.find(".certificate-modal").text()).toContain(
      "Certificate for a.example.com",
    );
    await wrapper
      .find(".certificate-modal")
      .findAll("button")
      .find((button) => button.text() === "Cancel")!
      .trigger("click");
    await flushPromises();
    expect(wrapper.find(".certificate-modal").exists()).toBe(false);

    await rows[1]!.find(".domain-certificate").trigger("click");
    await flushPromises();
    const reopened = wrapper.find(".certificate-modal").text();
    expect(reopened).toContain("Certificate for b.example.com");
    expect(reopened).not.toContain("a.example.com");

    await wrapper.find(".certificate-save").trigger("click");
    await flushPromises();
    expect(updateCertificate).toHaveBeenCalledWith(
      "cert-b",
      expect.objectContaining({
        challenge: "http-01",
      }),
    );
    wrapper.unmount();
  });

  it("deletes exactly the clicked row's certificate when two intents exist", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "a.example.com", 3000, { is_primary: true }),
      row("d2", "b.example.com", 8000),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-a", "a.example.com"),
      certificate("cert-b", "b.example.com"),
    ]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .findAll(".domain-row")[1]!
      .find(".domain-certificate")
      .trigger("click");
    await flushPromises();
    await wrapper
      .find(".certificate-modal")
      .findAll("button")
      .find((button) => button.text() === "Delete configuration")!
      .trigger("click");
    await nextTick();
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Confirm")!
      .trigger("click");
    await flushPromises();

    expect(deleteCertificate).toHaveBeenCalledWith("cert-b");
    expect(deleteCertificate).not.toHaveBeenCalledWith("cert-a");
    wrapper.unmount();
  });

  it("keeps the certificate modal open when the save fails", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "b.example.com", 8000),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-b", "b.example.com"),
    ]);
    vi.mocked(updateCertificate).mockRejectedValue(new Error("save down"));
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper.find(".domain-row").find(".domain-certificate").trigger("click");
    await flushPromises();
    await wrapper.find(".certificate-save").trigger("click");
    await flushPromises();

    expect(updateCertificate).toHaveBeenCalledWith(
      "cert-b",
      expect.anything(),
    );
    expect(wrapper.find(".certificate-modal").exists()).toBe(true);
    expect(wrapper.find(".certificate-modal").text()).toContain("save down");
    wrapper.unmount();
  });

  it("does not call delete when the server cascade already removed the renamed intent", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates)
      .mockResolvedValueOnce([
        certificate("cert-old", "old.example.com"),
        certificate("cert-other", "other.example.com"),
      ])
      .mockResolvedValue([certificate("cert-other", "other.example.com")]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .find(".domain-row")
      .findAll("button")
      .find((button) => button.text() === "Edit")!
      .trigger("click");
    await nextTick();
    await wrapper.find('input[aria-label="Domain hostname"]').setValue("new.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(updateDomain).toHaveBeenCalled();
    expect(deleteCertificate).not.toHaveBeenCalled();
    expect(wrapper.text()).not.toContain("It may have been deleted already");
    wrapper.unmount();
  });

  it("treats a 404 certificate delete after a rename as already done", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-old", "old.example.com"),
    ]);
    vi.mocked(deleteCertificate).mockRejectedValue(apiError(404, "not found"));
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .find(".domain-row")
      .findAll("button")
      .find((button) => button.text() === "Edit")!
      .trigger("click");
    await nextTick();
    await wrapper.find('input[aria-label="Domain hostname"]').setValue("new.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(deleteCertificate).toHaveBeenCalledWith("cert-old");
    expect(wrapper.text()).not.toContain("It may have been deleted already");
    wrapper.unmount();
  });

  it("does not call delete when the server cascade already removed the detached intent", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates)
      .mockResolvedValueOnce([
        certificate("cert-old", "old.example.com"),
        certificate("cert-other", "other.example.com"),
      ])
      .mockResolvedValue([certificate("cert-other", "other.example.com")]);
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .find(".domain-row")
      .findAll("button")
      .find((button) => button.text() === "Remove")!
      .trigger("click");
    await nextTick();
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Confirm")!
      .trigger("click");
    await flushPromises();

    expect(removeDomain).toHaveBeenCalledWith("app-1", "d1");
    expect(deleteCertificate).not.toHaveBeenCalled();
    expect(wrapper.text()).not.toContain("It may have been deleted already");
    wrapper.unmount();
  });

  it("treats a 404 certificate delete after a remove as already done", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-old", "old.example.com"),
    ]);
    vi.mocked(deleteCertificate).mockRejectedValue(apiError(404, "not found"));
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .find(".domain-row")
      .findAll("button")
      .find((button) => button.text() === "Remove")!
      .trigger("click");
    await nextTick();
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Confirm")!
      .trigger("click");
    await flushPromises();

    expect(deleteCertificate).toHaveBeenCalledWith("cert-old");
    expect(wrapper.text()).not.toContain("It may have been deleted already");
    wrapper.unmount();
  });

  it("surfaces a non-404 certificate cleanup failure on remove", async () => {
    vi.mocked(listDomains).mockResolvedValue([
      row("d1", "old.example.com", 3000, { is_primary: true }),
    ]);
    vi.mocked(listCertificates).mockResolvedValue([
      certificate("cert-old", "old.example.com"),
    ]);
    vi.mocked(deleteCertificate).mockRejectedValue(new Error("remove cleanup down"));
    const wrapper = mountPanel();
    await flushPromises();
    await nextTick();

    await wrapper
      .find(".domain-row")
      .findAll("button")
      .find((button) => button.text() === "Remove")!
      .trigger("click");
    await nextTick();
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Confirm")!
      .trigger("click");
    await flushPromises();

    expect(deleteCertificate).toHaveBeenCalledWith("cert-old");
    expect(wrapper.text()).toContain("remove cleanup down");
    wrapper.unmount();
  });
});
