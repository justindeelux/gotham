// Files schemas, pure formatting helpers and the HTTP status -> i18n key
// mapping (P18 step 6). The catalog parity check keeps en/vi in step.
import { describe, expect, it } from "vitest";

import { filesErrorKey } from "@/features/files/api/files";
import editorEn from "@/features/files/locales/editorEn";
import editorVi from "@/features/files/locales/editorVi";
import en from "@/features/files/locales/en";
import vi from "@/features/files/locales/vi";
import {
  filesContentSchema,
  filesEntrySchema,
  filesListingSchema,
  filesOctalModeSchema,
  filesRootListSchema,
} from "@/features/files/schemas/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import {
  breadcrumbSegments,
  countByKind,
  destinationProblem,
  entryIconKind,
  entryMatchesQuery,
  entryTypeLabel,
  filesFromDrop,
  formatMode,
  formatOctalMode,
  formatOctalModeFull,
  formatTimestamp,
  isFileDrag,
  isSelectable,
  joinPath,
  MAX_NAME_BYTES,
  modeFromFlags,
  normalizeEntryName,
  parentPath,
  parseOctalMode,
  permissionFlags,
  sortEntries,
  validateEntryName,
} from "@/features/files/utils/files";
import { checkCatalogParity } from "@/shared/i18n/catalog";

/** entry builds one row for the helper assertions. */
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

describe("files response schemas", () => {
  it("accepts the roots envelope", () => {
    const parsed = filesRootListSchema.safeParse({
      roots: [
        { name: "volumes", path: "/var/lib/gotham/volumes", writable: true, free_bytes: 10 },
      ],
    });
    expect(parsed.success).toBe(true);
  });

  it("accepts a listing and rejects an unknown kind", () => {
    expect(
      filesListingSchema.safeParse({
        entries: [
          { name: "app.yaml", kind: "file", size: 2, mode: 33188, modified_at: "2026-10-12T09:02:00Z", etag: '"a"' },
        ],
        truncated: false,
      }).success,
    ).toBe(true);
    expect(
      filesEntrySchema.safeParse({
        name: "x",
        kind: "wormhole",
        size: 0,
        mode: 0,
        modified_at: "2026-10-12T09:02:00Z",
        etag: '"a"',
      }).success,
    ).toBe(false);
  });

  it("requires every content field", () => {
    expect(
      filesContentSchema.safeParse({
        content: "a",
        etag: '"a"',
        sha256: "x",
        size: 1,
        mode: 33188,
      }).success,
    ).toBe(true);
    expect(filesContentSchema.safeParse({ content: "a" }).success).toBe(false);
  });
});

describe("filesErrorKey maps every status to a curated key", () => {
  it("covers the error contract", () => {
    const cases: Array<[number, string]> = [
      [400, "files.errors.invalid"],
      [401, "files.errors.sessionExpired"],
      [403, "files.errors.forbidden"],
      [404, "files.errors.notFound"],
      [409, "files.errors.conflict"],
      [413, "files.errors.tooLarge"],
      [415, "files.errors.notEditable"],
      [428, "files.errors.precondition"],
      [429, "files.errors.busy"],
      [500, "common.errors.unexpected"],
      [501, "files.errors.agentOutdated"],
      [502, "files.errors.agentUnavailable"],
      [503, "files.errors.disabled"],
      [507, "files.errors.insufficientStorage"],
    ];
    for (const [status, key] of cases) {
      expect(filesErrorKey({ message: "server text", status })).toBe(key);
    }
    expect(filesErrorKey(new Error("plain"))).toBeNull();
    expect(filesErrorKey({ message: "odd", status: 418 })).toBeNull();
  });

  it("prefers a stable server code over the status", () => {
    // An archive limit is a 413 but must not read as "file too large to edit".
    expect(filesErrorKey({ message: "x", status: 413, code: "archive_limit" })).toBe(
      "files.errors.archiveLimit",
    );
    // A refused archive member is a 409 but must not read as a conflict.
    expect(filesErrorKey({ message: "x", status: 409, code: "archive_entry" })).toBe(
      "files.errors.archiveEntry",
    );
    // A corrupt archive is a 400 but must not read as a generic invalid request.
    expect(filesErrorKey({ message: "x", status: 400, code: "invalid_archive" })).toBe(
      "files.errors.invalidArchive",
    );
    // An unknown code falls back to the status mapping.
    expect(filesErrorKey({ message: "x", status: 409, code: "other" })).toBe(
      "files.errors.conflict",
    );
  });
});

