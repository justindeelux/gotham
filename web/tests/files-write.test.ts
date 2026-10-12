// Files write surface (P18 step 7): create-over-existing, batch delete with a
// partial failure, and the clipboard 409 replace/skip flow. The API module is
// mocked so the store runs without axios or the DOM.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    createFolder: vi.fn(),
    createFile: vi.fn(),
    renameEntry: vi.fn(),
    moveEntry: vi.fn(),
    copyEntry: vi.fn(),
    deleteEntry: vi.fn(),
  };
});

import {
  copyEntry,
  createFile,
  createFolder,
  deleteEntry,
  listEntries,
  listRoots,
  moveEntry,
  renameEntry,
} from "@/features/files/api/files";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

const composeRoot: FilesRoot = {
  name: "compose",
  path: "/var/lib/gotham-agent/compose",
  writable: false,
  free_bytes: 0,
};

/** entry builds one listing row with sensible defaults. */
function entry(
  overrides: Partial<FilesEntry> & { name: string },
): FilesEntry {
  return {
    kind: "file",
    size: 1024,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

const conflict = { message: "conflict", status: 409 };

beforeEach(() => {
  setActivePinia(createPinia());
  for (const fn of [listRoots, listEntries, createFolder, createFile, copyEntry, moveEntry, deleteEntry, renameEntry]) {
    vi.mocked(fn).mockReset();
  }
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue({
    entries: [
      entry({ name: "app.yaml" }),
      entry({ name: "config", kind: "directory", size: 0, mode: 0o040755 }),
      entry({ name: "data", kind: "directory", size: 0, mode: 0o040755 }),
    ],
    truncated: false,
    cursor: "",
  });
});

describe("files store create", () => {
  it("fails a create over an existing name and keeps the listing", async () => {
    vi.mocked(createFolder).mockRejectedValue(conflict);

    const store = useFilesStore();
    await store.init("server-1");
    const before = store.entries.map((item) => item.name);

    await expect(store.createEntry("config", "directory")).rejects.toMatchObject({
      status: 409,
    });

    expect(vi.mocked(createFolder)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "config",
    });
    expect(store.entries.map((item) => item.name)).toEqual(before);
    expect(store.acting).toBe(false);
  });

  it("trims the submitted name and creates exclusively", async () => {
    vi.mocked(createFolder).mockResolvedValue(
      entry({ name: "uploads", kind: "directory", mode: 0o040755 }),
    );

    const store = useFilesStore();
    await store.init("server-1");
    await store.createEntry("  uploads  ", "directory");

    expect(vi.mocked(createFolder)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "uploads",
    });
  });
});

describe("files store delete", () => {
  it("reports a per-name partial failure instead of aborting", async () => {
    vi.mocked(deleteEntry)
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(conflict);

    const store = useFilesStore();
    await store.init("server-1");
    const summary = await store.remove([
      entry({ name: "app.yaml" }),
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
    ]);

    expect(summary.done).toEqual(["app.yaml"]);
    expect(summary.failed).toHaveLength(1);
    expect(summary.failed[0]?.name).toBe("config");
    expect(summary.failed[0]?.error).toBeTruthy();
    // A folder is deleted recursively; a file is not.
    expect(vi.mocked(deleteEntry)).toHaveBeenNthCalledWith(
      2,
      "server-1",
      "team-1",
      "volumes",
      "config",
      true,
    );
  });
});

describe("files store clipboard", () => {
  it("opens the replace/skip prompt on a 409 and retries with overwrite", async () => {
    vi.mocked(copyEntry).mockRejectedValueOnce(conflict).mockResolvedValueOnce(
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
    );

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("config");
    store.copySelection();
    expect(store.hasClipboard).toBe(true);
    expect(store.clipboardMode).toBe("copy");
    // Paste into a different directory: the same directory is a no-op.
    await store.openDirectory("data");

    const summary = await store.paste();
    expect(summary.conflicts).toEqual(["config"]);
    expect(store.conflictPrompt?.names).toEqual(["config"]);
    expect(vi.mocked(copyEntry)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "config",
      destination: "data/config",
      overwrite: false,
    });

    const resolved = await store.resolveConflictPrompt(true);
    expect(resolved.done).toEqual(["config"]);
    expect(store.conflictPrompt).toBeNull();
    expect(vi.mocked(copyEntry)).toHaveBeenLastCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "config",
      destination: "data/config",
      overwrite: true,
    });
  });

  it("skips conflicting names and leaves the destination untouched", async () => {
    vi.mocked(copyEntry).mockRejectedValue(conflict);

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("config");
    store.copySelection();
    await store.openDirectory("data");

    await store.paste();
    const resolved = await store.resolveConflictPrompt(false);

    expect(resolved.done).toEqual([]);
    expect(store.conflictPrompt).toBeNull();
    // Only the initial, non-overwriting attempt ran.
    expect(vi.mocked(copyEntry)).toHaveBeenCalledTimes(1);
  });

  it("consumes a cut clipboard once the move lands", async () => {
    vi.mocked(moveEntry).mockResolvedValue(entry({ name: "app.yaml" }));

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    expect(store.clipboardMode).toBe("cut");
    await store.openDirectory("data");

    await store.paste();
    expect(store.hasClipboard).toBe(false);
    expect(vi.mocked(moveEntry)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "app.yaml",
      destination: "data/app.yaml",
      overwrite: false,
    });
  });

  it("no-ops a paste into the clipboard's own directory", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();

    const summary = await store.paste();

    expect(summary).toEqual({ done: [], conflicts: [], failed: [] });
    expect(vi.mocked(moveEntry)).not.toHaveBeenCalled();
    // The clip survives: the same-dir paste changed nothing.
    expect(store.hasClipboard).toBe(true);
  });

  it("keeps a cut entry that failed and only drops the ones that moved", async () => {
    vi.mocked(moveEntry)
      .mockResolvedValueOnce(entry({ name: "app.yaml" }))
      .mockRejectedValueOnce(new Error("boom"));

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.toggleSelection("config");
    store.cutSelection();
    await store.openDirectory("data");

    const summary = await store.paste();

    expect(summary.done).toEqual(["app.yaml"]);
    expect(summary.failed).toHaveLength(1);
    // The failed name stays on the clipboard for a later retry.
    expect(store.clipboard?.names).toEqual(["config"]);
  });

  it("keeps skipped cut names on the clipboard", async () => {
    vi.mocked(moveEntry).mockRejectedValue(conflict);

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("config");
    store.cutSelection();
    await store.openDirectory("data");

    await store.paste();
    await store.resolveConflictPrompt(false);

    expect(store.hasClipboard).toBe(true);
    expect(store.clipboard?.names).toEqual(["config"]);
  });
});

