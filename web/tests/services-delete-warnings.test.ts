import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";

import * as servicesApi from "../src/features/services/api/services";
import type { Service } from "../src/features/services/api/services";
import { useServiceDetail } from "../src/features/services/composables/useServiceDetail";
import { useServersStore } from "@/features/servers/stores/servers";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const service: Service = {
  id: "svc-1",
  name: "blog-staging",
  status: "running",
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

let detail: ReturnType<typeof useServiceDetail> | null = null;
let wrapper: ReturnType<typeof mount> | null = null;

const Harness = defineComponent({
  setup() {
    detail = useServiceDetail();
    return () => h("div");
  },
});

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  detail = null;
  vi.spyOn(servicesApi, "getService").mockResolvedValue(service);
  vi.spyOn(servicesApi, "listServiceDeploys").mockResolvedValue([]);
  vi.spyOn(useServersStore(), "fetchServers").mockResolvedValue(undefined);
  globalThis.document.body.innerHTML = "";
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  vi.restoreAllMocks();
});

async function setup(): Promise<void> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/projects/:projectId/environments/:environmentId/services/:id",
        name: "service-detail",
        component: { template: "<div />" },
      },
      {
        path: "/projects/:projectId/environments/:environmentId",
        name: "environment-detail",
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

async function deleteService(): Promise<string> {
  await detail?.handleDelete();
  await flushPromises();
  return globalThis.document.body.textContent ?? "";
}

describe("service delete reports kept volumes", () => {
  it("warns when a data volume was kept", async () => {
    vi.spyOn(servicesApi, "deleteService").mockResolvedValue([
      "kept volume shared-data: it has a custom name",
    ]);
    await setup();

    const text = await deleteService();

    expect(text).toContain(
      "Service deleted, but some data volumes were kept: kept volume shared-data: it has a custom name",
    );
    expect(text).not.toContain("Its data volumes are removed.");
  });

  it("still claims a clean delete when no volume was kept", async () => {
    vi.spyOn(servicesApi, "deleteService").mockResolvedValue([]);
    await setup();

    const text = await deleteService();

    expect(text).toContain("Service deleted. Its data volumes are removed.");
  });
});
