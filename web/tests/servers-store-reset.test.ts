// JUS-143: hardening the servers store reset and the shell-owned poll.
//
// reset() is called on sign-out (see the auth store). It must invalidate any
// list load still in flight so a previous-account response cannot resolve into
// the next account's session, and the shared poll must keep a single timer no
// matter how many seeding sites arm it.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Server } from "../src/features/servers";
import { listServers } from "../src/features/servers/api/servers";
import { useServersStore } from "../src/features/servers/stores/servers";

vi.mock("../src/features/servers/api/servers", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../src/features/servers/api/servers")>()),
  listServers: vi.fn(),
}));

function server(id: string, name: string): Server {
  return {
    id,
    name,
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

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (_value: T) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
});

describe("servers store reset (JUS-143)", () => {
  it("drops an in-flight load across sign-out so the next account never sees the previous list", async () => {
    const store = useServersStore();
    const previous = deferred<Server[]>();
    vi.mocked(listServers).mockReturnValueOnce(previous.promise);

    // A load for the previous account is in flight when sign-out resets.
    const inFlight = store.fetchServers();
    expect(store.loading).toBe(true);

    store.reset();
    expect(store.servers).toEqual([]);

    previous.resolve([server("srv-1", "prev-account")]);
    await inFlight;

    // The stale response must not repopulate the list after reset.
    expect(store.servers).toEqual([]);

    // Signing in as another account loads and applies its own list.
    vi.mocked(listServers).mockResolvedValueOnce([server("srv-2", "next-account")]);
    await store.fetchServers();
    expect(store.servers.map((item) => item.name)).toEqual(["next-account"]);
  });

  it("keeps a single poll timer when armed more than once", () => {
    const store = useServersStore();
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");

    store.pollServers();
    store.pollServers();

    expect(setIntervalSpy).toHaveBeenCalledTimes(1);
    store.stopPolling();
    setIntervalSpy.mockRestore();
  });
});
