import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { nextTick } from "vue";
import type { Component } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ApplicationDetailPage from "../src/features/applications/pages/ApplicationDetailPage.vue";
import DatabaseDetailPage from "../src/features/databases/pages/DatabaseDetailPage.vue";
import ServiceDetailPage from "../src/features/services/pages/ServiceDetailPage.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const state = vi.hoisted(() => ({
  canWrite: true,
  serviceBusy: null as string | null,
  application: {
    id: "app-abcdef-1234",
    name: "checkout",
    project_id: "proj-1",
    project_name: "Acme",
    environment_id: "env-1",
    environment_name: "production",
    source_type: "git_public",
    server_id: "srv-1",
    pending_changes: [] as string[],
  },
  database: {
    id: "db-1",
    name: "pg-orders",
    project_id: "proj-1",
    project_name: "Acme",
    environment_id: "env-1",
    environment_name: "production",
    server_id: "srv-1",
    status: "running",
    volume: "gotham-db-1-data",
  },
  service: {
    id: "svc-1",
    name: "blog-staging",
    project_id: "proj-1",
    project_name: "Acme",
    environment_id: "env-1",
    environment_name: "production",
    server_id: "srv-1",
    status: "running",
    compose_project: "gotham-svc-1",
    domains: [] as unknown[],
  },
}));


vi.mock("@/features/applications/composables/useApplicationDetail", async () => {
  const { computed, ref } = await import("vue");
  return {
    useApplicationDetail: () => ({
      appsStore: { loading: false, error: null, acting: false },
      shortId: computed(() => "app-abcdef"),
      appId: computed(() => state.application.id),
      application: computed(() => state.application),
      displayName: computed(() => state.application.name),
      brand: computed(() => ({ logo: "git", letters: "CH", color: "#f05032" })),
      latest: computed(() => null),
      containerStopped: computed(() => false),
      active: computed(() => null),
      runningDeployments: computed(() => []),
      controlHint: computed(() => null),
      containerIsRunning: computed(() => false),
      deployments: computed(() => []),
      previews: computed(() => []),
      previewsLoaded: computed(() => false),
      previewsAvailable: computed(() => false),
      pipelineSteps: computed(() => []),
      descColumns: computed(() => 2),
      monitorSaving: ref(false),
      monitorError: ref(null),
      canWrite: computed(() => state.canWrite),
      moveSaving: ref(false),
      moveError: ref(null),
      handleMove: vi.fn(),
      deleteOpen: ref(false),
      deleting: ref(false),
      handleDeploy: vi.fn(),
      handleStop: vi.fn(),
      handleStart: vi.fn(),
      handleDelete: vi.fn(),
      handleMonitoringSave: vi.fn(),
      showLogsFor: vi.fn(),
      openRollback: vi.fn(),
      openRollbackFor: vi.fn(),
      handleRollback: vi.fn(),
      rollbackOpen: ref(false),
      rollingBack: ref(false),
      rollbackTarget: ref(null),
      activeTab: ref("overview"),
    }),
  };
});

vi.mock("@/features/applications/composables/useApplicationTerminalAccess", async () => {
  const { computed } = await import("vue");
  return {
    useApplicationTerminalAccess: () => ({
      accessKnown: computed(() => true),
      accessError: computed(() => null),
      retryAccess: vi.fn(),
      canWrite: computed(() => true),
      showTab: computed(() => false),
      blocked: computed(() => false),
      resolveTab: (tab: string) => tab,
    }),
  };
});

vi.mock("@/features/databases/composables/useDatabaseDetail", async () => {
  const { computed, ref } = await import("vue");
  return {
    databaseDetailKey: Symbol("database-detail"),
    useDatabaseDetail: () => ({
      databasesStore: { acting: false },
      dbId: computed(() => state.database.id),
      shortId: computed(() => state.database.id.slice(0, 8)),
      database: computed(() => state.database),
      pageLoading: ref(false),
      pageError: ref(null),
      canWrite: computed(() => state.canWrite),
      isCreating: computed(() => state.database.status === "creating"),
      controlHint: computed(() => null),
      moveSaving: ref(false),
      moveError: ref(null),
      handleMove: vi.fn(),
      handleDelete: vi.fn(),
      openRename: vi.fn(),
      monitorSaving: ref(false),
      monitorError: ref(null),
      handleMonitoringSave: vi.fn(),
    }),
  };
});