describe("files catalog parity", () => {
  it("keeps en and vi in step", () => {
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });

  it("keeps the lazy editor en and vi catalogs in step", () => {
    expect(checkCatalogParity(editorEn, editorVi)).toEqual([]);
  });
});

describe("formatMode", () => {
  it("renders regular files, directories and symlinks", () => {
    expect(formatMode(0o100644)).toBe("-rw-r--r--");
    expect(formatMode(0o040755)).toBe("drwxr-xr-x");
    expect(formatMode(0o120777)).toBe("lrwxrwxrwx");
    expect(formatMode(0o140777)).toBe("srwxrwxrwx");
  });
});

describe("formatTimestamp", () => {
  it("renders a stable UTC timestamp and an em dash for junk", () => {
    expect(formatTimestamp("2026-10-12T09:02:14Z")).toBe("2026-10-12 09:02");
    expect(formatTimestamp("")).toBe("—");
    expect(formatTimestamp(null)).toBe("—");
    expect(formatTimestamp("not-a-date")).toBe("—");
  });
});

describe("breadcrumbs and paths", () => {
  it("builds root -> dirs crumbs", () => {
    expect(breadcrumbSegments("volumes", "")).toEqual([
      { name: "volumes", path: "", isRoot: true },
    ]);
    expect(breadcrumbSegments("volumes", "a/b")).toEqual([
      { name: "volumes", path: "", isRoot: true },
      { name: "a", path: "a", isRoot: false },
      { name: "b", path: "a/b", isRoot: false },
    ]);
  });

  it("joins and strips paths", () => {
    expect(joinPath("", "x")).toBe("x");
    expect(joinPath("a", "b")).toBe("a/b");
    expect(parentPath("")).toBe("");
    expect(parentPath("a")).toBe("");
    expect(parentPath("a/b/c")).toBe("a/b");
  });
});

describe("sortEntries", () => {
  const rows = [
    entry({ name: "b.txt", size: 30 }),
    entry({ name: "a.txt", size: 10 }),
    entry({ name: "dir", kind: "directory", size: 0, mode: 0o040755 }),
    entry({ name: "link", kind: "symlink", size: 0, mode: 0o120777 }),
  ];

  it("leads with directories then files and symlinks", () => {
    expect(sortEntries(rows, "name", "asc").map((r) => r.name)).toEqual([
      "dir",
      "a.txt",
      "b.txt",
      "link",
    ]);
  });

  it("honours the key and direction within a group", () => {
    expect(sortEntries(rows, "size", "desc").map((r) => r.name)).toEqual([
      "dir",
      "b.txt",
      "a.txt",
      "link",
    ]);
  });

  it("does not mutate the input", () => {
    const copy = [...rows];
    sortEntries(rows, "name", "asc");
    expect(rows).toEqual(copy);
  });
});

