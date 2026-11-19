import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { flushPromises, mount } from "@vue/test-utils";
import { NDropdown, NSelect } from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";

import type {
  ContainerFileCapabilities,
  ContainerFileEntry,
  ContainerFileListing,
} from "@/features/applications/api/containerFiles";
import CodeEditor from "@/features/files/components/CodeEditor.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const tabSourcePath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../src/features/applications/components/ApplicationFilesTab.vue",
);

// The table virtualizes its rows, and vueuc reads media queries and a resize
// observer while naive-ui loads; jsdom has neither, and the observer must exist
// before that import, so both stubs are hoisted with the module mocks.
vi.hoisted(() => {
  const view = globalThis.window as unknown as {
    matchMedia?: unknown;
    ResizeObserver?: unknown;
  };
  if (!view) {
    return;
  }
  if (!view.matchMedia) {
    view.matchMedia = () => ({
      matches: false,
      media: "",
      addEventListener: () => {},
      removeEventListener: () => {},
    });
  }
  if (!view.ResizeObserver) {
    class ResizeObserverStub {
      private readonly listener: (_entries: unknown[]) => void;

      constructor(listener: (_entries: unknown[]) => void) {
        this.listener = listener;
      }

      observe(target: unknown): void {
        this.listener([{ target, contentRect: { width: 1024, height: 420 } }]);
      }

      unobserve(): void {}

      disconnect(): void {}
    }
    view.ResizeObserver = ResizeObserverStub;
  }
});

// CodeMirror needs layout APIs jsdom does not have; the session is faked the
// same way the code editor tests do.
const codeMirror = vi.hoisted(() => {
  function fakeSession(doc: string) {
    let current = doc;
    const status = { total: 0, current: 0, capped: false };
    return {
      getDoc: vi.fn(() => current),
      setDoc: vi.fn((next: string) => {
        current = next;
      }),
      setLanguage: vi.fn(),
      setSettings: vi.fn(),
      setReadOnly: vi.fn(),
      setAccessibility: vi.fn(),
      focus: vi.fn(),
      requestMeasure: vi.fn(),
      search: {
        setOptions: vi.fn(() => status),
        status: vi.fn(() => status),
        next: vi.fn(() => status),
        previous: vi.fn(() => status),
        replace: vi.fn(() => status),
        replaceAll: vi.fn(() => status),
        clear: vi.fn(),
      },
      lineInfo: vi.fn(() => ({ line: 1, total: 1 })),
      gotoLine: vi.fn(() => null),
      destroy: vi.fn(),
    };
  }

  return {
    createEditorSession: vi.fn(async (options: { doc: string }) =>
      fakeSession(options.doc),
    ),
  };
});

vi.mock("@/features/files/composables/useCodeMirror", () => ({
  createEditorSession: codeMirror.createEditorSession,
}));

const capabilities = vi.hoisted(() => ({
  value: null as ContainerFileCapabilities | null,
}));

const containers = vi.hoisted(() => ({
  value: [
    { id: "c1", name: "demo-web", image: "gotham/demo:1", state: "exited", status: "Exited" },
  ] as Array<Record<string, string>>,
}));

vi.mock("@/features/applications/api/containers", () => ({
  listApplicationContainers: vi.fn(async () => containers.value),
  applicationContainerAction: vi.fn(async () => "c1"),
}));

vi.mock("@/features/applications/api/containerFiles", async () => {
  const actual = await vi.importActual<
    typeof import("@/features/applications/api/containerFiles")
  >("@/features/applications/api/containerFiles");
  const entries: ContainerFileEntry[] = [
    { path: "/app", name: "app", kind: "directory", size: 0, mode: 0o755, uid: 0, gid: 0 },
    { path: "/app/config.yml", name: "config.yml", kind: "file", size: 12, mode: 0o644, uid: 1000, gid: 1000 },
  ];
  return {
    ...actual,
    containerFileCapabilities: vi.fn(async () => capabilities.value),
    listContainerFiles: vi.fn(async () => ({
      directory: { path: "/", name: "/", kind: "directory", size: 0, mode: 0o755, uid: 0, gid: 0 },
      entries,
      truncated: false,
    })),
    readContainerFile: vi.fn(async () => ({ text: "port: 8080\n", isText: true })),
    writeContainerFile: vi.fn(async () => undefined),
    uploadContainerFile: vi.fn(async () => undefined),
    changeContainerFiles: vi.fn(async () => undefined),
    destinationExists: vi.fn(() => false),
    downloadContainerFile: vi.fn(async () => undefined),
  };
});