vi.mock("@/features/services/composables/useServiceDetail", async () => {
  const { computed, ref } = await import("vue");
  return {
    serviceDetailKey: Symbol("service-detail"),
    useServiceDetailContext: () => {
      throw new Error("service detail context is stubbed in this test");
    },
    useServiceDetail: () => ({
      service: computed(() => state.service),
      serviceId: computed(() => state.service.id),
      loading: ref(false),
      serverName: computed(() => "node-1"),
      error: ref(null),
      notFound: ref(false),
      actionError: ref(null),
      busy: computed(() => state.serviceBusy),
      canWrite: computed(() => state.canWrite),
      moveSaving: ref(false),
      moveError: ref(null),
      loggableServices: computed(() => []),
      deploys: computed(() => []),
      isDeploying: computed(() => false),
      failedDeploy: computed(() => null),
      handleMove: vi.fn(),
      handleDelete: vi.fn(),
      handleDeploy: vi.fn(),
      handleRestart: vi.fn(),
      handleStop: vi.fn(),
    }),
  };
});

beforeEach(() => {
  state.canWrite = true;
  state.serviceBusy = null;
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

function pageRouter() {
  const stub = { template: "<div />" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "projects", component: stub },
      { path: "/projects/:projectId", name: "project-detail", component: stub },
      {
        path: "/projects/:projectId/environments/:environmentId",
        name: "environment-detail",
        component: stub,
      },
    ],
  });
}

const sharedStubs = {
  ServerPicker: true,
  ResourceScopeSummary: true,
};

async function mountPage(
  component: Component,
  stubs: Record<string, boolean>,
): Promise<VueWrapper> {
  const router = pageRouter();
  await router.push("/projects/proj-1/environments/env-1");
  await router.isReady();
  const wrapper = mount(component, {
    global: {
      plugins: [router, i18n],
      stubs: { ...sharedStubs, ...stubs },
    },
  });
  await flushPromises();
  await nextTick();
  return wrapper;
}

function settingsTab(wrapper: VueWrapper) {
  return wrapper.findAll(".n-tabs-tab").find((node) => node.text() === "Settings");
}

function hasDeleteButton(wrapper: VueWrapper): boolean {
  return wrapper
    .findAll("button")
    .some((node) => node.text().trim().startsWith("Delete"));
}

interface PageCase {
  name: string;
  component: Component;
  stubs: Record<string, boolean>;
}

const pages: PageCase[] = [
  {
    name: "application",
    component: ApplicationDetailPage,
    stubs: {
      ApplicationHeader: true,
      ApplicationOverviewTab: true,
      ApplicationImagePanel: true,
      ApplicationDeploymentsTab: true,
      ApplicationLogsTab: true,
      ApplicationEnvTab: true,
      ApplicationStorageTab: true,
      ApplicationPreviewsTab: true,
      ApplicationTerminalTab: true,
      ApplicationDeleteDialog: true,
      DomainEditor: true,
      RollbackDialog: true,
      ResourceHistoryPanel: true,
    },
  },
  {
    name: "database",
    component: DatabaseDetailPage,
    stubs: {
      DatabaseDetailHeader: true,
      DatabaseOverviewPanel: true,
      ResourceBackupsTab: true,
      RenameDatabaseDialog: true,
      BackupRestoreDialog: true,
      ResourceHistoryPanel: true,
    },
  },
  {
    name: "service",
    component: ServiceDetailPage,
    stubs: {
      ServiceDetailHeader: true,
      ServiceOverviewCard: true,
      ServiceDeployHistoryCard: true,
      ServiceDeployBanner: true,
      ServiceComposeCard: true,
      ServiceEnvCard: true,
      ServiceContainersCard: true,
      ServiceLogs: true,
    },
  },
];

describe.each(pages)("$name detail permissions", ({ component, stubs }) => {
  it("hides the Settings tab, Delete and the move card for a read-only member", async () => {
    state.canWrite = false;
    const wrapper = await mountPage(component, stubs);

    expect(settingsTab(wrapper), "no Settings tab").toBeUndefined();
    expect(wrapper.text(), "no move card").not.toContain("Server and environment");
    expect(hasDeleteButton(wrapper), "no Delete action").toBe(false);
  });

  it("shows the Settings tab with the move card and Delete for a writer", async () => {
    state.canWrite = true;
    const wrapper = await mountPage(component, stubs);

    const settings = settingsTab(wrapper);
    expect(settings, "Settings tab").toBeDefined();
    await settings!.trigger("click");
    await flushPromises();
    await nextTick();

    expect(wrapper.text(), "move card").toContain("Server and environment");
    expect(hasDeleteButton(wrapper), "Delete action").toBe(true);
  });
});

describe("service settings danger zone", () => {
  const stubs = pages.find((page) => page.name === "service")!.stubs;

  it("disables Delete while another service action runs", async () => {
    state.serviceBusy = "deploy";
    const wrapper = await mountPage(ServiceDetailPage, stubs);
    await settingsTab(wrapper)!.trigger("click");
    await flushPromises();
    await nextTick();

    const del = wrapper
      .findAll("button")
      .find((node) => node.text().trim() === "Delete")!;
    expect(del.attributes("disabled")).not.toBeUndefined();
  });
});