describe("entry predicates and labels", () => {
  it("marks folders and files selectable", () => {
    expect(isSelectable(entry({ name: "d", kind: "directory" }))).toBe(true);
    expect(isSelectable(entry({ name: "f" }))).toBe(true);
    expect(isSelectable(entry({ name: "l", kind: "symlink" }))).toBe(false);
    expect(isSelectable(entry({ name: "s", kind: "special" }))).toBe(false);
  });

  it("filters by name case-insensitively", () => {
    expect(entryMatchesQuery(entry({ name: "App.yaml" }), "app")).toBe(true);
    expect(entryMatchesQuery(entry({ name: "App.yaml" }), "")).toBe(true);
    expect(entryMatchesQuery(entry({ name: "App.yaml" }), "z")).toBe(false);
  });

  it("labels types in the active locale", () => {
    expect(entryTypeLabel(entry({ name: "d", kind: "directory" }), "en")).toBe("Folder");
    expect(entryTypeLabel(entry({ name: "d", kind: "directory" }), "vi")).toBe("Thư mục");
    expect(entryTypeLabel(entry({ name: "app.yaml" }), "en")).toBe("YAML");
    expect(entryTypeLabel(entry({ name: "logo.png" }), "en")).toBe("Image");
    expect(entryTypeLabel(entry({ name: "logo.png" }), "vi")).toBe("Ảnh");
    expect(entryTypeLabel(entry({ name: "backup.tar.gz" }), "en")).toBe("Archive");
    expect(entryTypeLabel(entry({ name: "run.sock", kind: "special", mode: 0o140777 }), "en")).toBe("Socket");
  });

  it("picks an icon family per entry", () => {
    expect(entryIconKind(entry({ name: "d", kind: "directory" }))).toBe("folder");
    expect(entryIconKind(entry({ name: "logo.png" }))).toBe("image");
    expect(entryIconKind(entry({ name: "backup.tar.gz" }))).toBe("archive");
    expect(entryIconKind(entry({ name: "server.log" }))).toBe("terminal");
    expect(entryIconKind(entry({ name: "current", kind: "symlink" }))).toBe("link");
    expect(entryIconKind(entry({ name: "app.yaml" }))).toBe("doc");
  });

  it("counts by kind", () => {
    const counts = countByKind([
      entry({ name: "d", kind: "directory" }),
      entry({ name: "f" }),
      entry({ name: "l", kind: "symlink" }),
      entry({ name: "s", kind: "special" }),
    ]);
    expect(counts).toMatchObject({
      directory: 1,
      file: 1,
      symlink: 1,
      special: 1,
    });
  });
});

describe("validateEntryName", () => {
  it("accepts a plain name and trims surrounding whitespace", () => {
    expect(validateEntryName("app.yaml")).toBeNull();
    expect(validateEntryName("  uploads  ")).toBeNull();
    expect(normalizeEntryName("  uploads  ")).toBe("uploads");
  });

  it("rejects empty and whitespace-only names", () => {
    expect(validateEntryName("")).toBe("files.name.required");
    expect(validateEntryName("   ")).toBe("files.name.required");
  });

  it("rejects the dot segments", () => {
    expect(validateEntryName(".")).toBe("files.name.reserved");
    expect(validateEntryName("..")).toBe("files.name.reserved");
  });

  it("rejects slashes, backslashes and NUL", () => {
    expect(validateEntryName("a/b")).toBe("files.name.invalid");
    expect(validateEntryName("a\\b")).toBe("files.name.invalid");
    expect(validateEntryName("a\u0000b")).toBe("files.name.invalid");
  });

  it("rejects a name over the 255-byte ceiling", () => {
    expect(validateEntryName("a".repeat(MAX_NAME_BYTES))).toBeNull();
    expect(validateEntryName("a".repeat(MAX_NAME_BYTES + 1))).toBe(
      "files.name.tooLong",
    );
    // Bytes, not code points: a 4-byte emoji overflows sooner.
    expect(validateEntryName("😀".repeat(64))).toBe("files.name.tooLong");
  });
});

describe("destinationProblem", () => {
  const folder = entry({ name: "config", kind: "directory", mode: 0o040755 });
  const file = entry({ name: "app.yaml" });

  it("refuses the source folder itself", () => {
    expect(destinationProblem("a", "a", [file])).toBe("same-folder");
    expect(destinationProblem("a", "a/b", [file])).toBeNull();
  });

  it("refuses a moved folder and its descendants", () => {
    expect(destinationProblem("src", "src/config", [folder])).toBe("into-itself");
    expect(destinationProblem("src", "src/config/nested", [folder])).toBe(
      "into-itself",
    );
    expect(destinationProblem("src", "other", [folder])).toBeNull();
  });

  it("lets a plain file move anywhere but its own folder", () => {
    expect(destinationProblem("src", "src", [file])).toBe("same-folder");
    expect(destinationProblem("src", "src/deeper", [file])).toBeNull();
  });
});

