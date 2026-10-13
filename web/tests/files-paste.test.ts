// Paste feedback (JUS-111 fix round 2): the toolbar paste action must report the
// transfer outcome instead of silently no-opping. useMessage is stubbed and the
// files API is mocked, so the browser runs under a real pinia + i18n.
import { beforeEach, describe, expect, it, vi } from "vitest";

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
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    createFile: vi.fn(),
    createFolder: vi.fn(),
    renameEntry: vi.fn(),
    moveEntry: vi.fn(),
    copyEntry: vi.fn(),
    deleteEntry: vi.fn(),
  };
});

import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";

import { listEntries, listRoots, moveEntry } from "@/features/files/api/files";
import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";
import { i18n, registerDiscoveredCatalogs, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

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
    entries: [
      entry({ name: "app.yaml" }),
      entry({ name: "data", kind: "directory", size: 0, mode: 0o040755 }),
    ],
    truncated: false,
    cursor: "",
  });
});

describe("files browser paste feedback", () => {
  it("toasts the transfer summary after a cut paste", async () => {
    vi.mocked(moveEntry).mockResolvedValue(entry({ name: "app.yaml" }));

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    await store.openDirectory("data");

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const pasteButton = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Paste"));
    expect(pasteButton).toBeTruthy();
    await pasteButton!.trigger("click");
    await flushPromises();

    expect(vi.mocked(moveEntry)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "app.yaml",
      destination: "data/app.yaml",
      overwrite: false,
    });
    expect(messages.success).toHaveBeenCalledWith("1 moved.");
  });

  it("disables paste when the current folder is not a valid destination", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const pasteButton = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Paste"));
    expect(pasteButton).toBeTruthy();
    expect(pasteButton!.attributes("disabled")).toBeDefined();
    expect(store.pasteProblem).toBe("same-folder");
  });

  it("hides paste when the clipboard is empty", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    expect(store.hasClipboard).toBe(false);
    expect(
      wrapper.findAll("button").find((button) => button.text().includes("Paste")),
    ).toBeUndefined();
  });

  it("refuses a cut paste while the source has unsaved editor changes", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    await store.openDirectory("data");

    const editor = useEditorStore();
    const dirty: EditorTab = {
      id: "t1",
      root: "volumes",
      path: "app.yaml",
      name: "app.yaml",
      doc: "changed",
      original: "start",
      etag: '"1"',
      eol: "\n",
      bom: false,
      size: 6,
      mode: 0o100644,
      language: "yaml",
      status: "ready",
      error: "",
      saving: false,
    };
    editor.tabs = [dirty];

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const pasteButton = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Paste"));
    expect(pasteButton).toBeTruthy();
    await pasteButton!.trigger("click");
    await flushPromises();

    // The move is refused before the agent call and the cut stays on the
    // clipboard so the user can save the buffer and paste again.
    expect(vi.mocked(moveEntry)).not.toHaveBeenCalled();
    expect(messages.warning).toHaveBeenCalledTimes(1);
    expect(store.hasClipboard).toBe(true);
  });
});
