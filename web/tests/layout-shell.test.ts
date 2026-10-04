// AppLayout shell: open-drawer DOM and remount parity (fast, deterministic).
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useAuthStore } from "../src/features/auth/stores/auth";
import { useServersStore } from "../src/features/servers/stores/servers";
import type { Server } from "../src/features/servers";

vi.mock("../src/features/version", async (importOriginal) => {
  const original = await importOriginal<typeof import("../src/features/version")>();
  return { ...original, getVersion: vi.fn().mockResolvedValue("v9.9.9-test") };
});

if (!globalThis.window.matchMedia) {
  globalThis.window.matchMedia = (() => ({
    matches: true,
    media: "",
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof globalThis.window.matchMedia;
}

const { default: AppLayout } = await import("../src/app/layouts/AppLayout.vue");

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

async function mountShell(): Promise<ReturnType<typeof mount>> {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.user = { email: "a@x.y" } as never;
  auth.accessToken = "tok";
  const servers = useServersStore();
  servers.servers = [server()];
  vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);
  vi.spyOn(servers, "pollServers").mockImplementation(() => {});
  const stub = { template: "<div />" };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/",
        component: stub,
        children: [
          { path: "dashboard", name: "dashboard", component: stub },
          { path: "servers", name: "servers", component: stub },
          { path: "applications", name: "applications", component: stub },
          { path: "services", name: "services", component: stub },
          { path: "databases", name: "databases", component: stub },
          { path: "templates", name: "templates", component: stub },
          { path: "domains", name: "domains", component: stub },
          { path: "teams", name: "teams", component: stub },
          { path: "notifications", name: "notifications", component: stub },
          { path: "servers/:id", name: "server-detail", component: stub },
        ],
      },
    ],
  });
  await router.push("/dashboard");
  await router.isReady();
  const wrapper = mount(AppLayout, {
    attachTo: globalThis.document.body,
    global: { plugins: [router], stubs: { transition: false } },
  });
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("shell drawer DOM", () => {
  it("renders nav and topbar, opens the drawer on toggle", async () => {
    const wrapper = mountShell();
    const layout = await wrapper;
    expect(layout.find("#app-nav").exists()).toBe(true);
    expect(layout.find(".topbar").exists()).toBe(true);
    expect(layout.find(".app.is-open").exists()).toBe(false);
    expect(layout.find(".backdrop").exists()).toBe(false);
    await layout
      .find('button[aria-label="Toggle navigation"]')
      .trigger("click");
    expect(layout.find(".app.is-open").exists()).toBe(true);
    expect(layout.find(".backdrop").exists()).toBe(true);
    await layout.find(".backdrop").trigger("click");
    expect(layout.find(".app.is-open").exists()).toBe(false);
    layout.unmount();
  });

  it("remounts with a closed drawer", async () => {
    const first = await mountShell();
    await first.find('button[aria-label="Toggle navigation"]').trigger("click");
    expect(first.find(".app.is-open").exists()).toBe(true);
    first.unmount();
    const second = await mountShell();
    expect(second.find(".app.is-open").exists()).toBe(false);
    expect(second.find(".backdrop").exists()).toBe(false);
    expect(second.find("#app-nav").exists()).toBe(true);
    second.unmount();
  });
});
