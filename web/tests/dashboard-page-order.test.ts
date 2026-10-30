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

const state = vi.hoisted(() => ({ isOwner: true }));

vi.mock("@/features/auth", () => ({
  useAuthStore: () => ({
    get isOwner() {
      return state.isOwner;
    },
  }),
}));

vi.mock("@/features/servers", () => ({
  useServersStore: () => ({
    servers: [],
    loading: false,
    error: null,
    fetchServers: async () => {},
    pollServers: () => {},
  }),
}));

vi.mock("@/features/applications", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/applications")>();
  return {
    ...actual,
    listApplications: async () => [],
    latestDeploymentStates: async () => ({ states: [], failed: 0 }),
  };
});

vi.mock("@/features/dashboard/components/DashboardHeader.vue", () => ({
  default: { template: '<div data-test="header" />' },
}));
vi.mock("@/features/dashboard/components/DashboardAside.vue", () => ({
  default: { template: '<div data-test="aside" />' },
}));
vi.mock("@/features/dashboard/components/RecentDeploysPanel.vue", () => ({
  default: { template: '<div data-test="deploys" />' },
}));
vi.mock("@/features/dashboard/components/ServerHealthSection.vue", () => ({
  default: { template: '<div data-test="health" />' },
}));
vi.mock("@/features/dashboard/components/ServersReadyKpi.vue", () => ({
  default: { template: '<div data-test="kpi" />' },
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
});

describe("DashboardPage order", () => {
  it("puts server health above recent deploys for the owner", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const [attention, health, deploys] = markerOrder(wrapper.html(), [
      "attention",
      "health",
      "deploys",
    ]);
    expect(attention, "needs attention stays first").toBeGreaterThanOrEqual(0);
    expect(health).toBeGreaterThan(attention);
    expect(deploys).toBeGreaterThan(health);
    wrapper.unmount();
  });

  it("keeps recent deploys and drops server health for non-owners", async () => {
    state.isOwner = false;
    const wrapper = mountPage();
    await flushPromises();

    const html = wrapper.html();
    expect(html).not.toContain('data-test="health"');
    expect(html).not.toContain('data-test="aside"');
    expect(html).toContain('data-test="deploys"');
    wrapper.unmount();
  });
});
