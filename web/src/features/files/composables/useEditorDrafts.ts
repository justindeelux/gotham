import { computed, onScopeDispose, ref, watch } from "vue";
import type { ComputedRef } from "vue";

import { useAuthStore } from "@/features/auth/stores/auth";
import type { EditorEol } from "@/features/files/utils/editor";
import {
  editorDraftStorageKey,
  editorDraftStoragePrefix,
  readEditorDraft,
  removeEditorDraft,
  writeEditorDraft,
} from "@/features/files/utils/editorDrafts";
import type { EditorDraftStorage } from "@/features/files/utils/editorDrafts";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";

/** EDITOR_DRAFT_DEBOUNCE_MS batches a burst of keystrokes into one write. */
export const EDITOR_DRAFT_DEBOUNCE_MS = 800;

/** EditorDraftPrompt is one stored draft offered for a freshly loaded file. */
export interface EditorDraftPrompt {
  tabId: string;
  name: string;
  root: string;
  path: string;
  doc: string;
  eol: EditorEol;
  bom: boolean;
  savedAt: number;
  /** conflict is true when the server etag moved past the draft's base. */
  conflict: boolean;
}

/**
 * useEditorDrafts recovers unsaved editor buffers after a reload or crash
 * (JUS-229). A dirty, writable, fully loaded tab is written to localStorage
 * behind a short debounce (plus a flush on pagehide/unmount); a successful save
 * or an explicit discard removes its entry. Reopening a file with a stored
 * draft raises {@link EditorDraftPrompt}, and the caller renders Restore /
 * Discard. Read-only, over-cap and history-diff buffers are never stored, and
 * every storage failure degrades to "no drafts" instead of breaking the editor.
 */