describe("filesFromDrop", () => {
  function droppedFile(name: string) {
    return new globalThis.File([new Uint8Array(4)], name);
  }

  it("separates files from folders and rejects a directory", () => {
    const file = droppedFile("a.txt");
    const result = filesFromDrop({
      items: [
        {
          kind: "file",
          webkitGetAsEntry: () => ({ isDirectory: false }),
          getAsFile: () => file,
        },
        {
          kind: "file",
          webkitGetAsEntry: () => ({ isDirectory: true }),
          getAsFile: () => null,
        },
      ],
    });
    expect(result.files).toEqual([file]);
    expect(result.folders).toBe(1);
  });

  it("falls back to dataTransfer.files when items are absent", () => {
    const file = droppedFile("b.txt");
    expect(filesFromDrop({ files: [file] })).toEqual({
      files: [file],
      folders: 0,
    });
    expect(filesFromDrop(null)).toEqual({ files: [], folders: 0 });
  });
});

describe("isFileDrag", () => {
  it("detects a Files drag and ignores everything else", () => {
    expect(isFileDrag(["Files"])).toBe(true);
    expect(isFileDrag(["text/plain"])).toBe(false);
    expect(isFileDrag(null)).toBe(false);
    expect(isFileDrag(undefined)).toBe(false);
  });
});

describe("filesOctalModeSchema", () => {
  it("accepts exactly three octal digits and trims", () => {
    expect(filesOctalModeSchema.safeParse("644").success).toBe(true);
    expect(filesOctalModeSchema.safeParse("000").success).toBe(true);
    expect(filesOctalModeSchema.safeParse(" 755 ").success).toBe(true);
  });

  it("rejects a wrong length or a non-octal digit", () => {
    expect(filesOctalModeSchema.safeParse("64").success).toBe(false);
    expect(filesOctalModeSchema.safeParse("6444").success).toBe(false);
    expect(filesOctalModeSchema.safeParse("689").success).toBe(false);
    expect(filesOctalModeSchema.safeParse("abc").success).toBe(false);
    expect(filesOctalModeSchema.safeParse("").success).toBe(false);
  });
});

describe("permission bit helpers", () => {
  it("formats and parses octal modes round-trip", () => {
    expect(formatOctalMode(0o100644)).toBe("644");
    expect(formatOctalMode(0o040755)).toBe("755");
    expect(formatOctalMode(0)).toBe("000");
    expect(parseOctalMode("644")).toBe(0o644);
    expect(parseOctalMode("750")).toBe(0o750);
  });

  it("masks away special bits (never offered by the dialog)", () => {
    expect(formatOctalMode(0o4755)).toBe("755");
    expect(parseOctalMode("755")).toBe(0o755);
  });

  it("formats the display octal, adding a special-bits digit when set", () => {
    // Files and directories stay three digits; the type bits are ignored.
    expect(formatOctalModeFull(0o100644)).toBe("644");
    expect(formatOctalModeFull(0o040755)).toBe("755");
    expect(formatOctalModeFull(0)).toBe("000");
    // setuid, setgid and sticky each take the fourth digit.
    expect(formatOctalModeFull(0o4755)).toBe("4755");
    expect(formatOctalModeFull(0o2755)).toBe("2755");
    expect(formatOctalModeFull(0o1777)).toBe("1777");
    // Type + special + permission bits combine for a real st_mode.
    expect(formatOctalModeFull(0o104755)).toBe("4755");
  });

  it("maps a mode to the nine owner/group/other rwx flags", () => {
    expect(permissionFlags(0o644)).toEqual([
      true, true, false,
      true, false, false,
      true, false, false,
    ]);
    expect(permissionFlags(0o755)).toEqual([
      true, true, true,
      true, false, true,
      true, false, true,
    ]);
  });

  it("rebuilds the mode from the flags", () => {
    expect(modeFromFlags([true, true, false, true, false, false, true, false, false])).toBe(0o644);
    expect(modeFromFlags(permissionFlags(0o750))).toBe(0o750);
  });
});
