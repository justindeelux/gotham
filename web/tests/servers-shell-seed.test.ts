// JUS-143: the shell seeds the server list when arming the poll so the
// sidebar count and the rail are correct on first paint, on any page — even a
// mobile cold load where neither the rail mounts nor the page fetches servers.
// The seed skips when a load is already in flight, so it never double fetches
// with the rail or a page seed; the store's listSync ordering keeps any
// overlapping loads correct.
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NMessageProvider } from "naive-ui";
import { defineComponent, h } from "vue";

import type { Server } from "../src/features/servers";
import { useServersStore } from "../src/features/servers/stores/servers";
import { listServers } from "../src/features/servers/api/servers";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

vi.mock("../src/features/version", async (importOriginal) => {
  const original = await importOriginal<typeof import("../src/features/version")>();
  return { ...original, getVersion: vi.fn().mockResolvedValue("v9.9.9-test") };
});

vi.mock("../src/features/servers/api/servers", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../src/features/servers/api/servers")>()),
  listServers: vi.fn(),
}));

if (!globalThis.window.matchMedia) {
  globalThis.window.matchMedia = (() => ({
    matches: true,
    media: "",
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof globalThis.window.matchMedia;
}

const { default: AppLayout } = await import("../src/app/layouts/AppLayout.vue");
const { default: ServerRail } = await import("../src/features/servers/components/ServerRail.vue");

function server(): Server {
  return {
    id: "srv-1",
    name: "alpha",
    ip: "10.0.0.2",
    port: 9442,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: null,
    os: null,
    docker_version: null,
    arch: null,
    total_mem: null,
    total_disk: null,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: null,
    last_seen: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function shellRouter(): ReturnType<typeof createRouter> {
  const stub = { template: "<div />" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/",
        component: stub,
        children: [
          { path: "dashboard", name: "dashboard", component: stub },
          { path: "servers", name: "servers", component: stub },
          { path: "projects", name: "projects", component: stub },
          { path: "templates", name: "templates", component: stub },
          { path: "toolchain", name: "toolchain", component: stub },
          { path: "domains", name: "domains", component: stub },
          { path: "teams", name: "teams", component: stub },
          { path: "notifications", name: "notifications", component: stub },
          { path: "git-sources", name: "git-sources", component: stub },
          { path: "files", name: "files", component: stub },
          { path: "instance", name: "instance", component: stub },
          { path: "updates", name: "updates", component: stub },
          { path: "servers/:id", name: "server-detail", component: stub },
        ],
      },
    ],
  });
}

async function readyRouter(): Promise<ReturnType<typeof createRouter>> {
  const router = shellRouter();
  await router.push("/dashboard");
  await router.isReady();
  return router;
}

async function mountShell(): Promise<ReturnType<typeof mount>> {
  const router = await readyRouter();
  // The app shell hosts the global editor window (JUS-232), which needs
  // the message provider production supplies in App.vue.
  const shell = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(AppLayout),
      }),
  });
  const wrapper = mount(shell, {
    attachTo: globalThis.document.body,
    global: { plugins: [router, i18n], stubs: { transition: false } },
  });
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
  vi.clearAllMocks();
});

describe("shell server seed (JUS-143)", () => {
  it("seeds exactly one fetch and shows the count on first paint", async () => {
    vi.mocked(listServers).mockResolvedValue([server()]);

    const layout = await mountShell();

    const servers = useServersStore();
    expect(vi.mocked(listServers)).toHaveBeenCalledTimes(1);
    expect(servers.servers).toHaveLength(1);
    expect(layout.find(".nav-count").text()).toBe("1");
    layout.unmount();
  });

  it("skips the seed when a page's load is already in flight (no double fetch)", async () => {
    // A page mounts before the shell; it has already started a load, so the
    // shell's seed must not fire a second request.
    useServersStore().loading = true;

    const layout = await mountShell();

    expect(vi.mocked(listServers)).not.toHaveBeenCalled();
    layout.unmount();
  });

  it("arms the shared poll once while seeding", async () => {
    const servers = useServersStore();
    const poll = vi.spyOn(servers, "pollServers").mockImplementation(() => {});
    vi.mocked(listServers).mockResolvedValue([]);

    const layout = await mountShell();

    expect(poll).toHaveBeenCalledTimes(1);
    layout.unmount();
  });
});

describe("rail seed does not double fetch with the shell (JUS-143)", () => {
  async function mountRail(): Promise<ReturnType<typeof mount>> {
    const router = await readyRouter();
    const wrapper = mount(ServerRail, { global: { plugins: [router, i18n] } });
    await flushPromises();
    return wrapper;
  }

  it("seeds when the list is empty and idle", async () => {
    vi.mocked(listServers).mockResolvedValue([server()]);
    const rail = await mountRail();
    expect(vi.mocked(listServers)).toHaveBeenCalledTimes(1);
    rail.unmount();
  });

  it("does not seed while another load is in flight", async () => {
    useServersStore().loading = true;
    const rail = await mountRail();
    expect(vi.mocked(listServers)).not.toHaveBeenCalled();
    rail.unmount();
  });
});
