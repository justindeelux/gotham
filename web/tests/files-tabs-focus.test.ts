// Files browser tab-close focus (JUS-207): closing an INACTIVE tab must not
// arm the close refocus, so a later + switch from outside the tablist leaves
// keyboard focus where it was; closing the ACTIVE tab still moves focus to
// the tab that takes over. Mounts the real browser so the activeTabId watcher
// runs; the files API and useMessage are mocked.
/* global document:readonly, HTMLElement:readonly, window:readonly */
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

import { nextTick } from "vue";
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

const rootEntries: FilesEntry[] = [entry({ name: "alpha" })];

const scrollIntoView = vi.fn();

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  // jsdom has no scrollIntoView; focusActiveTab is the only caller, so the
  // spy also proves which test ran the refocus path.
  window.HTMLElement.prototype.scrollIntoView = scrollIntoView;
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

/** settle flushes the store promises and the watcher's chained nextTicks. */
async function settle(): Promise<void> {
  for (let index = 0; index < 5; index += 1) {
    await flushPromises();
    await nextTick();
  }
}

/**
 * mountTwoTabs inits the store, opens a second tab and mounts the browser:
 * tabs [A, B] with B active and DOM focus on <body> (outside the tablist).
 */
async function mountTwoTabs(): Promise<VueWrapper> {
  const store = useFilesStore();
  await store.init("server-1");
  await store.openTab();
  expect(store.tabs).toHaveLength(2);
  const wrapper = mount(FilesBrowser, {
    props: { serverId: "server-1" },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
  await settle();
  expect(document.activeElement).toBe(document.body);
  return wrapper;
}

describe("tab-close focus (JUS-207)", () => {
  it("closing an inactive tab then pressing + leaves focus where it was", async () => {
    const wrapper = await mountTwoTabs();
    const store = useFilesStore();
    const activeBefore = store.activeTabId;

    // Close the inactive tab A via its ×.
    await wrapper.find(".fm-tab:not(.is-active) .fm-tab-close").trigger("click");
    await settle();
    expect(store.tabs).toHaveLength(1);
    expect(store.activeTabId).toBe(activeBefore);

    // Press + : the new tab activates, but focus must not follow it in.
    await wrapper.find(".fm-tab-add").trigger("click");
    await settle();
    expect(store.tabs).toHaveLength(2);
    expect(store.activeTabId).not.toBe(activeBefore);
    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(document.body);
    expect(document.activeElement?.getAttribute("role")).not.toBe("tab");
  });

  it("closing the active tab moves focus to the tab that takes over", async () => {
    const wrapper = await mountTwoTabs();

    // Keyboard focus sits on the active tab, as if the user tabbed there.
    (wrapper.find(".fm-tab.is-active").element as HTMLElement).focus();
    expect(document.activeElement?.getAttribute("aria-selected")).toBe("true");

    await wrapper.find(".fm-tab.is-active .fm-tab-close").trigger("click");
    await settle();

    // The neighbour takes over and receives DOM focus (existing behaviour).
    const focused = document.activeElement as HTMLElement | null;
    expect(scrollIntoView).toHaveBeenCalled();
    expect(focused?.getAttribute("role")).toBe("tab");
    expect(focused?.getAttribute("aria-selected")).toBe("true");
    expect(wrapper.find(".fm-tab.is-active").element).toBe(focused);
  });
});
