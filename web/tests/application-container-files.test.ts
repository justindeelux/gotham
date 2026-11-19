import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";

import type {
  ContainerFileCapabilities,
  ContainerFileEntry,
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
  if (typeof window === "undefined") {
    return;
  }
  if (!window.matchMedia) {
    window.matchMedia = (() => ({
      matches: false,
      media: "",
      addEventListener: () => {},
      removeEventListener: () => {},
    })) as unknown as typeof window.matchMedia;
  }
  if (!window.ResizeObserver) {
    class ResizeObserverStub {
      constructor(
        private readonly callback: (entries: Array<{
          target: Element;
          contentRect: { width: number; height: number };
        }>) => void,
      ) {}

      observe(target: Element): void {
        this.callback([{ target, contentRect: { width: 1024, height: 420 } }]);
      }

      unobserve(): void {}

      disconnect(): void {}
    }
    window.ResizeObserver =
      ResizeObserverStub as unknown as typeof window.ResizeObserver;
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
