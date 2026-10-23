import { computed, nextTick } from "vue";
import type { Ref } from "vue";

import type {
  ExplorerEntryRow,
  ExplorerRow,
} from "@/features/files/utils/explorer";
import {
  firstEntryIndex,
  lastEntryIndex,
  nextEntryIndex,
} from "@/features/files/utils/explorer";
import { parentPath } from "@/features/files/utils/files";

export interface TreeKeyboardOptions {
  rows: () => ExplorerRow[];
  activeIndex: Ref<number>;
  scrollEl: Ref<HTMLElement | null>;
  rowHeight: number;
  select: (_path: string) => void;
  expand: (_path: string) => Promise<void>;
  collapse: (_path: string) => void;
  toggle: (_path: string) => Promise<void>;
  activate: (_row: ExplorerEntryRow) => void | Promise<void>;
  retry: (_dir: string) => void;
  canWrite: () => boolean;
  rename: (_row: ExplorerEntryRow) => void;
  remove: (_row: ExplorerEntryRow) => void;
  openMenu: (_row: ExplorerEntryRow) => void;
}

export function useTreeKeyboard(options: TreeKeyboardOptions) {
  const { rows, activeIndex, scrollEl, rowHeight } = options;

  const activeRow = computed<ExplorerRow | null>(
    () => rows()[activeIndex.value] ?? null,
  );

  function revealIndex(index: number): void {
    const el = scrollEl.value;
    if (!el) {
      return;
    }
    const top = index * rowHeight;
    const height = el.clientHeight || 360;
    if (top < el.scrollTop) {
      el.scrollTop = top;
    } else if (top + rowHeight > el.scrollTop + height) {
      el.scrollTop = top + rowHeight - height;
    }
  }

  function setActive(index: number): void {
    const list = rows();
    if (index < 0 || index >= list.length) {
      return;
    }
    activeIndex.value = index;
    const row = list[index];
    if (row?.type === "entry") {
      options.select(row.path);
    }
    revealIndex(index);
  }

  function onTreeFocus(): void {
    if (activeRow.value?.type === "entry") {
      return;
    }
    const first = firstEntryIndex(rows());
    if (first >= 0) {
      activeIndex.value = first;
      revealIndex(first);
    }
  }

  function onTreeKeydown(event: KeyboardEvent): void {
    const list = rows();
    if (list.length === 0) {
      return;
    }
    const index = Math.min(Math.max(activeIndex.value, 0), list.length - 1);
    const row = list[index];

    switch (event.key) {
      case "ArrowDown": {
        event.preventDefault();
        const next = nextEntryIndex(list, index + 1, 1);
        if (next >= 0) {
          setActive(next);
        }
        break;
      }
      case "ArrowUp": {
        event.preventDefault();
        const prev = nextEntryIndex(list, index - 1, -1);
        if (prev >= 0) {
          setActive(prev);
        }
        break;
      }
      case "ArrowRight": {
        if (row.type !== "entry" || row.kind !== "directory") {
          break;
        }
        event.preventDefault();
        if (!row.expanded) {
          void options
            .expand(row.path)
            .then(() => void nextTick(() => revealIndex(index)));
        } else {
          const child = nextEntryIndex(list, index + 1, 1);
          if (
            child >= 0 &&
            list[child].type === "entry" &&
            list[child].depth > row.depth
          ) {
            setActive(child);
          }
        }
        break;
      }
      case "ArrowLeft": {
        if (row.type !== "entry") {
          break;
        }
        if (row.kind === "directory" && row.expanded) {
          event.preventDefault();
          options.collapse(row.path);
          break;
        }
        const parent = parentPath(row.path);
        const parentIndex = list.findIndex(
          (item) => item.type === "entry" && item.path === parent,
        );
        if (parentIndex >= 0) {
          event.preventDefault();
          setActive(parentIndex);
        }
        break;
      }
      case "Home": {
        event.preventDefault();
        const first = firstEntryIndex(list);
        if (first >= 0) {
          setActive(first);
        }
        break;
      }
      case "End": {
        event.preventDefault();
        const last = lastEntryIndex(list);
        if (last >= 0) {
          setActive(last);
        }
        break;
      }
      case "Enter": {
        if (row.type === "status") {
          if (row.status === "error") {
            event.preventDefault();
            options.retry(row.dir);
          }
          break;
        }
        event.preventDefault();
        void options.activate(row);
        break;
      }
      case " ": {
        if (row.type === "entry" && row.kind === "directory") {
          event.preventDefault();
          void options.toggle(row.path);
        }
        break;
      }
      case "F2": {
        if (row.type === "entry" && options.canWrite()) {
          event.preventDefault();
          options.rename(row);
        }
        break;
      }
      case "Delete": {
        if (row.type === "entry" && options.canWrite()) {
          event.preventDefault();
          options.remove(row);
        }
        break;
      }
      case "ContextMenu": {
        if (row.type === "entry") {
          event.preventDefault();
          options.openMenu(row);
        }
        break;
      }
      case "F10": {
        if (event.shiftKey && row.type === "entry") {
          event.preventDefault();
          options.openMenu(row);
        }
        break;
      }
      default:
        break;
    }
  }

  return { activeRow, revealIndex, setActive, onTreeFocus, onTreeKeydown };
}
