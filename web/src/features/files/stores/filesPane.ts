import { inject, provide } from "vue";
import type { InjectionKey } from "vue";

import { useEditorStore } from "@/features/files/stores/editor";
import { useExplorerStore } from "@/features/files/stores/explorer";
import { useFilesStore } from "@/features/files/stores/files";
import { useFilesPaneSession } from "@/features/files/stores/paneSession";
import type { FilesPaneId } from "@/features/files/stores/paneSession";
import { useTrashStore } from "@/features/files/stores/trash";

export type { FilesPaneId };

export const FILES_PANE_KEY: InjectionKey<FilesPaneContext> = Symbol("filesPane");

export interface FilesPaneContext {
  id: FilesPaneId;
  files: ReturnType<typeof useFilesStore>;
  editor: ReturnType<typeof useEditorStore>;
  explorer: ReturnType<typeof useExplorerStore>;
  trash: ReturnType<typeof useTrashStore>;
}

export function filesPane(id: FilesPaneId): FilesPaneContext {
  return {
    id,
    files: useFilesStore(id),
    editor: useEditorStore(id),
    explorer: useExplorerStore(id),
    trash: useTrashStore(id),
  };
}

export function provideFilesPane(id: FilesPaneId): FilesPaneContext {
  const pane = filesPane(id);
  provide(FILES_PANE_KEY, pane);
  return pane;
}

/** The pane a component renders for: its provider, else the workspace-active pane. */
export function useFilesPane(): FilesPaneContext {
  return inject(FILES_PANE_KEY, null) ?? filesPane(useFilesPaneSession().activePane);
}
