import { NButton } from "naive-ui";
import { computed, ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ApplicationHeader from "../src/features/applications/components/ApplicationHeader.vue";
import DatabaseDetailHeader from "../src/features/databases/components/DatabaseDetailHeader.vue";
import { databaseDetailKey } from "../src/features/databases/composables/useDatabaseDetail";
import ServerDetailHeader from "../src/features/servers/components/ServerDetailHeader.vue";
import ServiceDetailHeader from "../src/features/services/components/ServiceDetailHeader.vue";
import { serviceDetailKey } from "../src/features/services/composables/useServiceDetail";
import PageHeader from "../src/shared/ui/PageHeader.vue";
import type { Deployment } from "../src/features/applications/api/applications";
import type { Database } from "../src/features/databases/api/databases";
import type { Server } from "../src/features/servers/api/servers";
import type { Service } from "../src/features/services/api/services";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const longName = "checkout-service-with-a-very-long-resource-name-for-wrapping";

const running = { id: "deploy-1", state: "running" } as Deployment;

const server = {
  id: "srv-1",
  name: longName,
  ip: "10.0.0.5",
  port: 22,
  ssh_user: "root",
  ssh_key_id: null,
  has_password: true,
  status: "ready",
  node_id: "localhost",
  os: "Ubuntu 22.04",
  docker_version: "27.0.3",
  arch: "amd64",
  total_mem: null,
  total_disk: null,
  cpu_usage: null,
  mem_usage: null,
  disk_usage: null,
  container_count: 0,
  last_seen: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
} as Server;

const database = {
  id: "db-1",
  name: longName,
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "Acme",
  engine: "postgres",
  version: "16",
  status: "running",
  server_id: "srv-1",
  server_name: "gotham-prod-01",
  container_id: "ctr-1",
  public_port: 15432,
  volume: "gotham-db-1-data",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
} as Database;

const service = {
  id: "svc-1",
  name: longName,
  status: "running",
  server_id: "srv-1",
  server_name: "gotham-prod-01",
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "Acme",
  compose_project: "acme-checkout",
  env: {},
  domains: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
} as Service;

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

function headerRouter() {
  const stub = { template: "<div />" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "projects", component: stub },
      { path: "/servers", name: "servers", component: stub },
      { path: "/servers/:id", name: "server-detail", component: stub },
      { path: "/servers/:id/containers", name: "server-containers", component: stub },
      {
        path: "/projects/:projectId/environments/:environmentId",
        name: "environment-detail",
        component: stub,
      },
    ],
  });
}

function buttonByText(wrapper: ReturnType<typeof mount>, label: string) {
  const button = wrapper
    .findAll("button")
    .find((node) => node.text().trim() === label);
  if (!button) {
    throw new Error(`button ${label} not found`);
  }
  return button;
}

describe("PageHeader", () => {
  it("renders one h1 with every slot", () => {
    const wrapper = mount(PageHeader, {
      slots: {
        identity: "<span class='avatar'>AV</span>",
        breadcrumb: "<nav class='crumb'>Home</nav>",
        title: longName,
        badges: "<span class='badge'>ready</span>",
        meta: "10.0.0.5:22",
        description: "Human description",
        actions: "<button class='action'>Refresh</button>",
      },
    });

    expect(wrapper.findAll("h1")).toHaveLength(1);
    expect(wrapper.find("h1").text()).toBe(longName);
    expect(wrapper.find(".page-head__meta").text()).toBe("10.0.0.5:22");
    expect(wrapper.find(".page-desc").text()).toBe("Human description");
    expect(wrapper.find(".page-actions .action").exists()).toBe(true);
    expect(wrapper.find(".page-head__breadcrumb .crumb").exists()).toBe(true);
    expect(wrapper.find(".page-head__identity .avatar").exists()).toBe(true);
  });

  it("omits the description when the slot is absent", () => {
    const wrapper = mount(PageHeader, { slots: { title: "Title" } });
    expect(wrapper.findAll("h1")).toHaveLength(1);
    expect(wrapper.find(".page-desc").exists()).toBe(false);
    expect(wrapper.find(".page-actions").exists()).toBe(false);
  });
});