export function useEditorDrafts(getServerId: () => string): {
  prompt: ComputedRef<EditorDraftPrompt | null>;
  restore: () => void;
  discard: () => void;
} {
  const editor = useEditorStore();
  const files = useFilesStore();
  const auth = useAuthStore();

  const prompts = ref<EditorDraftPrompt[]>([]);
  const prompt = computed<EditorDraftPrompt | null>(
    () => prompts.value[0] ?? null,
  );
  /** stored maps a live tab id onto the draft key currently on disk. */
  const stored = new Map<string, string>();
  /** checked holds the tab ids whose reopen prompt was already decided. */
  const checked = new Set<string>();
  let timer: ReturnType<typeof setTimeout> | null = null;

  /** storage reaches localStorage without throwing when it is blocked. */
  function storage(): EditorDraftStorage | null {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  }

  /** draftPrefix is scoped by server and user; empty means "do not persist". */
  function draftPrefix(): string {
    return editorDraftStoragePrefix(getServerId(), auth.user?.id ?? "");
  }

  /**
   * flush writes every eligible dirty tab and removes entries whose tab was
   * saved, reverted, closed or reloaded. A demoted caller keeps the last draft
   * already on disk: the buffer is still theirs, only new writes stop.
   */
  function flush(): void {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
    const prefix = draftPrefix();
    const store = storage();
    if (prefix === "" || store === null) {
      stored.clear();
      return;
    }
    const live = new Set<string>();
    for (const tab of editor.tabs) {
      live.add(tab.id);
      const previous = stored.get(tab.id);
      const dirty =
        tab.kind === "file" && tab.status === "ready" && editor.isDirty(tab);
      if (!dirty) {
        if (previous) {
          removeEditorDraft(store, previous);
          stored.delete(tab.id);
        }
        continue;
      }
      if (!files.canWrite) {
        continue;
      }
      const key = editorDraftStorageKey(prefix, tab.root, tab.path);
      if (previous && previous !== key) {
        removeEditorDraft(store, previous);
        stored.delete(tab.id);
      }
      const written = writeEditorDraft(store, prefix, {
        root: tab.root,
        path: tab.path,
        doc: tab.doc,
        etag: tab.etag,
        eol: tab.eol,
        bom: tab.bom,
        savedAt: Date.now(),
      });
      if (written) {
        stored.set(tab.id, key);
      }
    }
    for (const [id, key] of stored) {
      if (!live.has(id)) {
        removeEditorDraft(store, key);
        stored.delete(id);
      }
    }
  }

  /** schedule batches rapid edits behind one debounced write. */
  function schedule(): void {
    if (timer !== null) {
      return;
    }
    timer = setTimeout(() => {
      timer = null;
      flush();
    }, EDITOR_DRAFT_DEBOUNCE_MS);
  }

  /** dropPrompt settles one offered draft and shows the next queued one. */
  function dropPrompt(item: EditorDraftPrompt): void {
    prompts.value = prompts.value.filter((entry) => entry !== item);
  }

  /**
   * pruneSaved drops the entry of a tab that stopped being dirty (a successful
   * save) or left the store (closed) without waiting for the debounce, so a
   * save never leaves a draft behind that could be offered on the next reload.
   */
  function pruneSaved(): void {
    const store = storage();
    if (store === null || draftPrefix() === "") {
      return;
    }
    for (const [id, key] of stored) {
      const tab = editor.tabs.find((item) => item.id === id);
      const dirty =
        tab !== undefined &&
        tab.kind === "file" &&
        tab.status === "ready" &&
        editor.isDirty(tab);
      if (!dirty) {
        removeEditorDraft(store, key);
        stored.delete(id);
      }
    }
  }

  /**
   * checkDrafts offers a stored draft when a file finishes loading clean. A
   * buffer that is already dirty in memory wins over the stored copy (it is
   * newer), so it is never replaced by a stale draft.
   */
  function checkDrafts(): void {
    const live = new Set(editor.tabs.map((tab) => tab.id));
    if (prompts.value.some((item) => !live.has(item.tabId))) {
      prompts.value = prompts.value.filter((item) => live.has(item.tabId));
    }
    const prefix = draftPrefix();
    const store = storage();
    if (prefix === "" || store === null || !files.canWrite) {
      return;
    }
    for (const tab of editor.tabs) {
      if (
        checked.has(tab.id) ||
        tab.kind !== "file" ||
        tab.status !== "ready" ||
        editor.isDirty(tab)
      ) {
        continue;
      }
      checked.add(tab.id);
      const key = editorDraftStorageKey(prefix, tab.root, tab.path);
      const draft = readEditorDraft(store, key);
      if (!draft) {
        continue;
      }
      if (draft.doc === tab.doc) {
        removeEditorDraft(store, key);
        continue;
      }
      prompts.value = [
        ...prompts.value,
        {
          tabId: tab.id,
          name: tab.name,
          root: tab.root,
          path: tab.path,
          doc: draft.doc,
          eol: draft.eol,
          bom: draft.bom,
          savedAt: draft.savedAt,
          conflict: draft.etag !== "" && tab.etag !== "" && draft.etag !== tab.etag,
        },
      ];
    }
  }

  /** restore applies the offered draft; the next flush stores it again. */
  function restore(): void {
    const item = prompt.value;
    if (!item) {
      return;
    }
    editor.restoreDraft(item.tabId, {
      doc: item.doc,
      eol: item.eol,
      bom: item.bom,
    });
    dropPrompt(item);
  }

  /** discard drops the stored draft and leaves the server version in place. */
  function discard(): void {
    const item = prompt.value;
    if (!item) {
      return;
    }
    const prefix = draftPrefix();
    const store = storage();
    if (prefix !== "" && store !== null) {
      removeEditorDraft(store, editorDraftStorageKey(prefix, item.root, item.path));
    }
    dropPrompt(item);
  }

  watch(
    () => editor.tabs,
    () => {
      pruneSaved();
      checkDrafts();
      schedule();
    },
    { deep: true, immediate: true },
  );

  function onVisibility(): void {
    if (document.visibilityState === "hidden") {
      flush();
    }
  }

  window.addEventListener("pagehide", flush);
  document.addEventListener("visibilitychange", onVisibility);
  onScopeDispose(() => {
    window.removeEventListener("pagehide", flush);
    document.removeEventListener("visibilitychange", onVisibility);
    flush();
  });

  return { prompt, restore, discard };
}
