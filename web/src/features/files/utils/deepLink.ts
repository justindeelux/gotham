import type { FilesRoot } from "@/features/files/schemas/files";

export interface FilesDeepLink {
  serverId: string;
  root: string;
  path: string;
}

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
