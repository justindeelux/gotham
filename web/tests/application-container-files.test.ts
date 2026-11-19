import { flushPromises, mount } from "@vue/test-utils";
import { NSelect } from "naive-ui";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";

import type {
  ContainerFileCapabilities,
  ContainerFileEntry,
  ContainerFileListing,
} from "@/features/applications/api/containerFiles";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

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

const capabilities = vi.hoisted(() => ({
  value: null as ContainerFileCapabilities | null,
}));

vi.mock("@/features/applications/api/containers", () => ({
  listApplicationContainers: vi.fn(async () => [
    { id: "c1", name: "demo-web", image: "gotham/demo:1", state: "exited", status: "Exited" },
  ]),
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
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  capabilities.value = {
    readable: true,
    exec_available: true,
    state: "running",
  };
});

async function mountTab() {
  const wrapper = mount(ApplicationFilesTab, {
    props: {
      appId: "app-1",
      canWrite: true,
    },
    global: { plugins: [i18n] },
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

    await widgetByTestId(wrapper, "files-action-open")[0].trigger("click");
    await widgetByTestId(wrapper, "files-breadcrumb-root")[0].trigger("click");

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
    const containers = await import("@/features/applications/api/containers");
    vi.mocked(containers.listApplicationContainers).mockResolvedValueOnce([
      { id: "c1", name: "demo-web", image: "gotham/demo:1", state: "running", status: "Up" },
      { id: "c2", name: "demo-worker", image: "gotham/demo:1", state: "running", status: "Up" },
    ]);
    const wrapper = await mountTab();
    expect(wrapper.find('[data-testid="files-container-picker"]').exists()).toBe(true);

    const api = await import("@/features/applications/api/containerFiles");
    const list = vi.mocked(api.listContainerFiles);
    list.mockClear();
    const slow = deferred<ContainerFileListing>();
    list.mockReturnValueOnce(slow.promise);
    list.mockResolvedValueOnce(listing("/", [fileEntry("/c2-marker.txt")]));

    await widgetByTestId(wrapper, "files-action-open")[0].trigger("click");
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

  it("shows the folder and file rows", async () => {
    const wrapper = await mountTab();
    const text = wrapper.text();
    expect(text).toContain("app");
    expect(text).toContain("config.yml");
    expect(text).toContain("644");
    wrapper.unmount();
  });

  it("renders the Vietnamese copy without a raw key", async () => {
    syncComposerLocale("vi");
    const wrapper = await mountTab();
    const note = wrapper.find('[data-testid="files-persistent-note"]');
    expect(note.text()).toContain("sẽ mất khi triển khai lại ứng dụng");
    expect(wrapper.text()).not.toContain("applications.files.");
    expect(wrapper.text()).toContain("Tệp");
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
    await widgetByTestId(wrapper, "files-action-edit")[0].trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("not a text file");
    expect(wrapper.find('[data-testid="files-editor-input"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("hides the write controls for a reader", async () => {
    const wrapper = mount(ApplicationFilesTab, {
      props: { appId: "app-1", canWrite: false },
      global: { plugins: [i18n] },
    });
    await nextTick();
    await flushPromises();
    await nextTick();
    expect(wrapper.find('[data-testid="files-upload"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="files-new-folder"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("Only the owner can browse and change");
    wrapper.unmount();
  });
});

function widgetByTestId(
  wrapper: ReturnType<typeof mount>,
  testId: string,
): ReturnType<typeof wrapper.findAll> {
  return wrapper.findAll(`[data-testid="${testId}"]`);
}

void i18n;
