import { reactive } from "vue";

import {
  calculateFolderSize as fetchFolderSize,
  describeFilesError,
  isCanceledError,
} from "@/features/files/api/files";

/**
 * Folder size state for the on-demand "Calculate" action (JUS-162).
 *
 * One module-level reactive map holds every folder's state, keyed by
 * `serverId`, root and root-relative path, so the state survives the list's row
 * windowing (a row scrolled out and back keeps its result). Lives outside the
 * files store so the feature stays self-contained; the cache is session-scoped
 * — a page reload recomputes, and a partial result is never cached as final.
 */

export type FolderSizeStatus = "idle" | "loading" | "done" | "error";

/** FolderSizeState is one folder's current calculate state. */
export interface FolderSizeState {
  status: FolderSizeStatus;
  /** bytes is the recursive size, valid once status is "done". */
  bytes: number;
  /** truncated marks a partial result the agent stopped at its cap with. */
  truncated: boolean;
  /** error is the localized failure message, valid once status is "error". */
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

/** folderSizeState returns the (created-on-demand) state for one folder. */
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

/**
 * isFolderSizeLoading reports whether a folder has an in-flight walk, so the
 * cell can gate its Calculate link and offer a Cancel control.
 */
export function isFolderSizeLoading(
  serverId: string,
  root: string,
  path: string,
): boolean {
  return folderSizeState(serverId, root, path).status === "loading";
}

/**
 * requestFolderSize runs one walk. A second call while one is in flight is a
 * no-op (the first wins), so a double click never starts two walks. On abort
 * the state returns to idle; a failure records the localized message.
 */
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
    // A superseded request (cancelled then recalculated, or reset) must not
    // touch the shared state object: its controller is gone from the map, so
    // the newer request owns the state now.
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

/** cancelFolderSize aborts one in-flight walk; a no-op when none is running. */
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

/** resetFolderSizes clears every cached state (node switch, tests). */
export function resetFolderSizes(): void {
  for (const controller of controllers.values()) {
    controller.abort();
  }
  controllers.clear();
  states.clear();
}