describe("files store selection hygiene", () => {
  it("drops selected names that vanish on refresh", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    expect(store.selectedNames).toEqual(["app.yaml"]);

    vi.mocked(listEntries).mockResolvedValue({
      entries: [entry({ name: "config", kind: "directory", size: 0, mode: 0o040755 })],
      truncated: false,
      cursor: "",
    });
    await store.refresh();

    expect(store.selectedNames).toEqual([]);
  });

  it("follows a renamed entry in the selection and clipboard", async () => {
    vi.mocked(renameEntry).mockResolvedValue(entry({ name: "main.yaml" }));

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();

    vi.mocked(listEntries).mockResolvedValue({
      entries: [entry({ name: "main.yaml" })],
      truncated: false,
      cursor: "",
    });
    await store.rename(entry({ name: "app.yaml" }), "main.yaml");

    expect(store.selectedNames).toEqual(["main.yaml"]);
    expect(store.clipboard?.names).toEqual(["main.yaml"]);
  });

  it("prunes a deleted name from the clipboard", async () => {
    vi.mocked(deleteEntry).mockResolvedValue(undefined);

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();

    await store.remove([entry({ name: "app.yaml" })]);

    expect(store.hasClipboard).toBe(false);
  });

  it("removes a symlink with a plain unlink (no false success)", async () => {
    vi.mocked(deleteEntry).mockResolvedValue(undefined);

    const store = useFilesStore();
    await store.init("server-1");
    const link = entry({ name: "current", kind: "symlink", size: 0, mode: 0o120777 });

    const summary = await store.remove([link]);

    expect(summary.done).toEqual(["current"]);
    expect(vi.mocked(deleteEntry)).toHaveBeenCalledWith(
      "server-1",
      "team-1",
      "volumes",
      "current",
      false,
    );
  });

  it("does not retarget the clipboard on a rename in another directory", async () => {
    vi.mocked(renameEntry).mockResolvedValue(entry({ name: "c.txt" }));

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    await store.openDirectory("data");

    await store.rename(entry({ name: "app.yaml" }), "c.txt");

    // The clipboard still points at the root's app.yaml, never data/c.txt.
    expect(store.clipboard?.root).toBe("volumes");
    expect(store.clipboard?.path).toBe("");
    expect(store.clipboard?.names).toEqual(["app.yaml"]);
  });

  it("does not clear the clipboard on a delete in another directory", async () => {
    vi.mocked(deleteEntry).mockResolvedValue(undefined);

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    await store.openDirectory("data");

    await store.remove([entry({ name: "app.yaml" })]);

    expect(store.hasClipboard).toBe(true);
    expect(store.clipboard?.path).toBe("");
    expect(store.clipboard?.names).toEqual(["app.yaml"]);
  });
});

describe("files store paste validation", () => {
  it("refuses a cross-root paste and keeps the clipboard", async () => {
    vi.mocked(listRoots).mockResolvedValue([root, composeRoot]);

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    await store.setRoot("compose");

    expect(store.pasteProblem).toBe("cross-root");
    const summary = await store.paste();

    expect(summary).toEqual({ done: [], conflicts: [], failed: [] });
    expect(vi.mocked(moveEntry)).not.toHaveBeenCalled();
    expect(store.hasClipboard).toBe(true);
  });

  it("refuses pasting a folder into its own subtree", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("config");
    store.cutSelection();
    await store.openDirectory("config");

    expect(store.pasteProblem).toBe("into-itself");
    await store.paste();

    expect(vi.mocked(moveEntry)).not.toHaveBeenCalled();
  });
});

describe("files store row targets", () => {
  it("acts on the single row when it is not selected and the selection when it is", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    const app = entry({ name: "app.yaml" });
    const config = entry({ name: "config", kind: "directory", size: 0, mode: 0o040755 });

    // Not selected: the row itself.
    expect(store.targetsForRow(app).map((item) => item.name)).toEqual(["app.yaml"]);

    store.toggleSelection("app.yaml");
    store.toggleSelection("config");
    // Selected: the whole selection, including the row (no toggle-off).
    expect(store.targetsForRow(app).map((item) => item.name).sort()).toEqual([
      "app.yaml",
      "config",
    ]);
    expect(store.targetsForRow(config).map((item) => item.name).sort()).toEqual([
      "app.yaml",
      "config",
    ]);
  });

  it("stages a cut for an explicit row without mutating the selection", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("config");

    const app = entry({ name: "app.yaml" });
    store.cutSelection(store.targetsForRow(app).map((item) => item.name));

    expect(store.clipboardMode).toBe("cut");
    expect(store.clipboard?.names).toEqual(["app.yaml"]);
    // The pre-existing selection is untouched.
    expect(store.selectedNames).toEqual(["config"]);
  });
});
