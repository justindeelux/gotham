import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import DashboardPage from "@/features/dashboard/pages/DashboardPage.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const state = vi.hoisted(() => ({
  isOwner: true,
  servers: [] as Array<Record<string, unknown>>,
  applications: [] as Array<Record<string, unknown>>,
  applicationsFail: false,
  incidentCount: 0,
}));

function server(): Record<string, unknown> {
  return {
    id: "srv-1",
    name: "web-1",
    ip: "10.0.0.1",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: null,
    os: "Ubuntu 24.04",
    docker_version: "27.0.0",
    arch: "amd64",
    total_mem: 8_000_000_000,
    total_disk: 100_000_000_000,
    cpu_usage: 0.1,
    mem_usage: 0.2,
    disk_usage: 0.3,
    container_count: 2,
    last_seen: "2026-10-31T00:00:00Z",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-31T00:00:00Z",
  };
}

vi.mock("@/features/auth", () => ({
  useAuthStore: () => ({
    get isOwner() {
      return state.isOwner;
    },
  }),
}));

vi.mock("@/features/servers", () => ({
  useServersStore: () => ({
    get servers() {
      return state.servers;
    },
    loading: false,
    error: null,
    fetchServers: async () => {},
    pollServers: () => {},
  }),
}));

vi.mock("@/features/incidents/stores/incidents", () => ({
  useIncidentsStore: () => ({
    state: "ready",
    get activeCount() {
      return state.incidentCount;
    },
  }),
}));

vi.mock("@/features/applications", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/applications")>();
  return {
    ...actual,
    listApplications: async () => {
      if (state.applicationsFail) {
        throw new Error("applications unavailable");
      }
      return state.applications;
    },
    latestDeploymentStates: async () => ({ states: [], failed: 0 }),
  };
});

vi.mock("@/features/dashboard/components/DashboardHeader.vue", () => ({
  default: {
    props: ["showAddServer"],
    template:
      '<div data-test="header"><button v-if="showAddServer">Add server</button></div>',
  },
}));
vi.mock("@/features/dashboard/components/RecentDeploysPanel.vue", () => ({
  default: { template: '<div data-test="deploys" />' },
}));
vi.mock("@/features/dashboard/components/DashboardProjectsSection.vue", () => ({
  default: { template: '<div data-test="projects" />' },
}));
vi.mock("@/features/dashboard/components/ServerHealthSection.vue", () => ({
  default: { template: '<div data-test="health" />' },
}));
vi.mock("@/features/incidents/components/NeedsAttentionSection.vue", () => ({
  default: { template: '<div data-test="attention" />' },
}));
vi.mock("@/shared/ui/GothamIcon.vue", () => ({
  default: { template: "<span />" },
}));

function mountPage() {
  return mount(DashboardPage, {
    global: {
      plugins: [createPinia(), i18n],
      stubs: { RouterLink: { template: "<a><slot /></a>" } },
    },
  });
}

function markerOrder(html: string, markers: string[]): number[] {
  return markers.map((marker) => html.indexOf(`data-test="${marker}"`));
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  state.isOwner = true;
  state.servers = [server()];
  state.applications = [];
  state.applicationsFail = false;
  state.incidentCount = 0;
});

describe("DashboardPage order", () => {
  it("puts status, attention, projects, server health and deploys in that order for the owner", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const [status, attention, projects, health, deploys] = markerOrder(wrapper.html(), [
      "status",
      "attention",
      "projects",
      "health",
      "deploys",
    ]);
    expect(status, "status strip comes first").toBeGreaterThanOrEqual(0);
    expect(attention).toBeGreaterThan(status);
    expect(projects).toBeGreaterThan(attention);
    expect(health).toBeGreaterThan(projects);
    expect(deploys).toBeGreaterThan(health);
    wrapper.unmount();
  });

  it("keeps the status strip, projects and deploys but drops owner-only sections for non-owners", async () => {
    state.isOwner = false;
    const wrapper = mountPage();
    await flushPromises();

    const html = wrapper.html();
    expect(html).toContain('data-test="status"');
    expect(html).toContain('data-test="projects"');
    expect(html).toContain('data-test="deploys"');
    expect(html).not.toContain('data-test="health"');
    expect(html).not.toContain('data-testid="dashboard-servers-figure"');
    wrapper.unmount();
  });

  it("shows one first-run state instead of empty cards", async () => {
    state.servers = [];
    const wrapper = mountPage();
    await flushPromises();

    const html = wrapper.html();
    expect(html).toContain('data-testid="dashboard-first-run"');
    expect(html).not.toContain('data-test="status"');
    expect(html).not.toContain('data-test="attention"');
    expect(html).not.toContain('data-test="projects"');
    expect(html).not.toContain('data-test="health"');
    expect(html).not.toContain('data-test="deploys"');
    expect(wrapper.text()).toContain("Add your first server");
    wrapper.unmount();
  });

  it("keeps one server CTA when the owner has apps but no servers", async () => {
    state.servers = [];
    state.applications = [{ id: "app-1" }];
    const wrapper = mountPage();
    await flushPromises();

    const html = wrapper.html();
    expect(html).not.toContain('data-testid="dashboard-first-run"');
    expect(html).not.toContain('data-test="health"');
    expect(wrapper.get('[data-testid="dashboard-status"]').text()).toBe(
      "No servers yet",
    );
    const addButtons = wrapper
      .findAll("button")
      .filter((button) => button.text() === "Add server");
    expect(addButtons, "one server CTA surface").toHaveLength(1);
    wrapper.unmount();
  });

  it("keeps one server CTA when the applications request fails", async () => {
    state.servers = [];
    state.applicationsFail = true;
    const wrapper = mountPage();
    await flushPromises();

    const html = wrapper.html();
    expect(html).not.toContain('data-testid="dashboard-first-run"');
    expect(html).not.toContain('data-test="health"');
    expect(wrapper.get('[data-testid="dashboard-status"]').text()).toBe(
      "No servers yet",
    );
    const addButtons = wrapper
      .findAll("button")
      .filter((button) => button.text() === "Add server");
    expect(addButtons, "one server CTA surface").toHaveLength(1);
    wrapper.unmount();
  });

  it("derives the status sentence from incidents and server status", async () => {
    state.incidentCount = 1;
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.get('[data-testid="dashboard-status"]').text()).toBe(
      "1 thing needs attention",
    );
    wrapper.unmount();
  });
});
