import { describe, expect, it } from "vitest";

import type { FilesEntry } from "@/features/files/schemas/files";
import {
  symlinkTargetsDirectory,
  terminalDirectoryFor,
} from "@/features/files/utils/files";

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

  it("refuses files, symlinks and special entries without directory proof", () => {
    for (const kind of ["file", "symlink", "special", "unspecified"] as const) {
      const row = entry({ name: "item", kind, mode: 0o100644 });
      expect(terminalDirectoryFor(row, "my-app")).toBeNull();
    }
  });

  it("enables a symlink only when the listing proves a directory target", () => {
    const config = entry({
      name: "config",
      kind: "directory",
      mode: 0o040755,
    });
    const link = entry({
      name: "current",
      kind: "symlink",
      mode: 0o120777,
      symlink_target: "config",
    });

    expect(
      terminalDirectoryFor(link, "", { entries: [config] }),
    ).toBe("current");
    expect(
      terminalDirectoryFor(link, "my-app", { entries: [config] }),
    ).toBe("my-app/current");

    expect(
      terminalDirectoryFor(
        entry({
          name: "here",
          kind: "symlink",
          symlink_target: ".",
        }),
        "my-app",
        { entries: [] },
      ),
    ).toBe("my-app/here");

    for (const target of ["nowhere", "app.yaml", "another-link", "../escape"]) {
      const unknown = entry({
        name: "mystery",
        kind: "symlink",
        symlink_target: target,
      });
      expect(
        terminalDirectoryFor(unknown, "", {
          entries: [
            config,
            entry({ name: "app.yaml" }),
            entry({ name: "another-link", kind: "symlink" }),
          ],
        }),
      ).toBeNull();
    }
  });

  it("identifies a directory target from the current listing", () => {
    const config = entry({ name: "config", kind: "directory" });
    const link = entry({
      name: "current",
      kind: "symlink",
      symlink_target: "config",
    });
    expect(symlinkTargetsDirectory(link, "", [config])).toBe(true);
    expect(symlinkTargetsDirectory(link, "", [entry({ name: "config" })])).toBe(
      false,
    );
    expect(symlinkTargetsDirectory(link, "", [])).toBe(false);
    expect(
      symlinkTargetsDirectory(
        entry({
          name: "nested",
          kind: "symlink",
          symlink_target: "config/sub",
        }),
        "",
        [config],
      ),
    ).toBe(false);
    expect(
      symlinkTargetsDirectory(
        entry({
          name: "escape",
          kind: "symlink",
          symlink_target: "../outside",
        }),
        "",
        [],
      ),
    ).toBe(false);
  });

  it("keeps spaces, quotes, Unicode and shell metacharacters as one path", () => {
    const name = `my app; rm -rf "$HOME" $(id) \`whoami\` ünïcode & |`;
    const folder = entry({ name, kind: "directory", mode: 0o040755 });
    const path = terminalDirectoryFor(folder, "nested dir");

    expect(path).toBe(`nested dir/${name}`);
    expect(path).not.toContain("cd ");
    expect(path?.includes(name)).toBe(true);
  });

  it("leaves names that look like traversal to the agent's resolver", () => {
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
