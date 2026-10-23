
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick, reactive } from "vue";

import type { FilesRoot } from "@/features/files/schemas/files";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const filesMock = vi.hoisted(() => ({ state: null as unknown }));

vi.mock("@/features/files", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/files")>();
  return { ...actual, useFilesStore: () => filesMock.state };
});

import { useServersStore } from "@/features/servers";
import ServerFilesPage from "@/features/files/pages/ServerFilesPage.vue";

const volumes: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1,
};
const system: FilesRoot = {
  name: "system",
  path: "/",
  writable: true,
  free_bytes: 1,
};

interface MockFiles {
  roots: FilesRoot[];
  rootsServerId: string;
  activeTab: { rootName: string };
  errorRaw: unknown;
  openAt: ReturnType<typeof vi.fn>;
}

let files: MockFiles;

async function routerFor(url: string) {
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

async function mountPage(url: string) {
  const router = await routerFor(url);
  const wrapper = mount(ServerFilesPage, {
    global: {
      plugins: [router, i18n],
      stubs: { FilesBrowser: true },
    },
  });
  await nextTick();
  return { wrapper, router };
}

function noticeText(wrapper: Awaited<ReturnType<typeof mountPage>>["wrapper"]): string {
  return wrapper.find(".server-files__deep-link").text();
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
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

  files = reactive<MockFiles>({
    roots: [volumes, system],
    rootsServerId: "server-1",
    activeTab: { rootName: "system" },
    errorRaw: null,
    openAt: vi.fn(async () => undefined),
  });
  filesMock.state = files;
});

describe("ServerFilesPage deep link", () => {
  it("never validates a link against the previous server's roots", async () => {
    files.rootsServerId = "server-1";
    const { wrapper } = await mountPage(
      "/servers/server-2/files?root=volumes&path=app-1/data",
    );
    expect(files.openAt).not.toHaveBeenCalled();
    expect(wrapper.find(".server-files__deep-link").exists()).toBe(false);

    files.rootsServerId = "server-2";
    await nextTick();
    await nextTick();
    expect(files.openAt).toHaveBeenCalledWith("volumes", "app-1/data");
    expect(wrapper.find(".server-files__deep-link").exists()).toBe(false);
    wrapper.unmount();
  });

  it("opens the exact root and path of a valid link", async () => {
    const { wrapper } = await mountPage(
      "/servers/server-1/files?root=volumes&path=app-1/data",
    );
    expect(files.openAt).toHaveBeenCalledWith("volumes", "app-1/data");
    expect(wrapper.find(".server-files__deep-link").exists()).toBe(false);
    wrapper.unmount();
  });

  it("explains an unknown root with the root the browser actually shows", async () => {
    files.activeTab.rootName = "system";
    const { wrapper } = await mountPage(
      "/servers/server-1/files?root=nope&path=somewhere",
    );
    const text = noticeText(wrapper);
    expect(text).toContain("not available on this server");
    expect(text).toContain("nope");
    expect(text).toContain("system");
    wrapper.unmount();
  });

  it("translates the unknown-root notice to Vietnamese", async () => {
    syncComposerLocale("vi");
    const { wrapper } = await mountPage(
      "/servers/server-1/files?root=nope&path=somewhere",
    );
    expect(noticeText(wrapper)).toContain("không có trên máy chủ này");
    wrapper.unmount();
  });

  it("explains a malformed or escaping path as an invalid location", async () => {
    const { wrapper } = await mountPage(
      "/servers/server-1/files?root=volumes&path=..%2Fetc",
    );
    const text = noticeText(wrapper);
    expect(text).toContain("not a valid location");
    expect(text).not.toContain("not available on this server");
    wrapper.unmount();
  });

  it("translates the invalid-path notice to Vietnamese", async () => {
    syncComposerLocale("vi");
    const { wrapper } = await mountPage(
      "/servers/server-1/files?root=volumes&path=..%2Fetc",
    );
    expect(noticeText(wrapper)).toContain("không phải là vị trí hợp lệ");
    wrapper.unmount();
  });

  it("names a valid-but-missing directory after the load", async () => {
    files.openAt = vi.fn(async () => {
      files.errorRaw = { message: "not found", status: 404 };
    });
    const { wrapper } = await mountPage(
      "/servers/server-1/files?root=volumes&path=app-1/gone",
    );
    await nextTick();
    await nextTick();
    const text = noticeText(wrapper);
    expect(text).toContain("was not found in root");
    expect(text).toContain("app-1/gone");
    wrapper.unmount();
  });

  it("shows no notice without a deep-link query", async () => {
    const { wrapper } = await mountPage("/servers/server-1/files");
    expect(wrapper.find(".server-files__deep-link").exists()).toBe(false);
    expect(files.openAt).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
