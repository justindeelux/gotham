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
    deleteEntry: vi.fn(),
  };
});

import {
  createFolder,
  deleteEntry,
  listEntries,
  listRoots,
  renameEntry,
} from "@/features/files/api/files";
import CreateEntryDialog from "@/features/files/components/CreateEntryDialog.vue";
import DeleteEntriesDialog from "@/features/files/components/DeleteEntriesDialog.vue";
import RenameEntryDialog from "@/features/files/components/RenameEntryDialog.vue";
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
  registerDiscoveredCatalogs();
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

describe("files delete dialog (recycle bin)", () => {
  function confirmButton(
    wrapper: ReturnType<typeof mount>,
    label: string,
  ) {
    return wrapper.findAll("button").find((button) => button.text().includes(label));
  }

  it("always moves to the recycle bin and offers no permanent option", async () => {
    vi.mocked(deleteEntry).mockResolvedValue(undefined);
    const store = useFilesStore();
    await store.init("server-1");

    const wrapper = mount(
      dialogShell(DeleteEntriesDialog, {
        show: true,
        entries: [entry({ name: "app.yaml" })],
      }),
      { global: { plugins: [i18n], stubs: { teleport: true } } },
    );
    await flushPromises();

    expect(wrapper.text()).toContain("Move to recycle bin");
    expect(wrapper.text()).not.toContain("Delete permanently");

    await confirmButton(wrapper, "Move to recycle bin")!.trigger("click");
    await flushPromises();
    expect(vi.mocked(deleteEntry)).toHaveBeenLastCalledWith(
      "server-1",
      "team-1",
      "volumes",
      "app.yaml",
      false,
    );
  });

  it("reassures that a symlink's target is not removed", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    const wrapper = mount(
      dialogShell(DeleteEntriesDialog, {
        show: true,
        entries: [
          entry({ name: "current", kind: "symlink", mode: 0o120777 }),
        ],
      }),
      { global: { plugins: [i18n], stubs: { teleport: true } } },
    );
    await flushPromises();

    expect(wrapper.text()).toContain("its target is not removed");
  });
});
