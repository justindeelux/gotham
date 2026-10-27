/* global document:readonly, window:readonly */
import { NMessageProvider } from "naive-ui";

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
  return {
    ...actual,
    listEntries: vi.fn(),
    listRoots: vi.fn(),
  };
});

import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h, nextTick } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";

import { listEntries, listRoots } from "@/features/files/api/files";
import ServerFilesPage from "@/features/files/pages/ServerFilesPage.vue";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { filesPane } from "@/features/files/stores/filesPane";
import { useFilesPaneSession } from "@/features/files/stores/paneSession";
import { useServersStore } from "@/features/servers";
import { setSession } from "@/shared/api/token";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1,
};

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

function signIn(id: string): void {
  setSession({
    user: { id, email: `${id}@gotham.dev`, created_at: "2026-01-01T00:00:00Z" },
    accessToken: "a",
    refreshToken: "r",
  });
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/servers/:id/files",
        name: "server-files",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/servers/server-1/files");
  await router.isReady();
  const wrapper = mount(
    defineComponent({
      render: () =>
        h(NMessageProvider, null, { default: () => h(ServerFilesPage) }),
    }),
    {
      attachTo: document.body,
      global: { plugins: [router, i18n], stubs: { teleport: true } },
    },
  );
  await flushPromises();
  return { wrapper, router };
}

async function split(wrapper: Awaited<ReturnType<typeof mountPage>>["wrapper"]) {
  await wrapper.get('button[aria-label="Split view"]').trigger("click");
  await flushPromises();
}

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
  signIn("user-1");
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  const servers = useServersStore();
  servers.servers = [
    {
      id: "server-1",
      name: "node-1",
      ip: "10.0.0.1",
      status: "ready",
    } as never,
    {
      id: "server-2",
      name: "node-2",
      ip: "10.0.0.2",
      status: "ready",
    } as never,
  ];
  servers.loading = false;
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockImplementation(async (serverId) => ({
    entries: [entry(`${serverId}.txt`)],
    truncated: false,
    cursor: "",
  }));
});

describe("split view", () => {
  it("duplicates the left location and keeps node selection per pane", async () => {
    const { wrapper, router } = await mountPage();
    const left = filesPane("left").files;
    await left.openAt("volumes", "apps");
    await flushPromises();

    await split(wrapper);

    expect(wrapper.findAll(".files-browser")).toHaveLength(2);
    const right = filesPane("right").files;
    expect(left.serverId).toBe("server-1");
    expect(right.serverId).toBe("server-1");
    expect(right.tabs.map((tab) => tab.path)).toEqual(["", "apps"]);
    expect(right.activeTab.path).toBe("apps");

    const select = wrapper.findComponent(
      '.files-browser[data-pane="right"] .fm-node__select',
    );
    expect(select.exists()).toBe(true);
    select.vm.$emit("update:value", "server-2");
    await flushPromises();

    expect(right.serverId).toBe("server-2");
    expect(left.serverId).toBe("server-1");
    expect(router.currentRoute.value.params.id).toBe("server-1");
  });

  it("persists the split state and secondary node per user", async () => {
    const { wrapper } = await mountPage();
    await split(wrapper);
    const right = filesPane("right").files;
    const select = wrapper.findComponent(
      '.files-browser[data-pane="right"] .fm-node__select',
    );
    select.vm.$emit("update:value", "server-2");
    await flushPromises();
    expect(right.serverId).toBe("server-2");

    const raw = window.localStorage.getItem("gotham.files.split.v1.user-1");
    expect(raw).not.toBeNull();
    expect(JSON.parse(raw ?? "{}")).toMatchObject({
      enabled: true,
      secondaryServerId: "server-2",
    });

    setActivePinia(createPinia());
    const reloaded = useFilesPaneSession();
    expect(reloaded.splitEnabled).toBe(true);
    expect(reloaded.secondaryServerId).toBe("server-2");
    wrapper.unmount();
  });

  it("guards a dirty editor once when the right pane switches node", async () => {
    const { wrapper, router } = await mountPage();
    await split(wrapper);
    const rightEditor = filesPane("right").editor;
    const right = filesPane("right").files;
    rightEditor.serverId = "server-1";
    rightEditor.tabs = [
      {
        id: "right-tab",
        name: "app.yaml",
        kind: "file",
        root: "volumes",
        path: "app.yaml",
        doc: "edited\n",
        original: "base\n",
        etag: '"1"',
        eol: "\n",
        bom: false,
        encoding: "utf-8",
        size: 6,
        mode: 0o100644,
        truncated: false,
        language: "yaml",
        languageManual: false,
        status: "ready",
        error: "",
        saving: false,
      } as never,
    ];
    rightEditor.activeId = "right-tab";
    const confirm = vi.spyOn(rightEditor, "confirmDiscard");
    expect(right.serverId).toBe("server-1");
    await vi.waitFor(() => {
      expect(
        wrapper
          .find('.files-browser[data-pane="right"] .fm-node__select')
          .exists(),
      ).toBe(true);
    });

    const select = wrapper.findComponent(
      '.files-browser[data-pane="right"] .fm-node__select',
    );
    select.vm.$emit("update:value", "server-2");
    await flushPromises();
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(rightEditor.leaveOpen).toBe(true);
    expect(right.serverId).toBe("server-1");

    rightEditor.resolveLeave(false);
    await flushPromises();
    expect(right.serverId).toBe("server-1");
    expect(router.currentRoute.value.params.id).toBe("server-1");

    select.vm.$emit("update:value", "server-2");
    await flushPromises();
    expect(confirm).toHaveBeenCalledTimes(2);
    rightEditor.resolveLeave(true);
    await flushPromises();
    expect(right.serverId).toBe("server-2");
    expect(rightEditor.tabs).toHaveLength(0);
    expect(router.currentRoute.value.params.id).toBe("server-1");
    wrapper.unmount();
  });

  it("guards a dirty secondary pane before closing it", async () => {
    const { wrapper } = await mountPage();
    await split(wrapper);
    const rightEditor = filesPane("right").editor;
    rightEditor.serverId = "server-1";
    rightEditor.tabs = [
      {
        id: "right-tab",
        name: "app.yaml",
        kind: "file",
        root: "volumes",
        path: "app.yaml",
        doc: "edited\n",
        original: "base\n",
        etag: '"1"',
        eol: "\n",
        bom: false,
        encoding: "utf-8",
        language: "yaml",
        languageManual: false,
        status: "ready",
        truncated: false,
        error: "",
        saving: false,
        size: 6,
        mode: 0o100644,
      } as never,
    ];
    rightEditor.activeId = "right-tab";
    await nextTick();
    expect(rightEditor.hasDirty).toBe(true);

    await wrapper.get('button[aria-label="Close right pane"]').trigger("click");
    await flushPromises();
    await nextTick();
    expect(rightEditor.leaveOpen).toBe(true);
    expect(wrapper.findAll(".files-browser")).toHaveLength(2);

    rightEditor.resolveLeave(false);
    await flushPromises();
    await nextTick();
    expect(wrapper.findAll(".files-browser")).toHaveLength(2);

    await wrapper.get('button[aria-label="Close right pane"]').trigger("click");
    await flushPromises();
    rightEditor.resolveLeave(true);
    await flushPromises();
    expect(useFilesPaneSession().splitEnabled).toBe(false);
    expect(wrapper.findAll(".files-browser")).toHaveLength(1);
    expect(rightEditor.tabs).toHaveLength(0);
    wrapper.unmount();
  });
});
