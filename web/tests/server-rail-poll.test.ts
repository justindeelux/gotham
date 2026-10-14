// JUS-135: below the tablet breakpoint the ServerRail moves into a drawer and
// unmounts on close, so it must never own (or stop) the shared server poll —
// the app shell does. This pins that contract.
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Server } from "../src/features/servers";
import { useServersStore } from "../src/features/servers/stores/servers";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

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
  } as Server;
}

function railRouter(): ReturnType<typeof createRouter> {
  const stub = { template: "<div />" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/dashboard", name: "dashboard", component: stub },
      { path: "/servers", name: "servers", component: stub },
      { path: "/servers/:id", name: "server-detail", component: stub },
    ],
  });
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("ServerRail poll ownership (JUS-135)", () => {
  it("never starts the shared poll and never stops it on unmount", async () => {
    setActivePinia(createPinia());
    const servers = useServersStore();
    servers.servers = [server()];
    const poll = vi.spyOn(servers, "pollServers").mockImplementation(() => {});
    const stop = vi.spyOn(servers, "stopPolling").mockImplementation(() => {});
    const fetch = vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);

    const router = railRouter();
    await router.push("/dashboard");
    await router.isReady();

    const wrapper = mount(ServerRail, { global: { plugins: [router, i18n] } });
    await flushPromises();

    // The shell owns the interval; the rail only seeds the list when empty.
    expect(poll).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();

    wrapper.unmount();
    expect(stop).not.toHaveBeenCalled();
  });
});
