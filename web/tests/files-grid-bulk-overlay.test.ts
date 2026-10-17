// JUS-208 round 2: mounting the real browser proves the grid bulk overlay's
// inert contract — idle the overlay is inert (out of the a11y tree and tab
// order) while the toolbar scroll content stays interactive; with a selection
// the covered content goes inert and the overlay carries the controls; on
// clear both return. The files API and useMessage are mocked, so the component
// runs under a real pinia + i18n.
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
import { useTeamsStore } from "@/features/teams";
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

/** entry builds one listing row with sensible defaults. */
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

/** rootEntries sort to alpha (dir), beta, gamma. */
const rootEntries: FilesEntry[] = [
  entry({ name: "alpha", kind: "directory", size: 0, mode: 0o040755 }),
  entry({ name: "beta" }),
  entry({ name: "gamma" }),
];

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [
    { id: "team-1", name: "A", is_personal: true, role: "owner" },
  ];
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

/** mountBrowser mounts the browser against the mocked store and settles it. */
async function mountBrowser(): Promise<VueWrapper> {
  // Bind the store before mount so onMounted's init does not reset the tab
  // (which would fire the active-tab refocus watcher; jsdom has no
  // scrollIntoView).
  await useFilesStore().init("server-1");
  const wrapper = mount(FilesBrowser, {
    props: { serverId: "server-1" },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
  await flushPromises();
  return wrapper;
}

describe("grid bulk overlay inert contract", () => {
  it("inerts the overlay while idle and the covered content on select", async () => {
    const wrapper = await mountBrowser();
    const store = useFilesStore();
    store.setViewMode("grid");
    await nextTick();

    const bulk = () => wrapper.find(".fm-grid-bulk");
    const scroller = () => wrapper.find(".fm-toolbar-scroll");
    expect(bulk().exists()).toBe(true);
    expect(bulk().attributes("inert")).toBeDefined();
    expect(scroller().attributes("inert")).toBeUndefined();

    store.selectOnly("beta");
    await nextTick();
    expect(bulk().attributes("inert")).toBeUndefined();
    expect(scroller().attributes("inert")).toBeDefined();

    store.clearSelection();
    await nextTick();
    expect(bulk().attributes("inert")).toBeDefined();
    expect(scroller().attributes("inert")).toBeUndefined();
  });
});
