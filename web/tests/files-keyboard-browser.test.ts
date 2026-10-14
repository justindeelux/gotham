// JUS-139 fix round 1, browser-level: mounting the real browser proves the row
// key handler keeps exactly one roving tab stop when the roving row leaves the
// listing (folder change), resets the type-to-focus prefix on a listing change,
// clears its type-to-focus timer on unmount, and seeds the range anchor at the
// focused row when Shift+Arrow starts with no anchor. The files API and
// useMessage are mocked, so the component runs under a real pinia + i18n.
/* global document:readonly, HTMLElement:readonly */
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

/** tabStops is the rendered name controls that own the roving tab stop. */
function tabStops(wrapper: VueWrapper) {
  return wrapper.findAll('.fm-label[tabindex="0"]');
}

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

describe("row keyboard navigation lifecycle", () => {
  it("re-resolves the roving stop on a folder change", async () => {
    const wrapper = await mountBrowser();
    const store = useFilesStore();

    await wrapper
      .find('.fm-row[data-entry="alpha"] .fm-label')
      .trigger("keydown", { key: "ArrowDown" });
    await flushPromises();
    expect(tabStops(wrapper)).toHaveLength(1);
    expect(tabStops(wrapper)[0].text()).toContain("beta");

    // Navigate into a folder that also holds a "beta": the previous stop must
    // not survive onto the same-named entry.
    vi.mocked(listEntries).mockResolvedValue({
      entries: [entry({ name: "beta" }), entry({ name: "alpha" })],
      truncated: false,
      cursor: "",
    });
    await store.openDirectory("alpha");
    await flushPromises();

    expect(tabStops(wrapper)).toHaveLength(1);
    expect(tabStops(wrapper)[0].text()).toContain("alpha");
  });

  it("resets the type-to-focus prefix on a folder change", async () => {
    const wrapper = await mountBrowser();
    const store = useFilesStore();

    const first = wrapper.find('.fm-row[data-entry="alpha"] .fm-label');
    (first.element as HTMLElement).focus();
    await first.trigger("keydown", { key: "g" });
    await flushPromises();
    expect(tabStops(wrapper)[0].text()).toContain("gamma");

    // A folder change must drop the "g" prefix: the next key is a fresh run.
    await store.openDirectory("alpha");
    await flushPromises();
    const nextFirst = wrapper.find('.fm-row[data-entry="alpha"] .fm-label');
    (nextFirst.element as HTMLElement).focus();
    await nextFirst.trigger("keydown", { key: "b" });
    await flushPromises();
    // A stale "gb" would match nothing and leave the focus where it was.
    expect(tabStops(wrapper)[0].text()).toContain("beta");
  });

  it("clears the pending type-to-focus timer on unmount", async () => {
    const setSpy = vi.spyOn(globalThis, "setTimeout");
    const clearSpy = vi.spyOn(globalThis, "clearTimeout");

    const wrapper = await mountBrowser();
    const label = wrapper.find('.fm-row[data-entry="alpha"] .fm-label');
    (label.element as HTMLElement).focus();
    await label.trigger("keydown", { key: "b" });

    const armed = setSpy.mock.results.at(-1)?.value;
    expect(armed).toBeDefined();

    wrapper.unmount();
    expect(clearSpy.mock.calls.some((call) => call[0] === armed)).toBe(true);
  });

  it("seeds the range anchor at the focused row when Shift+Arrow starts with none", async () => {
    const wrapper = await mountBrowser();
    const store = useFilesStore();
    expect(store.activeTab.anchor).toBeNull();

    const label = wrapper.find('.fm-row[data-entry="alpha"] .fm-label');
    (label.element as HTMLElement).focus();
    await label.trigger("keydown", { key: "ArrowDown", shiftKey: true });
    await flushPromises();

    // The run includes the origin row (alpha), not just the target (beta).
    expect(store.selectedNames.sort()).toEqual(["alpha", "beta"]);
  });
});
