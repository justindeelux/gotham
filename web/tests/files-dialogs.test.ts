// Create/Rename dialog submit guard (JUS-111 fix round 1): a fast double Enter
// must not fire two mutations. The dialogs mount under the naive-ui message
// provider with the files API mocked; teleport is stubbed so the modal stays
// inside the wrapper.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    createFolder: vi.fn(),
    createFile: vi.fn(),
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    renameEntry: vi.fn(),
  };
});

import {
  createFolder,
  listEntries,
  listRoots,
  renameEntry,
} from "@/features/files/api/files";
import CreateEntryDialog from "@/features/files/components/CreateEntryDialog.vue";
import RenameEntryDialog from "@/features/files/components/RenameEntryDialog.vue";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useTeamsStore } from "@/features/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

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

/** deferred exposes the resolve of a promise. */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function dialogShell(component: object, props: Record<string, unknown>) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(component as never, props),
      });
    },
  });
}

beforeEach(() => {
  setActivePinia(createPinia());
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
    entries: [],
    truncated: false,
    cursor: "",
  });
});

describe("files create/rename dialogs", () => {
  it("creates once on a fast double Enter", async () => {
    const gate = deferred<FilesEntry>();
    vi.mocked(createFolder).mockImplementation(() => gate.promise);

    const wrapper = mount(
      dialogShell(CreateEntryDialog, { show: true, kind: "directory" }),
      { global: { plugins: [i18n], stubs: { teleport: true } } },
    );
    await flushPromises();

    const input = wrapper.find("input");
    await input.setValue("uploads");
    input.trigger("keydown.enter");
    input.trigger("keydown.enter");
    await flushPromises();

    expect(vi.mocked(createFolder)).toHaveBeenCalledTimes(1);
    gate.resolve(entry({ name: "uploads", kind: "directory", mode: 0o040755 }));
    await flushPromises();
  });

  it("renames once on a fast double Enter", async () => {
    const target = entry({ name: "app.yaml" });
    const gate = deferred<FilesEntry>();
    vi.mocked(renameEntry).mockImplementation(() => gate.promise);

    const wrapper = mount(
      dialogShell(RenameEntryDialog, { show: true, entry: target }),
      { global: { plugins: [i18n], stubs: { teleport: true } } },
    );
    await flushPromises();

    const input = wrapper.find("input");
    await input.setValue("main.yaml");
    input.trigger("keydown.enter");
    input.trigger("keydown.enter");
    await flushPromises();

    expect(vi.mocked(renameEntry)).toHaveBeenCalledTimes(1);
    gate.resolve(entry({ name: "main.yaml" }));
    await flushPromises();
  });
});
