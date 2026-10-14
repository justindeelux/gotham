// Files symlink "Go to target" resolution (JUS-132): the pure helper behind the
// row action maps a symlink's raw target onto a browsable root-relative path,
// and refuses (null) what cannot be addressed so the action is hidden.
import { describe, expect, it } from "vitest";

import { resolveSymlinkTarget } from "@/features/files/utils/files";

describe("resolveSymlinkTarget", () => {
  it("resolves a relative target from the link's own folder", () => {
    expect(resolveSymlinkTarget("", "bin", "usr/bin")).toBe("usr/bin");
    expect(resolveSymlinkTarget("conf", "current", "app.yaml")).toBe(
      "conf/app.yaml",
    );
  });

  it("applies .. segments without escaping the root", () => {
    expect(resolveSymlinkTarget("conf", "current", "../app-1190")).toBe(
      "app-1190",
    );
    expect(resolveSymlinkTarget("a/b", "link", "../../other")).toBe("other");
  });

  it("returns null when a relative target climbs above the root", () => {
    expect(resolveSymlinkTarget("", "x", "../y")).toBeNull();
    expect(resolveSymlinkTarget("conf", "x", "../../y")).toBeNull();
  });

  it("maps an absolute target that sits inside the root", () => {
    expect(resolveSymlinkTarget("", "greet", "/srv/shared", "/srv")).toBe(
      "shared",
    );
    // A root of "/" (whole-filesystem) strips the single leading slash.
    expect(resolveSymlinkTarget("", "bin", "/usr/bin", "/")).toBe("usr/bin");
    expect(resolveSymlinkTarget("", "x", "/srv/a/b", "/srv/")).toBe("a/b");
  });

  it("resolves a target at the root's top level to \"\"", () => {
    // The root itself is inside the root and browsable.
    expect(resolveSymlinkTarget("", "x", "/srv", "/srv")).toBe("");
    expect(resolveSymlinkTarget("", "x", "/", "/")).toBe("");
    expect(resolveSymlinkTarget("conf", "up", "..")).toBe("");
    expect(resolveSymlinkTarget("", "self", ".")).toBe("");
    // A link to "." in a subfolder is that folder, not the root.
    expect(resolveSymlinkTarget("conf", "self", ".")).toBe("conf");
  });

  it("returns null for an absolute target outside the root or with no root path", () => {
    expect(resolveSymlinkTarget("", "x", "/etc/passwd", "/srv")).toBeNull();
    expect(resolveSymlinkTarget("", "x", "/usr/bin")).toBeNull();
  });

  it("returns null for an empty target", () => {
    expect(resolveSymlinkTarget("", "x", "")).toBeNull();
  });
});
