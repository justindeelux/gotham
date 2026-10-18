// Files double-click dispatch (JUS-130): the pure rule behind a double-click
// (or Enter) on a browser row. It maps a row's kind + extension + the caller's
// gates onto the default action, and it decides whether the editor can show the
// file as text at all (known binary families; an oversize text file opens as a
// read-only tail since JUS-225). No DOM, no store.
import { describe, expect, it } from "vitest";

import type { FilesEntry } from "@/features/files/schemas/files";
import {
  MAX_EDITABLE_BYTES,
  canOpenInEditor,
  entryAction,
} from "@/features/files/utils/files";
import type { EntryActionGates } from "@/features/files/utils/files";

/** entry builds one listing row with sensible defaults. */
function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
  return {
    kind: "file",
    size: 2048,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

/** gates builds the owner/admin + writable-root gate set. */
function gates(overrides: Partial<EntryActionGates> = {}): EntryActionGates {
  return { canOpenEditor: true, canExtract: true, ...overrides };
}

describe("entryAction", () => {
  it("navigates into a folder regardless of the gates", () => {
    const folder = entry({ name: "config", kind: "directory", mode: 0o040755 });
    expect(entryAction(folder, gates())).toBe("directory");
    expect(
      entryAction(folder, gates({ canOpenEditor: false, canExtract: false })),
    ).toBe("directory");
  });

  it("extracts a recognized archive when the root is writable", () => {
    for (const name of ["backup.zip", "bundle.tar", "site.tar.gz", "old.tgz"]) {
      expect(entryAction(entry({ name }), gates())).toBe("extract");
    }
  });

  it("does nothing for an archive when the root is not writable", () => {
    // The archive branch wins over the editor: a read-only root never falls
    // back to opening the archive in the editor.
    expect(
      entryAction(entry({ name: "backup.zip" }), gates({ canExtract: false })),
    ).toBe("none");
  });

  it("opens a text/code file in the editor with the read role", () => {
    for (const name of ["app.yaml", "notes.md", "main.go", "script.sh", ".env", "readme.txt"]) {
      expect(entryAction(entry({ name }), gates())).toBe("editor");
    }
  });

  it("does nothing for a text file without the read role", () => {
    expect(
      entryAction(entry({ name: "app.yaml" }), gates({ canOpenEditor: false })),
    ).toBe("none");
  });

  it("does nothing for a binary file the editor cannot show as text", () => {
    for (const name of ["logo.png", "clip.mp4", "track.flac", "manual.pdf", "data.gz"]) {
      expect(entryAction(entry({ name }), gates())).toBe("none");
    }
  });

  it("opens a file over the editor ceiling as a read-only tail", () => {
    const large = entry({ name: "server.log", size: 4 * 1024 * 1024 });
    expect(entryAction(large, gates())).toBe("editor");
  });

  it("does nothing for a symlink, special or unspecified entry", () => {
    expect(entryAction(entry({ name: "current", kind: "symlink" }), gates())).toBe("none");
    expect(entryAction(entry({ name: "run.sock", kind: "special" }), gates())).toBe("none");
    expect(entryAction(entry({ name: "mystery", kind: "unspecified" }), gates())).toBe("none");
  });
});

describe("canOpenInEditor", () => {
  it("accepts a text file at any size", () => {
    expect(canOpenInEditor(entry({ name: "app.yaml" }))).toBe(true);
    expect(canOpenInEditor(entry({ name: "big.txt", size: MAX_EDITABLE_BYTES }))).toBe(true);
    // Over the ceiling opens as a read-only tail, not a dead double-click.
    expect(
      canOpenInEditor(entry({ name: "big.txt", size: MAX_EDITABLE_BYTES + 1 })),
    ).toBe(true);
  });

  it("rejects binary families and every non-file kind", () => {
    expect(canOpenInEditor(entry({ name: "logo.png" }))).toBe(false);
    expect(canOpenInEditor(entry({ name: "run.sock", kind: "special" }))).toBe(false);
    expect(
      canOpenInEditor(entry({ name: "config", kind: "directory", mode: 0o040755 })),
    ).toBe(false);
  });
});
