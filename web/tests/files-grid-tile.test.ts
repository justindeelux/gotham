/* global document:readonly */
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
import { nextTick } from "vue";

import { listEntries, listRoots } from "@/features/files/api/files";
import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
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

function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
  return {
    kind: "file",
    size: 0,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

const rootEntries: FilesEntry[] = [
  entry({
    name: "app.yaml",
    size: 2458,
    owner: "root",
    modified_at: new Date(Date.now() - 3 * 86_400_000).toISOString(),
  }),
  entry({ name: "config", kind: "directory", size: 0, mode: 0o040755 }),
];

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue({
    entries: rootEntries,
    truncated: false,
    cursor: "",
  });
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

async function mountGrid(): Promise<VueWrapper> {
  await useFilesStore().init("server-1");
  const wrapper = mount(FilesBrowser, {
    props: { serverId: "server-1" },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
  await flushPromises();
  useFilesStore().setViewMode("grid");
  await nextTick();
  await flushPromises();
  return wrapper;
}

function tile(wrapper: VueWrapper, name: string) {
  return wrapper.find(`.fm-tile-wrap[data-entry="${name}"]`);
}

describe("grid tile layout", () => {
  it("puts the icon before the name on top with the age in the checkbox row", async () => {
    const wrapper = await mountGrid();
    const top = tile(wrapper, "app.yaml").find(".fm-tile-top");
    expect(top.exists()).toBe(true);
    expect(top.find(".fm-mark").exists()).toBe(true);
    expect(top.find(".fm-tile-name b").text()).toBe("app.yaml");
    expect(top.find(".fm-tile-ago").exists()).toBe(false);
    const bottom = tile(wrapper, "app.yaml").find(".fm-tile-bottom");
    expect(bottom.exists()).toBe(true);
    expect(bottom.find(".fm-tile-ago").exists()).toBe(false);
    const foot = tile(wrapper, "app.yaml").find(".fm-tile-foot");
    expect(foot.exists()).toBe(true);
    const ago = foot.find(".fm-tile-ago").text();
    expect(ago.length).toBeGreaterThan(0);
    expect(ago).not.toMatch(/\d{4}-\d{2}-\d{2}/);
    expect(tile(wrapper, "app.yaml").find(".fm-tile-check").exists()).toBe(true);
  });

  it("shows type, owner/mode and size under the icon for a file", async () => {
    const wrapper = await mountGrid();
    const lines = tile(wrapper, "app.yaml").find(".fm-tile-lines");
    expect(lines.find(".fm-tile-type").text()).toContain("YAML");
    expect(lines.find(".fm-tile-ownermode").text()).toBe("root / 644");
    expect(lines.find(".fm-tile-size").text()).toContain("KiB");
  });

  it("shows no size line for a directory", async () => {
    const wrapper = await mountGrid();
    const lines = tile(wrapper, "config").find(".fm-tile-lines");
    expect(lines.find(".fm-tile-type").text()).toBe("Folder");
    expect(lines.find(".fm-tile-ownermode").text()).toBe("755");
    expect(lines.find(".fm-tile-size").exists()).toBe(false);
  });

  it("keeps the checkbox as a bare label in the tile", async () => {
    const wrapper = await mountGrid();
    const wrap = tile(wrapper, "app.yaml");
    const check = wrap.find(".fm-tile-check");
    expect(check.exists()).toBe(true);
    expect(check.element.tagName).toBe("LABEL");
    expect(
      check.find('[aria-label="Select app.yaml"]').exists(),
    ).toBe(true);
  });
});
