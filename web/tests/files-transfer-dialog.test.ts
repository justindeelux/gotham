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

vi.mock("@/features/files/api/transfer", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/transfer")>();
  return {
    ...actual,
    planTransfer: vi.fn(),
    streamTransfer: vi.fn(),
  };
});

import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";

import { listEntries, listRoots } from "@/features/files/api/files";
import { planTransfer } from "@/features/files/api/transfer";
import ServerFilesPage from "@/features/files/pages/ServerFilesPage.vue";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import type { TransferPlan } from "@/features/files/schemas/transfer";
import { filesPane } from "@/features/files/stores/filesPane";
import { useFilesTransferStore } from "@/features/files/stores/transfer";
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

const plan: TransferPlan = {
  mode: "bundle",
  bounds: {
    max_bundle_bytes: 1073741824,
    max_expanded_bytes: 4294967296,
    max_entries: 100000,
    max_sources: 1024,
    max_concurrency: 4,
    bundle_available: true,
  },
  sources: [{ name: "server-1.txt", kind: "file", size: 0, conflict: false }],
  conflicts: [],
  unsupported: [],
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
  vi.mocked(planTransfer).mockResolvedValue(plan);
});

describe("copy to the other pane", () => {
  it("captures both pane values and keeps them after the panes move", async () => {
    const { wrapper } = await mountPage();
    const left = filesPane("left").files;
    await left.openAt("volumes", "reports");
    await flushPromises();

    await wrapper.get('button[aria-label="Split view"]').trigger("click");
    await flushPromises();
    const right = filesPane("right").files;
    const select = wrapper.findComponent(
      '.files-browser[data-pane="right"] .fm-node__select',
    );
    select.vm.$emit("update:value", "server-2");
    await flushPromises();
    await right.openAt("volumes", "backups");
    await flushPromises();

    left.selectOnly("server-1.txt");
    await flushPromises();

    const leftBrowser = wrapper.find('.files-browser[data-pane="left"]');
    const button = leftBrowser.find('button[data-action="copy-to-pane"]');
    expect(button.exists()).toBe(true);
    await button.trigger("click");
    await flushPromises();

    const dialog = wrapper.find(".transfer-modal");
    expect(dialog.exists()).toBe(true);
    expect(dialog.text()).toContain("node-1");
    expect(dialog.text()).toContain("reports");
    expect(dialog.text()).toContain("node-2");
    expect(dialog.text()).toContain("backups");
    expect(dialog.text()).toContain("server-1.txt");

    // Moving the destination pane after the capture must not retarget the run.
    await right.openAt("volumes", "moved");
    await flushPromises();
    const after = wrapper.find(".transfer-modal").text();
    expect(after).toContain("backups");
    expect(after).not.toContain("moved");
  });
});

describe("copy to a node", () => {
  it("offers the node action with a human-readable destination form", async () => {
    const { wrapper } = await mountPage();
    const left = filesPane("left").files;
    left.selectOnly("server-1.txt");
    await flushPromises();

    const leftBrowser = wrapper.find('.files-browser[data-pane="left"]');
    const operations = leftBrowser
      .findAll("button")
      .find((item) => item.attributes("aria-label") === "File operations");
    expect(operations).toBeTruthy();
    await operations!.trigger("click");
    await flushPromises();

    const copyToNode = wrapper
      .findAll(".n-dropdown-option-body")
      .find((item) => item.text().includes("Copy to node"));
    expect(copyToNode).toBeTruthy();

    useFilesTransferStore().openNodeCopy({
      serverId: "server-1",
      pane: "left",
      root: "volumes",
      path: "",
      names: ["server-1.txt"],
      sizes: { "server-1.txt": 0 },
      directories: [],
    });
    await flushPromises();

    await vi.waitFor(
      () => {
        expect(useFilesTransferStore().dialogOpen).toBe(true);
      },
      { timeout: 5000 },
    );
    await vi.waitFor(
      () => {
        expect(wrapper.find(".transfer-modal").exists()).toBe(true);
      },
      { timeout: 5000 },
    );
    const dialog = wrapper.find(".transfer-modal");
    expect(dialog.text()).toContain("Destination");
    expect(dialog.text()).toContain("server-1.txt");
  });
});
