// JUS-248: the File manager resolves the directory it hands to the terminal
// session create call. The pure helper must keep the path as data (never a
// shell command) and only ever accept a real directory row.
import { describe, expect, it } from "vitest";

import type { FilesEntry } from "@/features/files/schemas/files";
import { terminalDirectoryFor } from "@/features/files/utils/files";

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

describe("terminalDirectoryFor", () => {
  it("uses the folder on screen for the current-directory action", () => {
    expect(terminalDirectoryFor(null, "")).toBe("");
    expect(terminalDirectoryFor(null, "my-app/data")).toBe("my-app/data");
  });

  it("uses a directory row's own folder", () => {
    const folder = entry({ name: "config", kind: "directory", mode: 0o040755 });
    expect(terminalDirectoryFor(folder, "")).toBe("config");
    expect(terminalDirectoryFor(folder, "my-app")).toBe("my-app/config");
  });

  it("refuses files, symlinks and special entries", () => {
    for (const kind of ["file", "symlink", "special", "unspecified"] as const) {
      const row = entry({ name: "item", kind, mode: 0o100644 });
      expect(terminalDirectoryFor(row, "my-app")).toBeNull();
    }
  });

  it("keeps spaces, quotes, Unicode and shell metacharacters as one path", () => {
    const name = `my app; rm -rf "$HOME" $(id) \`whoami\` ünïcode & |`;
    const folder = entry({ name, kind: "directory", mode: 0o040755 });
    const path = terminalDirectoryFor(folder, "nested dir");

    expect(path).toBe(`nested dir/${name}`);
    // Data only: no command, no quoting and no escaping is added.
    expect(path).not.toContain("cd ");
    expect(path?.includes(name)).toBe(true);
  });

  it("leaves names that look like traversal to the agent's resolver", () => {
    // The file API refuses a ".." entry before it can reach this helper, so a
    // literal dotted name is data. The agent still re-checks root + relative
    // path; the browser never normalizes or rewrites it.
    const folder = entry({
      name: "..not-a-parent",
      kind: "directory",
      mode: 0o040755,
    });
    expect(terminalDirectoryFor(folder, "my-app")).toBe(
      "my-app/..not-a-parent",
    );
  });
});
