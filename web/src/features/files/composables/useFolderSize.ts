import { reactive } from "vue";

import {
  calculateFolderSize as fetchFolderSize,
  describeFilesError,
  isCanceledError,
} from "@/features/files/api/files";

export type FolderSizeStatus = "idle" | "loading" | "done" | "error";

export interface FolderSizeState {
  status: FolderSizeStatus;
  bytes: number;
  truncated: boolean;
  error: string | null;
}

const IDLE: FolderSizeState = {
  status: "idle",
  bytes: 0,
  truncated: false,
  error: null,
};

const states = reactive(new Map<string, FolderSizeState>());
const controllers = new Map<string, AbortController>();

function stateKey(serverId: string, root: string, path: string): string {
  return `${serverId}\u0000${root}\u0000${path}`;
}

export function folderSizeState(
  serverId: string,
  root: string,
  path: string,
): FolderSizeState {
  const key = stateKey(serverId, root, path);
  let state = states.get(key);
  if (!state) {
    state = { ...IDLE };
    states.set(key, state);
  }
  return state;
}

export function isFolderSizeLoading(
  serverId: string,
  root: string,
  path: string,
): boolean {
  return folderSizeState(serverId, root, path).status === "loading";
}

export async function requestFolderSize(
  serverId: string,
  teamId: string,
  root: string,
  path: string,
): Promise<void> {
  if (!serverId || root === "") {
    return;
  }
  const key = stateKey(serverId, root, path);
  if (states.get(key)?.status === "loading") {
    return;
  }
  const state = folderSizeState(serverId, root, path);
  const controller = new AbortController();
  controllers.set(key, controller);
  state.status = "loading";
  state.error = null;
  try {
    const size = await fetchFolderSize(serverId, teamId, root, path, controller.signal);
    state.bytes = size.bytes;
    state.truncated = size.truncated;
    state.status = "done";
  } catch (err) {
    if (controllers.get(key) !== controller) {
      return;
    }
    if (isCanceledError(err)) {
      state.status = "idle";
      return;
    }
    state.error = describeFilesError(err);
    state.status = "error";
  } finally {
    if (controllers.get(key) === controller) {
      controllers.delete(key);
    }
  }
}

export function cancelFolderSize(serverId: string, root: string, path: string): void {
  const key = stateKey(serverId, root, path);
  const controller = controllers.get(key);
  if (controller) {
    controller.abort();
    controllers.delete(key);
  }
  const state = states.get(key);
  if (state?.status === "loading") {
    state.status = "idle";
  }
}

export function resetFolderSizes(): void {
  for (const controller of controllers.values()) {
    controller.abort();
  }
  controllers.clear();
  states.clear();
}
