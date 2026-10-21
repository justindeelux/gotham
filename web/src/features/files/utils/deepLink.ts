import type { FilesRoot } from "@/features/files/schemas/files";

/**
 * Deep-link target of the File Manager: the server, an agent root name and a
 * root-relative directory. It is always re-validated against the roots the node
 * actually exposes before the browser applies it, so a forged `?root=`/`?path=`
 * can never reach an authorization boundary the caller does not have.
 */
export interface FilesDeepLink {
  serverId: string;
  root: string;
  path: string;
}

/**
 * isRootRelativePath reports whether value is a safe root-relative path:
 * empty (the root itself) or one or more non-empty segments with no "." or
 * ".." and no leading separator.
 */
export function isRootRelativePath(value: string): boolean {
  if (value === "") {
    return true;
  }
  if (value.startsWith("/") || value.includes("\\")) {
    return false;
  }
  return value
    .split("/")
    .every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

/**
 * isFilesDeepLink validates an untrusted navigation target against the
 * application's own server. It is the application-side guard before routing;
 * the browser still checks the root against the live root list.
 */
export function isFilesDeepLink(
  value: unknown,
  serverId: string,
): value is FilesDeepLink {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const target = value as Partial<FilesDeepLink>;
  return (
    typeof target.serverId === "string" &&
    target.serverId !== "" &&
    target.serverId === serverId &&
    typeof target.root === "string" &&
    /^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(target.root) &&
    typeof target.path === "string" &&
    isRootRelativePath(target.path)
  );
}

/**
 * filesDeepLinkRoute is the router location of a validated target: the named
 * route, the target's own server and the root/path query the browser reads.
 */
export function filesDeepLinkRoute(target: FilesDeepLink): {
  name: string;
  params: { id: string };
  query: { root: string; path: string };
} {
  return {
    name: "server-files",
    params: { id: target.serverId },
    query: { root: target.root, path: target.path },
  };
}

/**
 * resolveFilesDeepLink turns the route query into a target only when the named
 * root is one the node exposes. A missing or unknown root answers null, so the
 * browser keeps its normal default instead of erroring.
 */
export function resolveFilesDeepLink(
  query: Record<string, unknown>,
  serverId: string,
  roots: FilesRoot[],
): { root: string; path: string } | null {
  const root = typeof query.root === "string" ? query.root : "";
  const path = typeof query.path === "string" ? query.path : "";
  if (
    !isFilesDeepLink({ serverId, root, path }, serverId) ||
    !roots.some((entry) => entry.name === root)
  ) {
    return null;
  }
  return { root, path };
}
