import { defineStore } from "pinia";
import type { StateTree, Store } from "pinia";

import type { FilesPaneId } from "@/features/files/stores/paneSession";

export type PaneStore<T extends StateTree> = (
  _pane?: FilesPaneId,
) => Store<string, T>;

/**
 * One Pinia store per pane, built from a single setup function so both panes
 * share the implementation without sharing state.
 */
export function definePaneStore<T extends StateTree>(
  name: string,
  setup: (_pane: FilesPaneId) => T,
): PaneStore<T> {
  const definitions = new Map<FilesPaneId, () => Store<string, T>>();
  return (pane: FilesPaneId = "left") => {
    let use = definitions.get(pane);
    if (!use) {
      use = defineStore(`${name}:${pane}`, () => setup(pane)) as unknown as () => Store<
        string,
        T
      >;
      definitions.set(pane, use);
    }
    return use();
  };
}
