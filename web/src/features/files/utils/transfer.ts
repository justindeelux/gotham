import { isRootRelativePath } from "@/features/files/utils/deepLink";
import { destinationProblem } from "@/features/files/utils/files";
import type { ServerStatus } from "@/features/servers/api/servers";

export type CopyTargetProblem =
  | "no-server"
  | "offline"
  | "disabled"
  | "outdated"
  | "not-ready"
  | "no-root"
  | "read-only"
  | "invalid-path"
  | "same-cross-root"
  | "same-folder"
  | "into-itself";

export interface CopyDestinationState {
  serverId: string;
  root: string;
  path: string;
  serverStatus?: ServerStatus;
  writable?: boolean;
  fileManagerDisabled?: boolean;
  metadataUnavailable?: boolean;
}

export interface CopyTargetContext {
  sourceServerId: string;
  sourceRoot: string;
  sourcePath: string;
  sourceDirectories: string[];
  destination: CopyDestinationState;
}

/**
 * Why a destination cannot receive a cross-node copy. Cross-node selection is
 * only for another node, or the same node and root (which uses the local copy).
 */
export function copyTargetProblem(
  context: CopyTargetContext,
): CopyTargetProblem | null {
  const { destination } = context;
  if (destination.serverId === "") {
    return "no-server";
  }
  if (destination.serverStatus !== undefined && destination.serverStatus !== "ready") {
    return destination.serverStatus === "error" ||
      destination.serverStatus === "offline"
      ? "offline"
      : "outdated";
  }
  if (destination.metadataUnavailable === true) {
    return "not-ready";
  }
  if (destination.fileManagerDisabled === true) {
    return "disabled";
  }
  if (destination.root === "") {
    return "no-root";
  }
  if (destination.writable === false) {
    return "read-only";
  }
  if (!isRootRelativePath(destination.path)) {
    return "invalid-path";
  }
  if (destination.serverId !== context.sourceServerId) {
    return null;
  }
  if (destination.root !== context.sourceRoot) {
    return "same-cross-root";
  }
  const problem = destinationProblem(
    context.sourcePath,
    destination.path,
    context.sourceDirectories.map((name) => ({
      name,
      kind: "directory" as const,
    })),
  );
  if (problem === "same-folder") {
    return "same-folder";
  }
  if (problem === "into-itself") {
    return "into-itself";
  }
  return null;
}