describe("ServerDetailHeader", () => {
  async function mountHeader(overrides: Record<string, unknown> = {}) {
    const router = headerRouter();
    await router.push("/servers/srv-1");
    await router.isReady();
    return mount(ServerDetailHeader, {
      props: { server, validating: false, ...overrides },
      global: { plugins: [i18n, router] },
    });
  }

  it("renders a semantic title, status badge and technical meta line", async () => {
    const wrapper = await mountHeader();
    expect(wrapper.findAll("h1")).toHaveLength(1);
    expect(wrapper.find("h1").text()).toBe(longName);
    expect(wrapper.text()).toContain("Ready");
    expect(wrapper.find(".page-head__meta").text()).toContain("10.0.0.5:22");
    expect(wrapper.find(".page-desc").exists()).toBe(false);
  });

  it("keeps edit, validate and containers actions working", async () => {
    const wrapper = await mountHeader();
    await buttonByText(wrapper, "Edit").trigger("click");
    await buttonByText(wrapper, "Validate").trigger("click");
    expect(wrapper.emitted("edit")).toHaveLength(1);
    expect(wrapper.emitted("validate")).toHaveLength(1);
    expect(wrapper.text()).toContain("Open containers");
  });

  it("keeps the validate button loading while a validation runs", async () => {
    const wrapper = await mountHeader({ validating: true });
    const validate = wrapper
      .findAllComponents(NButton)
      .find((button) => button.text() === "Validate");
    expect(validate?.props("loading")).toBe(true);
  });
});

describe("ApplicationHeader", () => {
  function mountHeader(overrides: Record<string, unknown> = {}) {
    return mount(ApplicationHeader, {
      props: {
        displayName: longName,
        appId: "app-abcdef-1234",
        shortId: "app-abcd",
        brand: { logo: "github", letters: "AP", color: "#6e7681" },
        latest: running,
        containerStopped: false,
        activeDeploying: false,
        canRollback: true,
        acting: false,
        controlHint: null,
        containerIsRunning: true,
        ...overrides,
      },
      global: { plugins: [i18n] },
    });
  }

  it("renders the long name as the h1 and the id as mono meta", () => {
    const wrapper = mountHeader();
    expect(wrapper.findAll("h1")).toHaveLength(1);
    expect(wrapper.find("h1").text()).toBe(longName);
    expect(wrapper.find(".page-head__meta").text()).toBe("app-abcdef-1234");
    expect(wrapper.find(".page-desc").exists()).toBe(false);
  });

  it("keeps the running lifecycle states", () => {
    const wrapper = mountHeader();
    expect(buttonByText(wrapper, "Redeploy").attributes("disabled")).toBeUndefined();
    expect(buttonByText(wrapper, "Rollback").attributes("disabled")).toBeUndefined();
    expect(buttonByText(wrapper, "Stop").attributes("disabled")).toBeUndefined();
    expect(buttonByText(wrapper, "Start").attributes("disabled")).not.toBeUndefined();
  });

  it("keeps the read-only lifecycle states", () => {
    const wrapper = mountHeader({
      acting: true,
      canRollback: false,
      containerIsRunning: false,
    });
    expect(buttonByText(wrapper, "Rollback").attributes("disabled")).not.toBeUndefined();
    expect(buttonByText(wrapper, "Stop").attributes("disabled")).not.toBeUndefined();
    expect(buttonByText(wrapper, "Start").attributes("disabled")).not.toBeUndefined();
  });

  it("still emits lifecycle events", async () => {
    const wrapper = mountHeader();
    await buttonByText(wrapper, "Redeploy").trigger("click");
    await buttonByText(wrapper, "Rollback").trigger("click");
    await buttonByText(wrapper, "Stop").trigger("click");
    expect(wrapper.emitted("deploy")).toHaveLength(1);
    expect(wrapper.emitted("rollback")).toHaveLength(1);
    expect(wrapper.emitted("stop")).toHaveLength(1);
  });
});

