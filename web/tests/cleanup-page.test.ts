import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

import { cleanNode, scanNode } from "@/features/cleanup/api/cleanup";
import type {
  CleanupNodeScan,
  CleanupRunResult,
} from "@/features/cleanup/api/cleanup";
import { useCleanupPage } from "@/features/cleanup/composables/useCleanupPage";
import type { Server } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers/stores/servers";

const route = vi.hoisted(() => ({ query: {} as Record<string, string> }));

vi.mock("vue-router", () => ({
  useRoute: () => route,
}));

vi.mock("@/features/cleanup/api/cleanup", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/cleanup/api/cleanup")>();
  return { ...actual, scanNode: vi.fn(), cleanNode: vi.fn() };
});

function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function server(id = "srv-1", name = "node-a"): Server {
  return {
    id,
    name,
    ip: "10.0.0.1",
    port: 22,
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

function scan(): CleanupNodeScan {
  return {
    server_id: "srv-1",
    status: "ok",
    reason: "",
    items: [
      {
        id: "build-cache",
        category: "build_cache",
        tier: "safe",
        name: "Build cache",
        size_bytes: 10,
        created_at: "",
        reason: "",
      },
    ],
    notices: [],
  };
}

function setup(servers: Server[] = [server()]): ReturnType<typeof useCleanupPage> {
  setActivePinia(createPinia());
  const store = useServersStore();
  store.servers = servers;
  let page!: ReturnType<typeof useCleanupPage>;
  mount(
    defineComponent({
      setup() {
        page = useCleanupPage();
        return () => h("div");
      },
    }),
  );
  return page;
}

beforeEach(() => {
  route.query = {};
  vi.resetAllMocks();
});

describe("cleanup deep link", () => {
  it("preselects only the requested node", () => {
    route.query = { server: "srv-2" };
    const page = setup([server("srv-1", "node-a"), server("srv-2", "node-b")]);
    expect([...page.selectedServerIds.value]).toEqual(["srv-2"]);
  });

  it("keeps the online default for an unknown node", () => {
    route.query = { server: "gone" };
    const page = setup([server("srv-1", "node-a"), server("srv-2", "node-b")]);
    expect(page.selectedServerIds.value.size).toBe(2);
  });
});

describe("cleanup in-flight guards", () => {
  it("a second scan while scanning sends one request", async () => {
    const page = setup();
    const pending = deferred<CleanupNodeScan>();
    vi.mocked(scanNode).mockReturnValueOnce(pending.promise);

    const first = page.startScan();
    const second = page.startScan();

    expect(scanNode).toHaveBeenCalledTimes(1);
    expect(page.phase.value).toBe("scanning");

    pending.resolve(scan());
    await Promise.all([first, second]);
    expect(page.phase.value).toBe("results");
  });

  it("a second clean while cleaning sends one request", async () => {
    const page = setup();
    vi.mocked(scanNode).mockResolvedValueOnce(scan());
    await page.startScan();

    const pending = deferred<CleanupRunResult>();
    vi.mocked(cleanNode).mockReturnValueOnce(pending.promise);

    const first = page.startClean();
    const second = page.startClean();

    expect(cleanNode).toHaveBeenCalledTimes(1);
    expect(page.phase.value).toBe("cleaning");

    pending.resolve({
      server_id: "srv-1",
      status: "ok",
      reason: "",
      reclaimed_bytes: 10,
      items: [],
    });
    await Promise.all([first, second]);
    expect(page.phase.value).toBe("done");
  });
});
