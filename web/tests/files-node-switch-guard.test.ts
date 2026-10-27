/* global document:readonly, window:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider, NSelect } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

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
    getContent: vi.fn(),
    listEntries: vi.fn(),
    listFileVersions: vi.fn(async () => []),
    listRoots: vi.fn(),
  };
});

vi.mock("@/features/files/composables/useCodeMirror", () => ({
  createEditorSession: vi.fn(async () => ({
    getDoc: () => "",
    setDoc: vi.fn(),
    setLanguage: vi.fn(),
    setSettings: vi.fn(),
    setReadOnly: vi.fn(),
    focus: vi.fn(),
    requestMeasure: vi.fn(),
    search: {
      setOptions: vi.fn(() => ({ total: 0, current: 0, capped: false })),
      status: vi.fn(() => ({ total: 0, current: 0, capped: false })),
      next: vi.fn(() => ({ total: 0, current: 0, capped: false })),
      previous: vi.fn(() => ({ total: 0, current: 0, capped: false })),
      replace: vi.fn(() => ({ total: 0, current: 0, capped: false })),
      replaceAll: vi.fn(() => ({ total: 0, current: 0, capped: false })),
      clear: vi.fn(),
    },
    lineInfo: vi.fn(() => ({ line: 1, total: 10 })),
    gotoLine: vi.fn(() => null),
    destroy: vi.fn(),
  })),
}));

vi.mock("@/features/files/composables/useMergeView", () => ({
  createMergeSession: vi.fn(async () => ({
    setSettings: vi.fn(),
    destroy: vi.fn(),
    requestMeasure: vi.fn(),
  })),
}));

vi.mock("@/features/files/composables/useFileDownload", () => ({
  useFileDownload: () => ({ pending: { value: false }, download: vi.fn() }),
}));

import { listEntries, listRoots } from "@/features/files/api/files";
import FilesEditor from "@/features/files/components/FilesEditor.vue";
import ServerFilesPage from "@/features/files/pages/ServerFilesPage.vue";
import type { EditorTab } from "@/features/files/stores/editor";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { useServersStore } from "@/features/servers";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

function server(id: string, name: string) {
  return { id, name, ip: "127.0.0.1", status: "ready" } as never;
}

function makeTab(id: string, dirty: boolean): EditorTab {
  return {
    id,
    kind: "file",
    root: "volumes",
    path: `${id}.md`,
    name: `${id}.md`,
    doc: dirty ? "changed" : "start",
    original: "start",
    etag: '"1"',
    eol: "\n",
    bom: false,
    encoding: "utf-8",
    size: 7,
    mode: 0o100644,
    truncated: false,
    language: "markdown",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

async function mountHarness() {
  setActivePinia(createPinia());
  const servers = useServersStore();
  servers.servers = [
    server("server-1", "node-1"),
    server("server-2", "node-2"),
  ];
  servers.loading = false;

  const files = useFilesStore("left");
  const editor = useEditorStore("left");
  editor.serverId = "server-1";
  editor.tabs = [makeTab("t1", true)];
  editor.activeId = "t1";

  const stub = { template: "<div />" };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/servers/:id/files", name: "server-files", component: stub },
    ],
  });
  await router.push("/servers/server-1/files");
  await router.isReady();

  const wrapper = mount(
    defineComponent({
      render: () =>
        h(NMessageProvider, null, {
          default: () =>
            h("div", [h(ServerFilesPage), h(FilesEditor)]),
        }),
    }),
    {
      attachTo: document.body,
      global: { plugins: [router, i18n], stubs: { teleport: true } },
    },
  );
  await flushPromises();
  return { wrapper, router, files, editor };
}

async function switchToServer2(
  wrapper: Awaited<ReturnType<typeof mountHarness>>["wrapper"],
): Promise<void> {
  const select = wrapper.findComponent(".fm-node__select");
  expect(select.exists()).toBe(true);
  select.vm.$emit("update:value", "server-2");
  await flushPromises();
  await nextTick();
  await flushPromises();
}

beforeEach(() => {
  window.localStorage.clear();
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  vi.mocked(listRoots).mockResolvedValue([
    { name: "volumes", path: "/v", writable: true, free_bytes: 1 },
  ]);
  vi.mocked(listEntries).mockResolvedValue({
    entries: [],
    truncated: false,
    cursor: "",
  });
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("dirty editor on a node switch", () => {
  it("asks once and keeps the current node when the user stays", async () => {
    const { wrapper, router, files, editor } = await mountHarness();
    expect(wrapper.findAllComponents(NSelect).length).toBeGreaterThan(0);
    const confirm = vi.spyOn(editor, "confirmDiscard");

    await switchToServer2(wrapper);

    expect(router.currentRoute.value.params.id).toBe("server-2");
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(editor.leaveOpen).toBe(true);
    expect(files.serverId).toBe("server-2");

    editor.resolveLeave(false);
    await flushPromises();
    await nextTick();
    await flushPromises();

    expect(router.currentRoute.value.params.id).toBe("server-1");
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(editor.leaveOpen).toBe(false);
    expect(editor.tabs).toHaveLength(1);
    wrapper.unmount();
  });

  it("asks once and discards the editor when the user leaves", async () => {
    const { wrapper, router, files, editor } = await mountHarness();
    const confirm = vi.spyOn(editor, "confirmDiscard");

    await switchToServer2(wrapper);
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(editor.leaveOpen).toBe(true);

    editor.resolveLeave(true);
    await flushPromises();
    await nextTick();
    await flushPromises();

    expect(router.currentRoute.value.params.id).toBe("server-2");
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(editor.leaveOpen).toBe(false);
    expect(editor.tabs).toHaveLength(0);
    expect(files.serverId).toBe("server-2");
    wrapper.unmount();
  });
});
