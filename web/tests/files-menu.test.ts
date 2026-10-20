// Files menu builders + right-click selection rule (JUS-117 / JUS-124): the row
// menu carries no duplicate "Move" (cut + paste is the move), the folder menu
// offers create/paste/upload/refresh, and right-clicking a row applies the
// multi-select rule. The builders are plain functions and the rule is a store
// method, so both run without mounting a component.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { folderMenuOptions, rowMenuOptions } from "@/features/files/utils/filesMenu";
import type { FilesMenuContext } from "@/features/files/utils/filesMenu";

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

/** ctx builds a fully-writable menu context with a key-echo translator. */
function ctx(overrides: Partial<FilesMenuContext> = {}): FilesMenuContext {
  return {
    canWrite: true,
    canReadContent: true,
    canPaste: true,
    canOpenTerminal: false,
    t: (key: string) => key,
    ...overrides,
  };
}

/** keys reduces a menu to its dispatch keys. */
function keys(options: Array<{ key?: string | number }>): string[] {
  return options.map((option) => String(option.key));
}

/** byKey finds one option by its dispatch key. */
function byKey(
  options: Array<{ key?: string | number; disabled?: boolean }>,
  key: string,
) {
  return options.find((option) => option.key === key);
}

beforeEach(() => {
  setActivePinia(createPinia());
});

describe("rowMenuOptions", () => {
  it("offers the clipboard pair and never a duplicate Move on a file", () => {
    const options = rowMenuOptions(entry({ name: "app.yaml" }), ctx());

    // A file row has no destination, so it carries no Paste.
    expect(keys(options)).toEqual([
      "edit",
      "download",
      "permissions",
      "rename",
      "cut",
      "copy",
      "delete",
    ]);
    expect(keys(options)).not.toContain("move");
    expect(keys(options)).not.toContain("paste");
  });

  it("opens a folder with Open and pastes into that folder", () => {
    const options = rowMenuOptions(
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
      ctx(),
    );

    expect(keys(options)).toEqual([
      "open",
      "open-tab",
      "permissions",
      "rename",
      "cut",
      "copy",
      "paste",
      "delete",
    ]);
  });

  it("offers Open terminal here on a directory row only for an operator", () => {
    const folder = entry({
      name: "config",
      kind: "directory",
      mode: 0o040755,
    });
    const labelled: string[] = [];
    const options = rowMenuOptions(
      folder,
      ctx({
        canOpenTerminal: true,
        t: (key, named) => {
          labelled.push(`${key}:${String(named?.name ?? "")}`);
          return key;
        },
      }),
    );

    expect(keys(options).slice(0, 3)).toEqual([
      "open",
      "open-tab",
      "open-terminal",
    ]);
    // The row action names its target, so two open folders stay distinct.
    expect(labelled).toContain("files.terminal.openIn:config");

    // Files, symlinks and special entries never masquerade as directories.
    for (const kind of ["file", "symlink", "special"] as const) {
      const row = entry({ name: "item", kind, mode: 0o100644 });
      expect(
        keys(rowMenuOptions(row, ctx({ canOpenTerminal: true }))),
      ).not.toContain("open-terminal");
    }
  });

  it("hides Open terminal here from a non-operator", () => {
    const folder = entry({
      name: "config",
      kind: "directory",
      mode: 0o040755,
    });
    expect(
      keys(rowMenuOptions(folder, ctx({ canOpenTerminal: false }))),
    ).not.toContain("open-terminal");
    expect(keys(rowMenuOptions(folder, ctx()))).not.toContain("open-terminal");
  });

  it("adds Extract for an archive file only", () => {
    const archive = rowMenuOptions(
      entry({ name: "backup.tar.gz" }),
      ctx(),
    );
    expect(keys(archive)).toContain("extract");

    const plain = rowMenuOptions(entry({ name: "app.yaml" }), ctx());
    expect(keys(plain)).not.toContain("extract");
  });

  it("leads an image row with View and hides it elsewhere (JUS-204)", () => {
    const image = rowMenuOptions(entry({ name: "logo.png" }), ctx());
    expect(keys(image)[0]).toBe("view");
    expect(byKey(image, "view")?.disabled).toBe(false);

    const plain = rowMenuOptions(entry({ name: "app.yaml" }), ctx());
    expect(keys(plain)).not.toContain("view");

    const gated = rowMenuOptions(
      entry({ name: "logo.png" }),
      ctx({ canReadContent: false }),
    );
    expect(byKey(gated, "view")?.disabled).toBe(true);
  });

  it("keeps a symlink's write actions but never chmod", () => {
    const options = rowMenuOptions(
      entry({ name: "current", kind: "symlink", mode: 0o120777 }),
      ctx(),
    );

    expect(keys(options)).toEqual(["rename", "cut", "copy", "delete"]);
  });

  it("adds Go to target for a symlink with a browsable target only", () => {
    const symlink = entry({
      name: "bin",
      kind: "symlink",
      mode: 0o120777,
      symlink_target: "usr/bin",
    });

    const reachable = rowMenuOptions(symlink, ctx({ symlinkTargetPath: "usr/bin" }));
    expect(keys(reachable)[0]).toBe("go-target");

    // "" (the root's top level) is a valid target, not a hidden one.
    const root = rowMenuOptions(symlink, ctx({ symlinkTargetPath: "" }));
    expect(keys(root)[0]).toBe("go-target");

    // An unresolvable target (outside the root) hides the action.
    const elsewhere = rowMenuOptions(symlink, ctx({ symlinkTargetPath: null }));
    expect(keys(elsewhere)).not.toContain("go-target");

    // A regular file never grows the action, even with a stray path.
    const file = rowMenuOptions(
      entry({ name: "app.yaml" }),
      ctx({ symlinkTargetPath: "usr/bin" }),
    );
    expect(keys(file)).not.toContain("go-target");

    // A symlink with no reported target never grows the action.
    const bare = rowMenuOptions(
      entry({ name: "current", kind: "symlink", mode: 0o120777 }),
      ctx({ symlinkTargetPath: "usr/bin" }),
    );
    expect(keys(bare)).not.toContain("go-target");
  });

  it("offers nothing for a special file", () => {
    const options = rowMenuOptions(
      entry({ name: "run.sock", kind: "special", mode: 0o140777 }),
      ctx(),
    );

    expect(options).toHaveLength(0);
  });

  it("greys the write actions on a read-only root", () => {
    const options = rowMenuOptions(
      entry({ name: "app.yaml" }),
      ctx({ canWrite: false }),
    );

    expect(byKey(options, "rename")?.disabled).toBe(true);
    expect(byKey(options, "cut")?.disabled).toBe(true);
    expect(byKey(options, "copy")?.disabled).toBe(true);
    expect(byKey(options, "delete")?.disabled).toBe(true);
    // Open/download stay available without a write role.
    expect(byKey(options, "download")?.disabled).toBe(false);
  });

  it("disables edit/download without the read role", () => {
    const options = rowMenuOptions(
      entry({ name: "app.yaml" }),
      ctx({ canReadContent: false }),
    );

    expect(byKey(options, "edit")?.disabled).toBe(true);
    expect(byKey(options, "download")?.disabled).toBe(true);
  });

  it("disables Paste on a folder row when the clipboard cannot land there", () => {
    const folder = entry({ name: "config", kind: "directory", mode: 0o040755 });
    const blocked = rowMenuOptions(folder, ctx({ canPaste: false }));
    expect(byKey(blocked, "paste")?.disabled).toBe(true);

    const ready = rowMenuOptions(folder, ctx());
    expect(byKey(ready, "paste")?.disabled).toBe(false);
  });
});

