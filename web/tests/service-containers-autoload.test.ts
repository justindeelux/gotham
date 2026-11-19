import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import type { Router } from "vue-router";

import * as servicesApi from "@/features/services/api/services";
import type {
  ComposeServiceContainer,
  Service,
  ServiceDeploy,
} from "@/features/services/api/services";
import { useServiceDetail } from "@/features/services/composables/useServiceDetail";
import ServiceDetailPage from "@/features/services/pages/ServiceDetailPage.vue";
import { useServersStore } from "@/features/servers/stores/servers";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("naive-ui", async (importOriginal) => {
  const original = await importOriginal<typeof import("naive-ui")>();
  return {
    ...original,
    useMessage: () => ({
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      info: vi.fn(),
    }),
  };
});

function service(id: string): Service {
  return {
    id,
    name: `blog-${id}`,
    status: "running",
    server_id: "srv-1",
    server_name: "node-1",
    environment_id: "env-1",
    environment_name: "production",
    project_id: "proj-1",
    project_name: "shop",
    compose_project: `gotham-${id}`,
    compose_yaml: "services:\n  web:\n    image: nginx\n",
    env: {},
    domains: [],
    created_at: "2026-09-01T10:00:00Z",
    updated_at: "2026-09-02T10:00:00Z",
  };
}

const containerRows: ComposeServiceContainer[] = [
  {
    service: "web",
    container_id: "c1",
    name: "shop-web-1",
    image: "nginx:latest",
    state: "running",
    status: "Up 2 minutes",
  },
];

function deploy(state: ServiceDeploy["state"]): ServiceDeploy {
  return {
    id: "dep-1",
    service_id: "svc-1",
    state,
    created_at: "2026-09-02T10:00:00Z",
    updated_at: "2026-09-02T10:01:00Z",
  };
}

const pageStubs = {
  ServiceDetailHeader: true,
  ServiceOverviewCard: true,
  ServiceDeployHistoryCard: true,
  ServiceDeployBanner: true,
  ServiceComposeCard: true,
  ServiceEnvCard: true,
  ServiceDomainsTab: true,
  ResourceBackupsTab: true,
  ResourceSettingsTab: true,
  ServiceLogs: true,
};

function detailRouter(): Router {
  const stub = { template: "<div />" };
  const routes = [
    {
      path: "/projects/:projectId/environments/:environmentId/services/:id",
      name: "service-detail",
      component: stub,
    },
    {
      path: "/projects/:projectId/environments/:environmentId",
      name: "environment-detail",
      component: stub,
    },
    { path: "/projects/:projectId", name: "project-detail", component: stub },
    { path: "/projects", name: "projects", component: stub },
  ];
  return createRouter({ history: createMemoryHistory(), routes });
}

async function mountPage(
  path = "/projects/proj-1/environments/env-1/services/svc-1",
): Promise<{ wrapper: VueWrapper; router: Router }> {
  const router = detailRouter();
  await router.push(path);
  await router.isReady();
  const wrapper = mount(ServiceDetailPage, {
    global: { plugins: [router, i18n], stubs: pageStubs },
  });
  await flushPromises();
  return { wrapper, router };
}

function tab(wrapper: VueWrapper, label: string) {
  return wrapper.findAll(".n-tabs-tab").find((node) => node.text() === label);
}

const listContainers = () => vi.mocked(servicesApi.listServiceContainers);

let detail: ReturnType<typeof useServiceDetail> | null = null;
let harness: VueWrapper | null = null;

const ComposableHarness = defineComponent({
  setup() {
    detail = useServiceDetail();
    return () => h("div");
  },
});

async function mountHarness(): Promise<ReturnType<typeof useServiceDetail>> {
  const router = detailRouter();
  await router.push("/projects/proj-1/environments/env-1/services/svc-1");
  await router.isReady();
  harness = mount(ComposableHarness, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return detail!;
}

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  harness = null;
  vi.spyOn(servicesApi, "getService").mockResolvedValue(service("svc-1"));
  vi.spyOn(servicesApi, "listServiceDeploys").mockResolvedValue([]);
  vi.spyOn(servicesApi, "listServiceContainers").mockResolvedValue(containerRows);
  vi.spyOn(servicesApi, "stopService").mockResolvedValue(service("svc-1"));
  vi.spyOn(useServersStore(), "fetchServers").mockResolvedValue(undefined);
});

