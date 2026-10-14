// JUS-139 row keyboard navigation. Type-to-focus resolves a typed prefix
// against the visible (sorted+filtered) listing order, so it can jump to a row
// the windowed list never rendered; the store keeps the shift-range anchor in
// step with the keyboard cursor without touching the selection. These tests pin
// the pure match maths and the anchor action.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
  };
});

import { listEntries, listRoots } from "@/features/files/api/files";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { typeaheadIndex } from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1024 ** 3,
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

describe("typeaheadIndex", () => {
  const names = ["docs", "file-0001.txt", "file-0002.txt", "notes.md", "file.txt"];

  it("matches the prefix case-insensitively", () => {
    expect(typeaheadIndex(names, "Notes")).toBe(3);
    expect(typeaheadIndex(names, "FILE-0002")).toBe(2);
  });

  it("scans the whole listing for a mixed prefix", () => {
    expect(typeaheadIndex(names, "file-", 4)).toBe(1);
  });

  it("cycles through the entries that share a repeated letter", () => {
    // A single letter is a cycle: each press starts after `from`.
    expect(typeaheadIndex(names, "f", 0)).toBe(1);
    expect(typeaheadIndex(names, "ff", 3)).toBe(4);
  });

  it("wraps around the end of the listing", () => {
    expect(typeaheadIndex(names, "f", 5)).toBe(1);
    expect(typeaheadIndex(names, "n", 5)).toBe(3);
  });

  it("returns -1 when nothing matches", () => {
    expect(typeaheadIndex(names, "zzz")).toBe(-1);
    expect(typeaheadIndex([], "a")).toBe(-1);
    expect(typeaheadIndex(names, "")).toBe(-1);
  });

  it("spans a listing the windowed list did not render", () => {
    const many = Array.from({ length: 5000 }, (_, index) =>
      `file-${String(index).padStart(4, "0")}.txt`,
    );
    expect(typeaheadIndex(many, "file-4998")).toBe(4998);
  });
});

describe("files store anchor on keyboard focus", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.mocked(listRoots).mockReset();
    vi.mocked(listEntries).mockReset();
    vi.mocked(listRoots).mockResolvedValue([root]);
    vi.mocked(listEntries).mockResolvedValue({
      entries: [
        entry({ name: "alpha", kind: "directory", size: 0, mode: 0o040755 }),
        entry({ name: "beta" }),
        entry({ name: "gamma" }),
      ],
      truncated: false,
      cursor: "",
    });
    const teams = useTeamsStore();
    teams.activeTeamId = "team-1";
    teams.teams = [];
  });

  it("moves the anchor without changing the selection", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.setAnchor("gamma");

    expect(store.activeTab.anchor).toBe("gamma");
    expect(store.selectedNames).toEqual([]);
  });

  it("clears the anchor", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.setAnchor("beta");
    store.setAnchor(null);

    expect(store.activeTab.anchor).toBeNull();
  });

  it("lets a Shift+Arrow range extend from the keyboard anchor", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.setAnchor("alpha");
    store.selectRange("beta", false);

    expect(store.selectedNames.sort()).toEqual(["alpha", "beta"]);
  });
});