describe("folderMenuOptions", () => {
  it("offers create, paste, upload, the recycle bin and refresh in order", () => {
    expect(keys(folderMenuOptions(ctx()))).toEqual([
      "new-file",
      "new-folder",
      "paste",
      "upload",
      "recycle-bin",
      "refresh",
    ]);
  });

  it("adds Open terminal here only for a platform operator", () => {
    const operator = folderMenuOptions(ctx({ canOpenTerminal: true }));
    expect(keys(operator)).toContain("open-terminal");
    expect(byKey(operator, "open-terminal")?.disabled).toBe(false);
    // A read-only root does not hide the shell: it is not a file write.
    const readOnly = folderMenuOptions(
      ctx({ canOpenTerminal: true, canWrite: false }),
    );
    expect(byKey(readOnly, "open-terminal")?.disabled).toBe(false);

    // A non-operator never sees the action (file read access is not a shell).
    expect(keys(folderMenuOptions(ctx()))).not.toContain("open-terminal");
  });

  it("follows canWrite, keeping refresh and the recycle bin usable on a read-only root", () => {
    const options = folderMenuOptions(ctx({ canWrite: false }));

    expect(byKey(options, "new-file")?.disabled).toBe(true);
    expect(byKey(options, "new-folder")?.disabled).toBe(true);
    expect(byKey(options, "paste")?.disabled).toBe(true);
    expect(byKey(options, "upload")?.disabled).toBe(true);
    // Listing the bin needs no write role; only restore/purge inside it do.
    expect(byKey(options, "recycle-bin")?.disabled).toBe(false);
    expect(byKey(options, "refresh")?.disabled).toBe(false);
  });
});

describe("selectForRowContext", () => {
  it("makes an unselected row the sole selection", () => {
    const store = useFilesStore();
    store.entries = [
      entry({ name: "app.yaml" }),
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
    ];

    store.selectForRowContext(entry({ name: "app.yaml" }));

    expect(store.selectedNames).toEqual(["app.yaml"]);
  });

  it("keeps the whole multi-selection when the row is already selected", () => {
    const store = useFilesStore();
    store.entries = [
      entry({ name: "app.yaml" }),
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
    ];
    store.toggleSelection("app.yaml");
    store.toggleSelection("config");

    store.selectForRowContext(entry({ name: "app.yaml" }));

    expect(store.selectedNames.sort()).toEqual(["app.yaml", "config"]);
  });

  it("replaces a multi-selection when right-clicking an unselected row", () => {
    const store = useFilesStore();
    store.entries = [
      entry({ name: "app.yaml" }),
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
    ];
    store.toggleSelection("app.yaml");

    store.selectForRowContext(entry({ name: "config", kind: "directory" }));

    expect(store.selectedNames).toEqual(["config"]);
  });

  it("leaves a non-selectable entry alone", () => {
    const store = useFilesStore();
    store.toggleSelection("app.yaml");

    store.selectForRowContext(entry({ name: "run.sock", kind: "special" }));

    expect(store.selectedNames).toEqual(["app.yaml"]);
  });
});
