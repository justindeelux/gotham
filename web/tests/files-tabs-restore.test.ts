/* global document:readonly, Event:readonly, window:readonly */
import { createPinia, setActivePinia } from "pinia";
import type { Pinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return { ...actual, listRoots: vi.fn(), listEntries: vi.fn() };
});

import { listEntries, listRoots } from "@/features/files/api/files";
import { useAuthStore } from "@/features/auth/stores/auth";
import { useFilesStore } from "@/features/files/stores/files";
import { useFilesTabsPersist } from "@/features/files/composables/useFilesTabsPersist";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import type { FilesTabsSnapshot } from "@/features/files/schemas/tabsPersist";
import {
  filesTabsStorageKey,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";

const volumes: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

const compose: FilesRoot = {
  name: "compose",
  path: "/var/lib/gotham-agent/compose",
  writable: false,
  free_bytes: 0,
};

function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
  return {
    kind: "file",
    size: 1024,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

function snapshot(overrides: Partial<FilesTabsSnapshot> = {}): FilesTabsSnapshot {
  return {
    version: 1,
    tabs: [
      { id: "tab-a", root: "volumes", path: "data" },
      { id: "tab-b", root: "volumes", path: "media" },
    ],
    activeTabId: "tab-a",
    viewMode: "grid",
    ...overrides,
  };
}

function deferred<T>() {
  let resolve!: (_value: T) => void;
  let reject!: (_reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

const Harness = defineComponent({
  props: { serverId: { type: String, required: true } },
  setup(props) {
    const { initBrowser } = useFilesTabsPersist(() => props.serverId);
    return { initBrowser };
  },
  render() {
    return h("div");
  },
});

type HarnessVm = { initBrowser: (_serverId: string) => Promise<void> };

let wrappers: VueWrapper[] = [];
let pinia: Pinia;

function mountHarness(serverId: string): VueWrapper {
  const wrapper = mount(Harness, {
    props: { serverId },
    global: { plugins: [pinia] },
  });
  wrappers.push(wrapper);
  return wrapper;
}

let listedCalls: Array<{ root: string; path: string }> = [];

beforeEach(() => {
  vi.useFakeTimers();
  pinia = createPinia();
  setActivePinia(pinia);
  wrappers = [];
  listedCalls = [];
  window.localStorage.clear();
  vi.mocked(listRoots).mockReset();
  vi.mocked(listEntries).mockReset();
  vi.mocked(listRoots).mockResolvedValue([volumes, compose]);
  vi.mocked(listEntries).mockImplementation(
    async (_serverId, options) => {
      listedCalls.push({
        root: options.root,
        path: options.path ?? "",
      });
      return {
        entries: [entry({ name: `hit:${options.path ?? ""}` })],
        truncated: false,
        cursor: "",
      };
    },
  );


  const auth = useAuthStore();
  auth.user = {
    id: "user-1",
    email: "user-1@example.com",
    created_at: "2026-10-01T00:00:00Z",
  };
});

afterEach(() => {
  for (const wrapper of wrappers) {
    wrapper.unmount();
  }
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("slow first listing", () => {
  it("never overwrites the stored snapshot with defaults and restores it", async () => {
    const key = filesTabsStorageKey("server-1", "user-1");
    const stored = snapshot();
    expect(writeFilesTabsSnapshot(window.localStorage, key, stored)).toBe(true);
    const before = window.localStorage.getItem(key);

    const rootsGate = deferred<FilesRoot[]>();
    vi.mocked(listRoots).mockImplementation(() => rootsGate.promise);

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    const pending = vm.initBrowser("server-1");

    await vi.advanceTimersByTimeAsync(1000);
    expect(window.localStorage.getItem(key)).toBe(before);

    rootsGate.resolve([volumes, compose]);
    await pending;
    await flushPromises();

    const files = useFilesStore();
    expect(files.tabs.map((tab) => tab.path).sort()).toEqual(["data", "media"]);
    expect(files.activeTabId).toBe("tab-a");
    expect(files.activeTab.path).toBe("data");

    await vi.advanceTimersByTimeAsync(1000);
    expect(window.localStorage.getItem(key)).toBe(before);
  });

  it("loads the active restored tab with one fetch and no flash of the root", async () => {
    const key = filesTabsStorageKey("server-1", "user-1");
    writeFilesTabsSnapshot(window.localStorage, key, snapshot());

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    await vm.initBrowser("server-1");
    await flushPromises();

    expect(listedCalls).toEqual([{ root: "volumes", path: "data" }]);
    expect(useFilesStore().entries[0]?.name).toBe("hit:data");
  });
});

describe("restore guards", () => {
  it("skips the restore when the user acts during init", async () => {
    const key = filesTabsStorageKey("server-1", "user-1");
    writeFilesTabsSnapshot(window.localStorage, key, snapshot());

    const rootsGate = deferred<FilesRoot[]>();
    vi.mocked(listRoots).mockImplementation(() => rootsGate.promise);

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    const pending = vm.initBrowser("server-1");
    await flushPromises();

    const files = useFilesStore();
    await files.openTab({ root: "volumes", path: "sandbox" });
    const userTabId = files.activeTabId;

    rootsGate.resolve([volumes, compose]);
    await pending;
    await flushPromises();

    expect(files.activeTabId).toBe(userTabId);
    expect(files.tabs.some((tab) => tab.path === "sandbox")).toBe(true);
    expect(files.tabs.some((tab) => tab.id === "tab-a")).toBe(false);
  });

  it("concurrent inits share one roots fetch and restore once", async () => {
    const key = filesTabsStorageKey("server-1", "user-1");
    writeFilesTabsSnapshot(window.localStorage, key, snapshot());

    const rootsGate = deferred<FilesRoot[]>();
    vi.mocked(listRoots).mockImplementation(() => rootsGate.promise);

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    const first = vm.initBrowser("server-1");
    const second = vm.initBrowser("server-1");
    await flushPromises();
    rootsGate.resolve([volumes, compose]);
    await Promise.all([first, second]);
    await flushPromises();

    expect(vi.mocked(listRoots)).toHaveBeenCalledTimes(1);
    const files = useFilesStore();
    expect(files.activeTabId).toBe("tab-a");
    expect(files.tabs).toHaveLength(2);
  });

  it("store-level concurrent inits join the in-flight load", async () => {
    const rootsGate = deferred<FilesRoot[]>();
    vi.mocked(listRoots).mockImplementation(() => rootsGate.promise);
    const files = useFilesStore();
    const first = files.init("server-1", snapshot());
    const second = files.init("server-1", snapshot());
    await flushPromises();
    rootsGate.resolve([volumes, compose]);
    await Promise.all([first, second]);

    expect(vi.mocked(listRoots)).toHaveBeenCalledTimes(1);
    expect(files.activeTabId).toBe("tab-a");
  });
});

describe("debounce and flush", () => {
  it("debounces writes and flushes on pagehide", async () => {
    const key = filesTabsStorageKey("server-1", "user-1");
    writeFilesTabsSnapshot(window.localStorage, key, snapshot());

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    await vm.initBrowser("server-1");
    await flushPromises();
    const baseline = window.localStorage.getItem(key);

    const files = useFilesStore();
    await files.goToPath("data/photos");

    await vi.advanceTimersByTimeAsync(100);
    expect(window.localStorage.getItem(key)).toBe(baseline);

    window.dispatchEvent(new Event("pagehide"));
    const flushed = window.localStorage.getItem(key);
    expect(flushed).not.toBe(baseline);
    expect(flushed).toContain("data/photos");

    await files.goToPath("data/music");
    await vi.advanceTimersByTimeAsync(1000);
    expect(window.localStorage.getItem(key)).toContain("data/music");
  });

  it("flushes on visibilitychange to hidden", async () => {
    const key = filesTabsStorageKey("server-1", "user-1");
    writeFilesTabsSnapshot(window.localStorage, key, snapshot());

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    await vm.initBrowser("server-1");
    await flushPromises();
    const baseline = window.localStorage.getItem(key);

    await useFilesStore().goToPath("data/hidden");
    Object.defineProperty(document, "visibilityState", {
      value: "hidden",
      configurable: true,
    });
    try {
      document.dispatchEvent(new Event("visibilitychange"));
    } finally {
      Object.defineProperty(document, "visibilityState", {
        value: "prerender",
        configurable: true,
      });
    }
    expect(window.localStorage.getItem(key)).not.toBe(baseline);
    expect(window.localStorage.getItem(key)).toContain("data/hidden");
  });
});

describe("server and user isolation", () => {
  it("keeps each server's state in its own key", async () => {
    const keyOne = filesTabsStorageKey("server-1", "user-1");
    writeFilesTabsSnapshot(
      window.localStorage,
      keyOne,
      snapshot({ tabs: [{ id: "tab-a", root: "volumes", path: "data" }] }),
    );

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    await vm.initBrowser("server-1");
    await flushPromises();
    expect(useFilesStore().activeTab.path).toBe("data");

    await wrapper.setProps({ serverId: "server-2" });
    await vm.initBrowser("server-2");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);

    expect(useFilesStore().activeTab.path).toBe("");
    const keyTwo = filesTabsStorageKey("server-2", "user-1");
    expect(window.localStorage.getItem(keyTwo)).toContain('"path":""');
    expect(window.localStorage.getItem(keyOne)).toContain('"path":"data"');
  });

  it("keeps each user's state in its own key and keeps it after logout", async () => {
    const keyOne = filesTabsStorageKey("server-1", "user-1");
    writeFilesTabsSnapshot(window.localStorage, keyOne, snapshot());
    const stored = window.localStorage.getItem(keyOne);

    const wrapper = mountHarness("server-1");
    const vm = wrapper.vm as unknown as HarnessVm;
    await vm.initBrowser("server-1");
    await flushPromises();
    expect(useFilesStore().activeTab.path).toBe("data");

    const auth = useAuthStore();
    auth.user = {
      id: "user-2",
      email: "user-2@example.com",
      created_at: "2026-10-01T00:00:00Z",
    };
    useFilesStore().reset();
    await vm.initBrowser("server-1");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    expect(useFilesStore().activeTab.path).toBe("");
    expect(window.localStorage.getItem(keyOne)).toBe(stored);

    auth.user = null;
    useFilesStore().reset();
    await vm.initBrowser("server-1");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    expect(useFilesStore().activeTab.path).toBe("");
    expect(window.localStorage.getItem(keyOne)).toBe(stored);

    auth.user = {
      id: "user-1",
      email: "user-1@example.com",
      created_at: "2026-10-01T00:00:00Z",
    };
    useFilesStore().reset();
    await vm.initBrowser("server-1");
    await flushPromises();
    expect(useFilesStore().activeTab.path).toBe("data");
  });
});