afterEach(() => {
  harness?.unmount();
  harness = null;
  detail = null;
  vi.restoreAllMocks();
});

describe("service containers tab", () => {
  it("reads the container list when the tab opens, not before", async () => {
    const { wrapper } = await mountPage();
    expect(listContainers()).not.toHaveBeenCalled();

    await tab(wrapper, "Containers")!.trigger("click");
    await flushPromises();

    expect(listContainers()).toHaveBeenCalledTimes(1);
    expect(listContainers()).toHaveBeenCalledWith("svc-1");
    expect(wrapper.text()).toContain("shop-web-1");
    wrapper.unmount();
  });

  it("keeps the loaded rows when the tab is reopened", async () => {
    const { wrapper } = await mountPage();
    await tab(wrapper, "Containers")!.trigger("click");
    await flushPromises();
    await tab(wrapper, "Overview")!.trigger("click");
    await flushPromises();
    await tab(wrapper, "Containers")!.trigger("click");
    await flushPromises();

    expect(listContainers()).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("shop-web-1");
    wrapper.unmount();
  });

  it("reads again when the page switches service with the tab open", async () => {
    const { wrapper, router } = await mountPage();
    await tab(wrapper, "Containers")!.trigger("click");
    await flushPromises();

    vi.mocked(servicesApi.getService).mockResolvedValue(service("svc-2"));
    await router.push("/projects/proj-1/environments/env-1/services/svc-2");
    await flushPromises();

    expect(listContainers()).toHaveBeenCalledTimes(2);
    expect(listContainers()).toHaveBeenLastCalledWith("svc-2");
    wrapper.unmount();
  });

  it("drops a read that answers after the page left that service", async () => {
    let resolveOld: () => void = () => undefined;
    listContainers().mockImplementationOnce(
      () =>
        new Promise<ComposeServiceContainer[]>((resolve) => {
          resolveOld = () => resolve(containerRows);
        }),
    );
    const { wrapper, router } = await mountPage();
    await tab(wrapper, "Containers")!.trigger("click");
    await flushPromises();

    listContainers().mockResolvedValue([]);
    vi.mocked(servicesApi.getService).mockResolvedValue(service("svc-2"));
    await router.push("/projects/proj-1/environments/env-1/services/svc-2");
    await flushPromises();
    resolveOld();
    await flushPromises();

    expect(listContainers()).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).not.toContain("shop-web-1");
    expect(wrapper.text()).toContain("No container list loaded.");
    wrapper.unmount();
  });

  it("spins instead of showing the empty state until the read finishes", async () => {    let resolveRead: () => void = () => undefined;
    listContainers().mockReturnValue(
      new Promise<ComposeServiceContainer[]>((resolve) => {
        resolveRead = () => resolve([]);
      }),
    );
    const { wrapper } = await mountPage();
    await tab(wrapper, "Containers")!.trigger("click");
    await flushPromises();

    expect(wrapper.find(".n-card .n-spin").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("No container list loaded.");

    resolveRead();
    await flushPromises();

    expect(wrapper.find(".n-card .n-spin").exists()).toBe(false);
    expect(wrapper.text()).toContain("No container list loaded.");
    wrapper.unmount();
  });
});

describe("service containers refresh after a lifecycle action", () => {
  it("reloads rows the user has already read after a successful stop", async () => {
    const page = await mountHarness();
    await page.loadContainers();
    expect(listContainers()).toHaveBeenCalledTimes(1);

    await page.handleStop();

    expect(listContainers()).toHaveBeenCalledTimes(2);
  });

  it("does not read the list for an action the user never opened", async () => {
    const page = await mountHarness();
    expect(page.containersLoaded.value).toBe(false);

    await page.handleStop();

    expect(listContainers()).not.toHaveBeenCalled();
  });

  it("reloads the rows once a followed deploy finishes", async () => {
    vi.useFakeTimers();
    try {
      vi.mocked(servicesApi.listServiceDeploys).mockResolvedValue([
        deploy("deploying"),
      ]);
      const page = await mountHarness();
      await page.loadContainers();
      expect(listContainers()).toHaveBeenCalledTimes(1);

      vi.mocked(servicesApi.listServiceDeploys).mockResolvedValue([
        deploy("running"),
      ]);
      await vi.advanceTimersByTimeAsync(5_000);
      await flushPromises();

      expect(listContainers()).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});
