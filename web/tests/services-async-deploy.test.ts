import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";

import * as servicesApi from "../src/features/services/api/services";
import type { Service, ServiceDeploy } from "../src/features/services/api/services";
import ServiceDeployBanner from "../src/features/services/components/ServiceDeployBanner.vue";
import { useServiceDetail } from "../src/features/services/composables/useServiceDetail";
import { useServersStore } from "../src/features/servers/stores/servers";

const baseService: Service = {
  id: "svc-1",
  name: "blog-staging",
  status: "creating",
  server_id: "srv-1",
  server_name: "node-1",
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "shop",
  compose_project: "gotham-svc-1",
  compose_yaml: "services:\n  web:\n    image: nginx\n",
  env: {},
  domains: [],
  created_at: "2026-09-01T10:00:00Z",
  updated_at: "2026-09-02T10:00:00Z",
};

function deploy(state: ServiceDeploy["state"], error?: string): ServiceDeploy {
  return {
    id: "dep-1",
    service_id: "svc-1",
    state,
    error,
    created_at: "2026-09-02T10:00:00Z",
    updated_at: "2026-09-02T10:01:00Z",
  };
}

let detail: ReturnType<typeof useServiceDetail> | null = null;
let wrapper: ReturnType<typeof mount> | null = null;

const Harness = defineComponent({
  setup() {
    detail = useServiceDetail();
    return () =>
      h("div", [
        h(ServiceDeployBanner, {
          isDeploying: detail?.isDeploying.value ?? false,
        }),
      ]);
  },
});

async function setup(
  service: Service,
  deploys: ServiceDeploy[],
): Promise<void> {
  vi.spyOn(servicesApi, "getService").mockResolvedValue(service);
  vi.spyOn(servicesApi, "listServiceDeploys").mockResolvedValue(deploys);
  vi.spyOn(useServersStore(), "fetchServers").mockResolvedValue(undefined);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/projects/:projectId/environments/:environmentId/services/:id",
        name: "service-detail",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/projects/proj-1/environments/env-1/services/svc-1");
  await router.isReady();
  wrapper = mount(
    { render: () => h(NMessageProvider, null, { default: () => h(Harness) }) },
    { global: { plugins: [router] } },
  );
  await flushPromises();
}

function banner() {
  return wrapper?.find('[data-testid="service-deploying-banner"]');
}

beforeEach(() => {
  setActivePinia(createPinia());
  detail = null;
  vi.useFakeTimers();
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("service detail follows a background deploy", () => {
  it("leaves a creating service that was never deployed alone", async () => {
    await setup(baseService, []);
    const getService = vi.mocked(servicesApi.getService);
    expect(detail?.isDeploying.value).toBe(false);
    expect(detail?.controlHint.value).toBeNull();
    expect(banner()?.exists()).toBe(false);

    await vi.advanceTimersByTimeAsync(20_000);
    await flushPromises();
    expect(getService).toHaveBeenCalledTimes(1);
  });

  it("polls while deploying and stops once the deploy is terminal", async () => {
    await setup(baseService, [deploy("deploying")]);
    const getService = vi.mocked(servicesApi.getService);
    expect(detail?.isDeploying.value).toBe(true);
    expect(detail?.controlHint.value).not.toBeNull();
    expect(banner()?.exists()).toBe(true);

    await vi.advanceTimersByTimeAsync(5_000);
    await flushPromises();
    expect(getService).toHaveBeenCalledTimes(2);

    getService.mockResolvedValue({ ...baseService, status: "running" });
    vi.mocked(servicesApi.listServiceDeploys).mockResolvedValue([
      deploy("running"),
    ]);
    await vi.advanceTimersByTimeAsync(5_000);
    await flushPromises();
    expect(detail?.isDeploying.value).toBe(false);
    expect(detail?.service.value?.status).toBe("running");
    expect(banner()?.exists()).toBe(false);

    await vi.advanceTimersByTimeAsync(15_000);
    await flushPromises();
    expect(getService).toHaveBeenCalledTimes(3);
  });

  it("exposes a failed deploy and its redacted error", async () => {
    await setup({ ...baseService, status: "error" }, [
      deploy("failed", "services: validation: <redacted>"),
    ]);
    expect(detail?.isDeploying.value).toBe(false);
    expect(detail?.failedDeploy.value?.error).toBe(
      "services: validation: <redacted>",
    );
    expect(banner()?.exists()).toBe(false);
  });
});