describe("DatabaseDetailHeader", () => {
  function mountHeader(status: Database["status"], acting = false) {
    const current = ref<Database | null>({ ...database, status });
    const detail = {
      database: current,
      dbId: computed(() => database.id),
      shortId: computed(() => database.id.slice(0, 8)),
      initials: computed(() => "CH"),
      canStart: computed(() => status === "stopped" || status === "error"),
      canStop: computed(() => status === "running"),
      canRestart: computed(() => status === "running" || status === "stopped"),
      databasesStore: { acting },
      handleLifecycle: vi.fn(),
      openRename: vi.fn(),
      handleDelete: vi.fn(),
    };
    return mount(DatabaseDetailHeader, {
      global: {
        plugins: [i18n],
        provide: { [databaseDetailKey as symbol]: detail },
      },
    });
  }

  it("renders the long name as the h1 and the id as mono meta", () => {
    const wrapper = mountHeader("running");
    expect(wrapper.findAll("h1")).toHaveLength(1);
    expect(wrapper.find("h1").text()).toBe(longName);
    expect(wrapper.find(".page-head__meta").text()).toBe("db-1");
    expect(wrapper.find(".page-desc").exists()).toBe(false);
  });

  it("keeps the running lifecycle states", () => {
    const wrapper = mountHeader("running");
    expect(buttonByText(wrapper, "Start").attributes("disabled")).not.toBeUndefined();
    expect(buttonByText(wrapper, "Stop").attributes("disabled")).toBeUndefined();
    expect(buttonByText(wrapper, "Restart").attributes("disabled")).toBeUndefined();
    expect(buttonByText(wrapper, "Rename").attributes("disabled")).toBeUndefined();
  });

  it("keeps the stopped lifecycle states", () => {
    const wrapper = mountHeader("stopped");
    expect(buttonByText(wrapper, "Start").attributes("disabled")).toBeUndefined();
    expect(buttonByText(wrapper, "Stop").attributes("disabled")).not.toBeUndefined();
  });
});

describe("ServiceDetailHeader", () => {
  async function mountHeader(busy: string | null = null) {
    const router = headerRouter();
    await router.push("/projects/proj-1/environments/env-1");
    await router.isReady();
    const detail = {
      service: computed(() => service),
      serviceId: computed(() => service.id),
      serverName: computed(() => service.server_name),
      busy: ref(busy),
      handleDeploy: vi.fn(),
      handleRestart: vi.fn(),
      handleStop: vi.fn(),
      handleDelete: vi.fn(),
    };
    return mount(ServiceDetailHeader, {
      global: {
        plugins: [i18n, router],
        provide: { [serviceDetailKey as symbol]: detail },
      },
    });
  }

  it("renders the long name as the h1 without a description", async () => {
    const wrapper = await mountHeader();
    expect(wrapper.findAll("h1")).toHaveLength(1);
    expect(wrapper.find("h1").text()).toBe(longName);
    expect(wrapper.find(".page-desc").exists()).toBe(false);
  });

  it("keeps the lifecycle states while idle", async () => {
    const wrapper = await mountHeader();
    for (const label of ["Deploy", "Restart", "Stop", "Delete"]) {
      expect(buttonByText(wrapper, label).attributes("disabled")).toBeUndefined();
    }
  });

  it("disables lifecycle controls while an action runs", async () => {
    const wrapper = await mountHeader("deploy");
    for (const label of ["Deploy", "Restart", "Stop", "Delete"]) {
      expect(buttonByText(wrapper, label).attributes("disabled")).not.toBeUndefined();
    }
  });
});