const { default: ApplicationFilesTab } = await import(
  "@/features/applications/components/ApplicationFilesTab.vue"
);

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  capabilities.value = {
    readable: true,
    exec_available: true,
    state: "running",
  };
  containers.value = [
    { id: "c1", name: "demo-web", image: "gotham/demo:1", state: "exited", status: "Exited" },
  ];
  codeMirror.createEditorSession.mockClear();
});

async function mountTab(canWrite = true) {
  const wrapper = mount(ApplicationFilesTab, {
    props: {
      appId: "app-1",
      canWrite,
    },
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
  await nextTick();
  await flushPromises();
  await nextTick();
  return wrapper;
}

function deferred<T>(): { promise: Promise<T>; resolve: (_value: T) => void } {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

function listing(path: string, entries: ContainerFileEntry[]): ContainerFileListing {
  return {
    directory: {
      path,
      name: path === "/" ? "/" : (path.split("/").pop() ?? path),
      kind: "directory",
      size: 0,
      mode: 0o755,
      uid: 0,
      gid: 0,
    },
    entries,
    truncated: false,
  };
}

function fileEntry(path: string): ContainerFileEntry {
  return {
    path,
    name: path.split("/").pop() ?? path,
    kind: "file",
    size: 1,
    mode: 0o644,
    uid: 0,
    gid: 0,
  };
}

function rowMenu(wrapper: ReturnType<typeof mount>): DropdownOption[] {
  return wrapper.findComponent(NDropdown).props("options") as DropdownOption[];
}

function menuKeys(wrapper: ReturnType<typeof mount>): string[] {
  return rowMenu(wrapper).map((option) => String(option.key));
}

async function openRowMenu(
  wrapper: ReturnType<typeof mount>,
  rowIndex: number,
): Promise<void> {
  await wrapper.findAll(".app-files-more")[rowIndex].trigger("click");
  await nextTick();
}

async function selectMenu(
  wrapper: ReturnType<typeof mount>,
  key: string,
): Promise<void> {
  wrapper.findComponent(NDropdown).vm.$emit("select", key);
  await flushPromises();
  await nextTick();
}

describe("ApplicationFilesTab", () => {
  it("always shows the redeploy warning", async () => {
    const wrapper = await mountTab();
    const note = wrapper.find('[data-testid="files-persistent-note"]');
    expect(note.exists()).toBe(true);
    expect(note.text()).toContain("lost when the application is redeployed");
    wrapper.unmount();
  });

  it("keeps the exec actions enabled while the container has a shell", async () => {
    const wrapper = await mountTab();
    expect(wrapper.find('[data-testid="files-exec-blocked"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="files-new-folder"]').attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });

  it("asks to start a stopped container instead of listing its files", async () => {
    capabilities.value = {
      readable: true,
      exec_available: false,
      exec_blocked_by: "not_running",
      state: "exited",
    };
    const api = await import("@/features/applications/api/containerFiles");
    vi.mocked(api.listContainerFiles).mockClear();
    const wrapper = await mountTab();
    const empty = wrapper.find('[data-testid="files-start-container"]');
    expect(empty.exists()).toBe(true);
    expect(empty.text()).toContain("Start the container to browse its files");
    expect(wrapper.find('[data-testid="files-upload"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="files-breadcrumb-root"]').exists()).toBe(false);
    expect(api.listContainerFiles).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("clears the container picker spinner in the stopped state", async () => {
    containers.value = [
      { id: "c1", name: "demo-web", image: "gotham/demo:1", state: "exited", status: "Exited" },
      { id: "c2", name: "demo-worker", image: "gotham/demo:1", state: "exited", status: "Exited" },
    ];
    capabilities.value = {
      readable: true,
      exec_available: false,
      exec_blocked_by: "not_running",
      state: "exited",
    };
    const wrapper = await mountTab();
    expect(wrapper.findComponent(NSelect).props("loading")).toBe(false);
    await wrapper.find('[data-testid="files-refresh"]').trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.findComponent(NSelect).props("loading")).toBe(false);
    expect(wrapper.find('[data-testid="files-start-container"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("switches to the start state when the listing reports a stopped container", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    vi.mocked(api.listContainerFiles).mockRejectedValueOnce({
      status: 409,
      message: "the container is not running; start it to browse its files",
      code: "not_running",
      cause: null,
    });
    const wrapper = await mountTab();
    expect(wrapper.find('[data-testid="files-start-container"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="files-error"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("loads every tar fallback page without a load more button", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    const list = vi.mocked(api.listContainerFiles);
    list.mockClear();
    list.mockResolvedValueOnce({
      directory: { path: "/", name: "/", kind: "directory", size: 0, mode: 0o755, uid: 0, gid: 0 },
      entries: [
        { path: "/app", name: "app", kind: "directory", size: 0, mode: 0o755, uid: 0, gid: 0 },
      ],
      truncated: true,
      cursor: "app",
    });
    list.mockResolvedValueOnce({
      directory: { path: "/", name: "/", kind: "directory", size: 0, mode: 0o755, uid: 0, gid: 0 },
      entries: [
        { path: "/config.yml", name: "config.yml", kind: "file", size: 12, mode: 0o644, uid: 1000, gid: 1000 },
      ],
      truncated: false,
    });
    const wrapper = await mountTab();
    expect(list).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("app");
    expect(wrapper.text()).toContain("config.yml");
    expect(wrapper.find('[data-testid="files-load-more"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Only part of this folder");
    wrapper.unmount();
  });

  it("keeps the newest folder when an older load resolves last", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    const list = vi.mocked(api.listContainerFiles);
    const wrapper = await mountTab();
    expect(wrapper.text()).toContain("config.yml");

    list.mockClear();
    const slow = deferred<ContainerFileListing>();
    const fast = deferred<ContainerFileListing>();
    list.mockReturnValueOnce(slow.promise).mockReturnValueOnce(fast.promise);

    await wrapper.findAll(".app-files-row")[0].trigger("dblclick");
    await wrapper.find('[data-testid="files-breadcrumb-root"]').trigger("click");

    fast.resolve(listing("/", [fileEntry("/config.yml")]));
    await flushPromises();
    expect(wrapper.text()).toContain("config.yml");

    slow.resolve(listing("/app", [fileEntry("/app/nested.txt")]));
    await flushPromises();
    expect(wrapper.text()).toContain("config.yml");
    expect(wrapper.text()).not.toContain("nested.txt");
    expect(wrapper.find('[data-testid="files-breadcrumb-app"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("never sends an old cursor to a newly selected container", async () => {
    containers.value = [
      { id: "c1", name: "demo-web", image: "gotham/demo:1", state: "running", status: "Up" },
      { id: "c2", name: "demo-worker", image: "gotham/demo:1", state: "running", status: "Up" },
    ];
    const wrapper = await mountTab();
    expect(wrapper.find('[data-testid="files-container-picker"]').exists()).toBe(true);

    const api = await import("@/features/applications/api/containerFiles");
    const list = vi.mocked(api.listContainerFiles);
    list.mockClear();
    const slow = deferred<ContainerFileListing>();
    list.mockReturnValueOnce(slow.promise);
    list.mockResolvedValueOnce(listing("/", [fileEntry("/c2-marker.txt")]));

    await wrapper.findAll(".app-files-row")[0].trigger("dblclick");
    wrapper.findComponent(NSelect).vm.$emit("update:value", "c2");
    await flushPromises();

    const page = listing("/app", [fileEntry("/app/c1-page-one.txt")]);
    slow.resolve({ ...page, truncated: true, cursor: "cursor-1" });
    await flushPromises();

    const calls = list.mock.calls;
    expect(calls).toContainEqual(["app-1", "c2", "/"]);
    expect(calls.some((call) => call[1] === "c2" && call[3] === "cursor-1")).toBe(false);
    expect(wrapper.text()).toContain("c2-marker.txt");
    expect(wrapper.text()).not.toContain("c1-page-one.txt");
    wrapper.unmount();
  });

  it("explains a container without a shell", async () => {
    capabilities.value = {
      readable: true,
      exec_available: false,
      exec_blocked_by: "no_shell",
      state: "running",
    };
    const wrapper = await mountTab();
    const blocked = wrapper.find('[data-testid="files-exec-blocked"]');
    expect(blocked.exists()).toBe(true);
    expect(blocked.text()).toContain("no shell");
    wrapper.unmount();
  });

  it("shows an icon and the compact columns without a text-button row", async () => {
    const wrapper = await mountTab();
    const rows = wrapper.findAll(".app-files-row");
    expect(rows).toHaveLength(2);
    expect(rows[0].text()).toContain("app");
    expect(rows[1].text()).toContain("config.yml");
    expect(wrapper.find(".app-files-cell-name .fm-mark").exists()).toBe(true);
    const text = wrapper.text();
    expect(text).toContain("644");
    expect(text).toContain("1000:1000");
    expect(text).not.toContain("Download");
    expect(text).not.toContain("Permissions");
    expect(wrapper.findAll('[data-testid^="files-action-"]')).toHaveLength(0);
    wrapper.unmount();
  });

  it("offers every action from the row menu", async () => {
    const wrapper = await mountTab();
    await openRowMenu(wrapper, 0);
    expect(menuKeys(wrapper)).toEqual([
      "open",
      "download",
      "rename",
      "copy",
      "move",
      "chmod",
      "delete",
    ]);
    await openRowMenu(wrapper, 1);
    expect(menuKeys(wrapper)).toEqual([
      "edit",
      "download",
      "rename",
      "copy",
      "move",
      "chmod",
      "delete",
    ]);
    wrapper.unmount();
  });

  it("opens the row menu on right click", async () => {
    const wrapper = await mountTab();
    await wrapper.findAll(".app-files-row")[1].trigger("contextmenu");
    await nextTick();
    expect(menuKeys(wrapper)).toContain("edit");
    expect(wrapper.findComponent(NDropdown).props("show")).toBe(true);
    wrapper.unmount();
  });

  it("disables the exec actions with the reason when exec is unavailable", async () => {
    capabilities.value = {
      readable: true,
      exec_available: false,
      exec_blocked_by: "no_shell",
      state: "running",
    };
    const wrapper = await mountTab();
    await openRowMenu(wrapper, 1);
    const options = rowMenu(wrapper);
    const writeOptions = options.filter((option) =>
      ["rename", "copy", "move", "chmod", "delete"].includes(String(option.key)),
    );
    expect(writeOptions).toHaveLength(5);
    for (const option of writeOptions) {
      expect(option.disabled).toBe(true);
      expect(String(option.props?.title)).toContain("no shell");
    }
    const edit = options.find((option) => option.key === "edit");
    expect(edit?.disabled).toBe(false);
    wrapper.unmount();
  });

  it("hides the write actions from a reader", async () => {
    const wrapper = await mountTab(false);
    expect(wrapper.find('[data-testid="files-upload"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="files-new-folder"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("Only the owner can browse and change");
    await openRowMenu(wrapper, 1);
    expect(menuKeys(wrapper)).toEqual(["edit", "download"]);
    wrapper.unmount();
  });

  it("leaves Enter and double click on the row menu button to the menu", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    const list = vi.mocked(api.listContainerFiles);
    const wrapper = await mountTab();
    list.mockClear();
    vi.mocked(api.readContainerFile).mockClear();

    await wrapper.findAll(".app-files-more")[0].trigger("keydown", { key: "Enter" });
    await flushPromises();
    await wrapper.findAll(".app-files-more")[0].trigger("dblclick");
    await flushPromises();
    await wrapper.findAll(".app-files-more")[1].trigger("dblclick");
    await flushPromises();
    expect(list).not.toHaveBeenCalled();
    expect(api.readContainerFile).not.toHaveBeenCalled();
    expect(wrapper.findComponent(CodeEditor).exists()).toBe(false);
    expect(wrapper.find('[data-testid="files-prompt-input"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="files-delete-body"]').exists()).toBe(false);

    await wrapper.findAll(".app-files-row")[0].trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(list).toHaveBeenCalledWith("app-1", "c1", "/app");
    wrapper.unmount();
  });

  it("keeps every dialog on the shared standard width", async () => {
    const editorWrapper = await mountTab();
    await openRowMenu(editorWrapper, 1);
    await selectMenu(editorWrapper, "edit");
    expect(editorWrapper.findAll(".n-modal.app-modal.standard-width")).toHaveLength(1);
    editorWrapper.unmount();

    const promptWrapper = await mountTab();
    await promptWrapper.find('[data-testid="files-new-folder"]').trigger("click");
    await nextTick();
    expect(promptWrapper.findAll(".n-modal.app-modal.standard-width")).toHaveLength(1);
    promptWrapper.unmount();

    const deleteWrapper = await mountTab();
    await openRowMenu(deleteWrapper, 1);
    await selectMenu(deleteWrapper, "delete");
    expect(deleteWrapper.findAll(".n-modal.app-modal.standard-width")).toHaveLength(1);
    deleteWrapper.unmount();
  });

  it("keeps the row menu button visible without hover", () => {
    const source = readFileSync(tabSourcePath, "utf8");
    const mediaAt = source.indexOf("@media (hover: none)");
    expect(mediaAt).toBeGreaterThan(-1);
    const rule = source.slice(mediaAt, source.indexOf("}", mediaAt));
    expect(rule).toContain(".app-files-more");
    expect(rule).toContain("opacity: 1");
  });

  it("opens a folder on double click and on Enter", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    const list = vi.mocked(api.listContainerFiles);
    const wrapper = await mountTab();
    list.mockClear();
    await wrapper.findAll(".app-files-row")[0].trigger("dblclick");
    await flushPromises();
    expect(list).toHaveBeenCalledWith("app-1", "c1", "/app");

    list.mockClear();
    await wrapper.findAll(".app-files-row")[0].trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(list).toHaveBeenCalledWith("app-1", "c1", "/app");
    wrapper.unmount();
  });

  it("shows the breadcrumb without a double slash", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    vi.mocked(api.listContainerFiles).mockResolvedValueOnce(
      listing("/app", [fileEntry("/app/main.go")]),
    );
    const wrapper = await mountTab();
    await wrapper.findAll(".app-files-row")[0].trigger("dblclick");
    await flushPromises();
    const crumbs = wrapper.find(".app-files-breadcrumb");
    expect(crumbs.text().replace(/\s+/g, "")).toBe("/app");
    expect(crumbs.text()).not.toContain("//");
    expect(wrapper.text()).toContain("main.go");
    wrapper.unmount();
  });

  it("refuses to edit a file above the text limit", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    vi.mocked(api.listContainerFiles).mockResolvedValueOnce(
      listing("/", [
        {
          path: "/big.log",
          name: "big.log",
          kind: "file",
          size: 2 * 1024 * 1024,
          mode: 0o644,
          uid: 0,
          gid: 0,
        },
      ]),
    );
    const wrapper = await mountTab();
    await wrapper.findAll(".app-files-row")[0].trigger("dblclick");
    await flushPromises();
    expect(wrapper.text()).toContain("too large");
    expect(api.readContainerFile).not.toHaveBeenCalled();
    expect(wrapper.findComponent(CodeEditor).exists()).toBe(false);
    wrapper.unmount();
  });

  it("opens a text file in the code editor and saves through the API", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    const wrapper = await mountTab();
    await openRowMenu(wrapper, 1);
    await selectMenu(wrapper, "edit");
    const editor = wrapper.findComponent(CodeEditor);
    expect(editor.exists()).toBe(true);
    expect(editor.props("modelValue")).toBe("port: 8080\n");
    expect(editor.props("language")).toBe("yaml");
    expect(codeMirror.createEditorSession).toHaveBeenCalled();

    editor.vm.$emit("update:modelValue", "port: 9090\n");
    await nextTick();
    await wrapper.find('[data-testid="files-editor-save"]').trigger("click");
    await flushPromises();
    expect(api.writeContainerFile).toHaveBeenCalledWith(
      "app-1",
      "c1",
      "/app/config.yml",
      "port: 9090\n",
    );
    expect(wrapper.findComponent(CodeEditor).exists()).toBe(false);
    wrapper.unmount();
  });

  it("renders the Vietnamese copy without a raw key", async () => {
    syncComposerLocale("vi");
    const wrapper = await mountTab();
    const note = wrapper.find('[data-testid="files-persistent-note"]');
    expect(note.text()).toContain("sẽ mất khi triển khai lại ứng dụng");
    expect(wrapper.text()).not.toContain("applications.files.");
    expect(wrapper.text()).toContain("Tệp");
    expect(wrapper.text()).toContain("Kích thước");
    expect(wrapper.text()).not.toContain("Size");
    await openRowMenu(wrapper, 1);
    const labels = rowMenu(wrapper).map((option) => String(option.label));
    expect(labels).toEqual(["Sửa", "Tải xuống", "Đổi tên", "Sao chép", "Di chuyển", "Quyền", "Xoá"]);
    syncComposerLocale("en");
    wrapper.unmount();
  });

  it("renders the start state in Vietnamese", async () => {
    syncComposerLocale("vi");
    capabilities.value = {
      readable: true,
      exec_available: false,
      exec_blocked_by: "not_running",
      state: "exited",
    };
    const wrapper = await mountTab();
    const empty = wrapper.find('[data-testid="files-start-container"]');
    expect(empty.text()).toContain("Hãy khởi động container để xem tệp của nó");
    expect(wrapper.text()).not.toContain("applications.files.");
    syncComposerLocale("en");
    wrapper.unmount();
  });

  it("refuses to edit a file that is not text", async () => {
    const api = await import("@/features/applications/api/containerFiles");
    vi.mocked(api.readContainerFile).mockResolvedValueOnce({ text: "", isText: false });
    const wrapper = await mountTab();
    await openRowMenu(wrapper, 1);
    await selectMenu(wrapper, "edit");
    expect(wrapper.text()).toContain("not a text file");
    expect(wrapper.findComponent(CodeEditor).exists()).toBe(false);
    wrapper.unmount();
  });
});
