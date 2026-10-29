import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

const messages = {
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
};

vi.mock("naive-ui", async (importOriginal) => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return { ...actual, useMessage: () => messages };
});

vi.mock("@/features/services/api/services", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/services/api/services")>();
  return {
    ...actual,
    listServiceDomains: vi.fn(),
    addServiceDomain: vi.fn(),
    updateServiceDomain: vi.fn(),
    removeServiceDomain: vi.fn(),
    getService: vi.fn(),
  };
});

const state = vi.hoisted(() => ({
  canWrite: true,
  composeYaml: "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:3000\"\n",
}));

vi.mock("@/features/services/composables/useServiceDetail", async () => {
  const { computed } = await import("vue");
  return {
    serviceDetailKey: Symbol("service-detail"),
    useServiceDetailContext: () => ({
      service: computed(() => ({
        id: "svc-1",
        name: "blog",
        compose_yaml: state.composeYaml,
        pending_changes: [],
        domains: [],
      })),
      serviceId: computed(() => "svc-1"),
      canWrite: computed(() => state.canWrite),
    }),
  };
});

import {
  addServiceDomain,
  getService,
  listServiceDomains,
  type Service,
  type ServiceDomain,
} from "@/features/services/api/services";
import { defaultDomainTarget, serviceDomainHostSchema } from "@/features/services/schemas/domains";
import ServiceDomainsTab from "@/features/services/components/ServiceDomainsTab.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

function managedRow(overrides: Partial<ServiceDomain> = {}): ServiceDomain {
  return {
    id: "dom-1",
    service_id: "svc-1",
    domain: "app.example.com",
    compose_service: "web",
    port: 3000,
    protocol: "http",
    disabled: false,
    source: "managed",
    status: "routed",
    ...overrides,
  };
}

function composeRow(overrides: Partial<ServiceDomain> = {}): ServiceDomain {
  return {
    service_id: "svc-1",
    domain: "legacy.example.com",
    compose_service: "web",
    port: 3001,
    protocol: "http",
    disabled: false,
    source: "compose",
    status: "held",
    reason: "container port 3001 is not published; add it to the compose service ports",
    ...overrides,
  };
}

function apiError(status: number, message: string) {
  return { status, message, cause: null };
}

function mountTab() {
  const Host = defineComponent({
    render: () =>
      h(NMessageProvider, null, { default: () => h(ServiceDomainsTab) }),
  });
  return mount(Host, {
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
  state.canWrite = true;
  state.composeYaml =
    "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:3000\"\n";
  vi.clearAllMocks();
  vi.mocked(listServiceDomains).mockResolvedValue([]);
  vi.mocked(getService).mockResolvedValue({} as Service);
  vi.mocked(addServiceDomain).mockResolvedValue(managedRow());
});

describe("service domain schemas", () => {
  it("accepts a plain hostname and rejects wildcards or empty hosts", () => {
    expect(serviceDomainHostSchema.safeParse("app.example.com").success).toBe(true);
    expect(serviceDomainHostSchema.safeParse("").success).toBe(false);
    expect(serviceDomainHostSchema.safeParse("*.example.com").success).toBe(false);
    expect(serviceDomainHostSchema.safeParse("not a domain").success).toBe(false);
  });

  it("preselects the target only when one container port is routable", () => {
    expect(
      defaultDomainTarget(
        "services:\n  web:\n    image: nginx\n    ports:\n      - \"3000\"\n",
      ),
    ).toEqual({ service: "web", port: 3000 });
    expect(
      defaultDomainTarget(
        "services:\n  web:\n    image: nginx\n    ports:\n      - \"3000\"\n  api:\n    image: api\n    expose:\n      - \"8000\"\n",
      ),
    ).toBeNull();
    expect(
      defaultDomainTarget("services:\n  worker:\n    image: worker\n"),
    ).toBeNull();
  });
});

describe("service domains tab", () => {
  it("lists managed and compose rows with their routing status", async () => {
    vi.mocked(listServiceDomains).mockResolvedValue([
      managedRow(),
      composeRow(),
    ]);
    const wrapper = mountTab();
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain("app.example.com");
    expect(text).toContain("Routed");
    expect(text).toContain("legacy.example.com");
    expect(text).toContain("Held back");
    expect(text).toContain("From compose file");
    expect(text).toContain("container port 3001 is not published");
    expect(wrapper.find(".domain-open-compose").exists()).toBe(true);
    expect(wrapper.findAll(".n-switch")).toHaveLength(1);
    wrapper.unmount();
  });

  it("hides every write action for a read-only member", async () => {
    state.canWrite = false;
    vi.mocked(listServiceDomains).mockResolvedValue([managedRow()]);
    const wrapper = mountTab();
    await flushPromises();

    const labels = wrapper.findAll("button").map((node) => node.text().trim());
    expect(labels).not.toContain("Add domain");
    expect(labels).not.toContain("Edit");
    expect(labels).not.toContain("Remove");
    expect(wrapper.text()).toContain("app.example.com");
    wrapper.unmount();
  });

  it("pre-fills the only container port and saves the host-only case", async () => {
    const wrapper = mountTab();
    await flushPromises();

    await wrapper.find(".domain-add").trigger("click");
    await flushPromises();
    expect(wrapper.find(".domain-target").text()).toContain("web");
    expect(
      (wrapper.find(".domain-port input").element as { value: string }).value,
    ).toBe("3000");

    await wrapper.find(".domain-host input").setValue("new.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(addServiceDomain).toHaveBeenCalledWith("svc-1", {
      domain: "new.example.com",
      compose_service: "web",
      port: 3000,
      protocol: "http",
      disabled: false,
    });
    wrapper.unmount();
  });

  it("shows the API conflict under the host field", async () => {
    vi.mocked(addServiceDomain).mockRejectedValue(
      apiError(409, "domain already in use by another application"),
    );
    const wrapper = mountTab();
    await flushPromises();

    await wrapper.find(".domain-add").trigger("click");
    await flushPromises();
    await wrapper.find(".domain-host input").setValue("taken.example.com");
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain(
      "domain already in use by another application",
    );
    wrapper.unmount();
  });

  it("rejects an empty host before calling the API", async () => {
    const wrapper = mountTab();
    await flushPromises();

    await wrapper.find(".domain-add").trigger("click");
    await flushPromises();
    await wrapper.find(".domain-submit").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Enter a plain hostname");
    expect(addServiceDomain).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
