// Files archive surface (P18 step 9b): the compress/extract store actions and
// the archive name/format helpers. The API module is mocked so the store runs
// without axios or the DOM.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    compressEntries: vi.fn(),
    extractEntry: vi.fn(),
  };
});

import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import {
  compressEntries,
  extractEntry,
  listEntries,
  listRoots,
} from "@/features/files/api/files";
import { useFilesStore } from "@/features/files/stores/files";
import {
  archiveExtension,
  archiveFormatFromName,
  defaultArchiveName,
  isExtractableArchive,
} from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

/** entry builds one listing row with sensible defaults. */
function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
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
  for (const fn of [listRoots, listEntries, compressEntries, extractEntry]) {
    vi.mocked(fn).mockReset();
  }
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue({
    entries: [
      entry({ name: "app.yaml" }),
      entry({ name: "backup.tar.gz" }),
      entry({ name: "config", kind: "directory", size: 0, mode: 0o040755 }),
    ],
    truncated: false,
    cursor: "",
  });
});

describe("files archive helpers", () => {
  it("maps an archive extension onto its extract format", () => {
    expect(archiveFormatFromName("a.zip")).toBe("zip");
    expect(archiveFormatFromName("a.tar")).toBe("tar");
    expect(archiveFormatFromName("a.tar.gz")).toBe("tar.gz");
    expect(archiveFormatFromName("a.tgz")).toBe("tar.gz");
    expect(archiveFormatFromName("a.txt")).toBeNull();
    expect(archiveFormatFromName("a.gz")).toBeNull();
  });

  it("only treats a regular archive file as extractable", () => {
    expect(isExtractableArchive(entry({ name: "a.zip" }))).toBe(true);
    expect(
      isExtractableArchive(entry({ name: "a.zip", kind: "directory" })),
    ).toBe(false);
    expect(isExtractableArchive(entry({ name: "a.txt" }))).toBe(false);
  });

  it("derives a default archive name and extension", () => {
    expect(archiveExtension("zip")).toBe(".zip");
    expect(archiveExtension("tar")).toBe(".tar");
    expect(archiveExtension("tar.gz")).toBe(".tar.gz");
    expect(defaultArchiveName([entry({ name: "config" })], "zip")).toBe(
      "config.zip",
    );
    expect(defaultArchiveName([entry({ name: "backup.tar.gz" })], "tar.gz")).toBe(
      "backup.tar.gz",
    );
    expect(
      defaultArchiveName(
        [entry({ name: "a" }), entry({ name: "b" })],
        "tar",
      ),
    ).toBe("archive.tar");
  });
});

describe("files store compress", () => {
  it("archives the entries into the current directory", async () => {
    vi.mocked(compressEntries).mockResolvedValue(entry({ name: "config.zip" }));

    const store = useFilesStore();
    await store.init("server-1");
    await store.openDirectory("data");

    const created = await store.compress(
      [entry({ name: "a" }), entry({ name: "b" })],
      "config.zip",
      "zip",
    );

    expect(created.name).toBe("config.zip");
    expect(vi.mocked(compressEntries)).toHaveBeenCalledWith(
      "server-1",
      "team-1",
      {
        root: "volumes",
        sources: ["data/a", "data/b"],
        destination: "data/config.zip",
        format: "zip",
      },
    );
    // The listing refreshes after the archive is written.
    expect(vi.mocked(listEntries)).toHaveBeenLastCalledWith(
      "server-1",
      "team-1",
      { root: "volumes", path: "data" },
    );
    expect(store.acting).toBe(false);
  });

  it("propagates a 409 conflict and stops acting", async () => {
    vi.mocked(compressEntries).mockRejectedValue(conflict);

    const store = useFilesStore();
    await store.init("server-1");

    await expect(
      store.compress([entry({ name: "a" })], "a.zip", "zip"),
    ).rejects.toMatchObject({ status: 409 });
    expect(store.acting).toBe(false);
  });
});

describe("files store extract", () => {
  it("unpacks an archive into the current directory", async () => {
    vi.mocked(extractEntry).mockResolvedValue(undefined);

    const store = useFilesStore();
    await store.init("server-1");
    await store.openDirectory("data");

    await store.extract(entry({ name: "backup.tar.gz" }), true);

    expect(vi.mocked(extractEntry)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "data/backup.tar.gz",
      destination: "data",
      overwrite: true,
    });
    expect(store.acting).toBe(false);
  });
});
