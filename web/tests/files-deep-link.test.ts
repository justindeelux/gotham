// JUS-273: opening a volume in File Manager constructs a deep link
// (`/servers/:id/files?root=volumes&path=<dir>`). The target is untrusted
// input to the File Manager, so both the application-side gate and the
// browser-side root check reject forged or escaping values.

import { describe, expect, it } from "vitest";

import type { FilesRoot } from "@/features/files/schemas/files";
import {
  filesDeepLinkRoute,
  isFilesDeepLink,
  isRootRelativePath,
  resolveFilesDeepLink,
} from "@/features/files/utils/deepLink";

/** roots is the node's answer for GET /servers/{id}/files/roots. */
const roots: FilesRoot[] = [
  // Confined agent roots.
  { name: "volumes", path: "/var/lib/gotham/volumes", writable: true, free_bytes: 1 },
  { name: "compose", path: "/var/lib/gotham-agent/compose", writable: false, free_bytes: 1 },
  // Full-access agent: the only root is "system" at "/".
  { name: "system", path: "/", writable: true, free_bytes: 1 },
];

describe("file manager deep link target", () => {
  it("routes to the target's own server, root and directory", () => {
    expect(
      filesDeepLinkRoute({ serverId: "server-1", root: "volumes", path: "app-1/data" }),
    ).toEqual({
      name: "server-files",
      params: { id: "server-1" },
      query: { root: "volumes", path: "app-1/data" },
    });
  });

  it("accepts a target only for this application's server", () => {
    const target = { serverId: "server-1", root: "volumes", path: "app-1/data" };
    expect(isFilesDeepLink(target, "server-1")).toBe(true);
    expect(isFilesDeepLink(target, "server-2")).toBe(false);
    expect(isFilesDeepLink({ ...target, serverId: "" }, "")).toBe(false);
  });

  it("rejects forged roots and escaping paths", () => {
    const base = { serverId: "server-1" };
    for (const target of [
      { ...base, root: "", path: "app-1/data" },
      { ...base, root: "../etc", path: "app-1/data" },
      { ...base, root: "volumes/hidden", path: "app-1/data" },
      { ...base, root: "volumes", path: "/etc" },
      { ...base, root: "volumes", path: "app-1/../../etc" },
      { ...base, root: "volumes", path: "app-1\\data" },
      { ...base, root: "volumes", path: "app-1//data" },
    ]) {
      expect(isFilesDeepLink(target, "server-1"), JSON.stringify(target)).toBe(false);
    }
  });

  it("resolves a full-access system-root link", () => {
    expect(
      resolveFilesDeepLink(
        { root: "system", path: "var/lib/gotham/volumes/app-1/data" },
        "server-1",
        roots,
      ),
    ).toEqual({ root: "system", path: "var/lib/gotham/volumes/app-1/data" });
  });

  it("resolves a query only against a root the node exposes", () => {
    expect(
      resolveFilesDeepLink(
        { root: "volumes", path: "app-1/shared" },
        "server-1",
        roots,
      ),
    ).toEqual({ root: "volumes", path: "app-1/shared" });

    expect(
      resolveFilesDeepLink({ root: "nope", path: "" }, "server-1", roots),
    ).toBeNull();
    expect(
      resolveFilesDeepLink({ root: "volumes", path: "../app-2/data" }, "server-1", roots),
    ).toBeNull();
    expect(
      resolveFilesDeepLink({ root: "volumes", path: "/abs" }, "server-1", roots),
    ).toBeNull();
    expect(resolveFilesDeepLink({}, "server-1", roots)).toBeNull();
  });

  it("treats the root itself as a safe empty path", () => {
    expect(isRootRelativePath("")).toBe(true);
    expect(isRootRelativePath("app-1/data")).toBe(true);
    expect(isRootRelativePath("app-1/./data")).toBe(false);
  });
});
