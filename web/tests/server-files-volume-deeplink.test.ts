// GOTHAM-16: opening a storage volume in File Manager navigates to
// /servers/:id/files?root=&path=. The folder must open on the first render —
// not after a refresh — including when the browser re-initialises for a server
// whose tabs are still in the store (application detail → File Manager is a
// route change, so the page mounts with the previous visit's tabs loaded).
// This mounts the real page and the real browser with the files API mocked, so
// the mount-order race (deep link applied, then the tab restore overwrites it)
// is exercised end to end.
/* global document:readonly, window:readonly */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const messages = {
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
};

vi.mock("naive-ui", async (importOriginal) => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return { ...actual, useMessage: () => messages };
});

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return { ...actual, listRoots: vi.fn(), listEntries: vi.fn() };
});

import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import type { Router } from "vue-router";
import { nextTick } from "vue";

import { useAuthStore } from "@/features/auth";
import { listEntries, listRoots } from "@/features/files/api/files";
import ServerFilesPage from "@/features/files/pages/ServerFilesPage.vue";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import type { FilesTabsSnapshot } from "@/features/files/schemas/tabsPersist";
import { useFilesStore } from "@/features/files/stores/files";
import {
  filesTabsStorageKey,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";
import { useServersStore } from "@/features/servers";
import { useTeamsStore } from "@/features/teams";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const volumes: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1,
};

const compose: FilesRoot = {
  name: "compose",
  path: "/var/lib/gotham-agent/compose",
  writable: true,
  free_bytes: 1,
};

/** entry builds one distinguishable row per directory. */
function entry(name: string): FilesEntry {
  return {
    name,
    kind: "file",
    size: 0,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
  };
}

/** previousVisit is the persisted snapshot of the visit before the deep link. */
function previousVisit(): FilesTabsSnapshot {
  return {
    version: 1,
    tabs: [{ id: "tab-a", root: "compose", path: "" }],
    activeTabId: "tab-a",
    viewMode: "list",
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  window.localStorage.clear();
  vi.clearAllMocks();

  const auth = useAuthStore();
  auth.user = {
    id: "user-1",
    email: "ada@gotham.dev",
    created_at: "2026-10-12T09:02:00Z",
  };
  const servers = useServersStore();
  servers.servers = [
    {
      id: "server-1",
      name: "node-1",
      ip: "127.0.0.1",
      status: "ready",
    } as never,
  ];
  servers.loading = false;
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [
    { id: "team-1", name: "A", is_personal: true, role: "owner" },
  ];

  vi.mocked(listRoots).mockResolvedValue([volumes, compose]);
  vi.mocked(listEntries).mockImplementation(
    async (_serverId, _teamId, options) => ({
      entries: [entry(`${options.root}:${options.path ?? ""}`)],
      truncated: false,
      cursor: "",
    }),
  );
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

/** settle flushes the store promises and the watcher chain. */
async function settle(): Promise<void> {
  for (let index = 0; index < 5; index += 1) {
    await flushPromises();
    await nextTick();
  }
}

/** routerAt builds a memory router already resolved at one files URL. */
async function routerAt(url: string): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/servers/:id/files",
        name: "server-files",
        component: { template: "<div />" },
      },
      { path: "/", component: { template: "<div />" } },
    ],
  });
  await router.push(url);
  await router.isReady();
  return router;
}

/** mountPage mounts the real page + browser at one deep link. */
async function mountPage(
  url: string,
): Promise<{ wrapper: VueWrapper; router: Router }> {
  const router = await routerAt(url);
  const wrapper = mount(ServerFilesPage, {
    attachTo: document.body,
    global: { plugins: [router, i18n], stubs: { teleport: true } },
  });
  await settle();
  return { wrapper, router };
}

describe("opening a storage volume in File Manager", () => {
  it("adds the linked directory as a new tab beside the previous visit's tabs", async () => {
    const files = useFilesStore();
    // The user was in File Manager for this server before opening the volume:
    // the store is bound and localStorage holds the persisted snapshot.
    await files.init("server-1", previousVisit());
    expect(files.activeTab.rootName).toBe("compose");
    writeFilesTabsSnapshot(
      window.localStorage,
      filesTabsStorageKey("server-1", "user-1"),
      previousVisit(),
    );

    const { wrapper } = await mountPage(
      "/servers/server-1/files?root=volumes&path=app-1/data",
    );

    // The volume opens its own focused tab; the composed tab is still there.
    expect(files.tabs).toHaveLength(2);
    expect(files.tabs[0].rootName).toBe("compose");
    expect(files.activeTab.rootName).toBe("volumes");
    expect(files.activeTab.path).toBe("app-1/data");
    expect(files.entries[0]?.name).toBe("volumes:app-1/data");
    wrapper.unmount();
  });

  it("opens a second volume link while the file manager is already mounted", async () => {
    const files = useFilesStore();
    const { wrapper, router } = await mountPage(
      "/servers/server-1/files?root=volumes&path=app-1/data",
    );
    expect(files.activeTab.path).toBe("app-1/data");

    await router.push({
      name: "server-files",
      params: { id: "server-1" },
      query: { root: "volumes", path: "app-2/data" },
    });
    await settle();

    expect(files.activeTab.rootName).toBe("volumes");
    expect(files.activeTab.path).toBe("app-2/data");
    // The first volume's tab stays open too.
    expect(files.tabs.filter((tab) => tab.path === "app-1/data")).toHaveLength(1);
    wrapper.unmount();
  });

  it("keeps the URL on the directory the browser navigated to", async () => {
    const files = useFilesStore();
    const { wrapper, router } = await mountPage(
      "/servers/server-1/files?root=volumes&path=app-1/data",
    );

    await files.goToPath("app-1/data/nested");
    await settle();

    expect(router.currentRoute.value.query).toMatchObject({
      root: "volumes",
      path: "app-1/data/nested",
    });
    wrapper.unmount();
  });
});
