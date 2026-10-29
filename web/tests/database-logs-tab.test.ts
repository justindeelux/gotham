import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import DatabaseDetailPage from "../src/features/databases/pages/DatabaseDetailPage.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const state = vi.hoisted(() => ({
  database: {
    id: "db-1",
    name: "pg-orders",
    project_id: "proj-1",
    project_name: "Acme",
    environment_id: "env-1",
    environment_name: "production",
    server_id: "srv-1",
    server_name: "node-1",
    status: "running",
    container_id: "ctr-1",
    volume: "gotham-db-1-data",
  },
}));

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
      canWrite: computed(() => true),
      isCreating: computed(() => state.database.status === "creating"),
      controlHint: computed(() => null),
      moveSaving: ref(false),
      moveError: ref(null),
      handleMove: vi.fn(),
      handleDelete: vi.fn(),
      openRename: vi.fn(),
    }),
  };
});

beforeEach(() => {
  state.database = {
    id: "db-1",
    name: "pg-orders",
    project_id: "proj-1",
    project_name: "Acme",
    environment_id: "env-1",
    environment_name: "production",
    server_id: "srv-1",
    server_name: "node-1",
    status: "running",
    container_id: "ctr-1",
    volume: "gotham-db-1-data",
  };
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

async function mountPage() {
  const stub = { template: "<div />" };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "projects", component: stub },
      { path: "/projects/:projectId", name: "project-detail", component: stub },
      {
        path: "/projects/:projectId/environments/:environmentId",
        name: "environment-detail",
        component: stub,
      },
      {
        path: "/projects/:projectId/environments/:environmentId/databases/:id",
        name: "database-detail",
        component: stub,
      },
    ],
  });
  await router.push("/projects/proj-1/environments/env-1?tab=logs");
  await router.isReady();
  const wrapper = mount(DatabaseDetailPage, {
    global: {
      plugins: [router, i18n],
      stubs: {
        DatabaseDetailHeader: true,
        DatabaseOverviewPanel: true,
        ResourceBackupsTab: true,
        RenameDatabaseDialog: true,
        BackupRestoreDialog: true,
        ResourceHistoryPanel: true,
        LogViewer: true,
      },
    },
  });
  await flushPromises();
  return wrapper;
}

async function openLogs(wrapper: Awaited<ReturnType<typeof mountPage>>) {
  const tab = wrapper
    .findAll(".n-tabs-tab")
    .find((node) => node.text() === "Logs");
  expect(tab, "database Logs tab exists").toBeDefined();
  await tab!.trigger("click");
  await flushPromises();
}

describe("database detail Logs tab", () => {
  it("sits between Backups and Settings", async () => {
    const wrapper = await mountPage();
    const tabs = wrapper.findAll(".n-tabs-tab").map((node) => node.text());
    expect(tabs.indexOf("Logs")).toBe(tabs.indexOf("Backups") + 1);
    expect(tabs.indexOf("Settings")).toBe(tabs.indexOf("Logs") + 1);
  });

  it("shows an empty state instead of a viewer when there is no container", async () => {
    state.database = { ...state.database, container_id: "" };
    const wrapper = await mountPage();
    await openLogs(wrapper);

    expect(wrapper.text()).toContain("No logs yet");
    expect(wrapper.find("log-viewer-stub").exists()).toBe(false);
  });

  it("streams the container logs", async () => {
    const wrapper = await mountPage();
    await openLogs(wrapper);

    expect(wrapper.find("log-viewer-stub").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("The container is stopped");
  });

  it("keeps the last lines and explains a stopped container", async () => {
    state.database = { ...state.database, status: "stopped" };
    const wrapper = await mountPage();
    await openLogs(wrapper);

    expect(wrapper.find("log-viewer-stub").exists()).toBe(true);
    expect(wrapper.text()).toContain("The container is stopped");
  });
});
