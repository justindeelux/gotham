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
import type { FilesPaneId } from "@/features/files/stores/paneSession";

export const EDITOR_DRAFT_DEBOUNCE_MS = 800;

export interface EditorDraftPrompt {
  tabId: string;
  name: string;
  root: string;
  path: string;
  doc: string;
  eol: EditorEol;
  bom: boolean;
  savedAt: number;
  baseEtag: string;
  loadedEtag: string;
  baseDoc: string;
  conflict: boolean;
}

export function useEditorDrafts(
  getServerId: () => string,
  pane: FilesPaneId = "left",
): {
  prompt: ComputedRef<EditorDraftPrompt | null>;
  restore: () => void;
  discard: () => void;
} {
  const editor = useEditorStore(pane);
  const files = useFilesStore(pane);
  const auth = useAuthStore();

  const prompts = ref<EditorDraftPrompt[]>([]);
  const prompt = computed<EditorDraftPrompt | null>(
    () => prompts.value[0] ?? null,
  );
  const stored = new Map<string, string>();
  const checked = new Set<string>();
  // Guards two panes editing one file: a delayed autosave must not replace a
  // draft another pane wrote after this pane's last edit.
  const seen = new Map<string, string>();
  const touchedAt = new Map<string, number>();
  let timer: ReturnType<typeof setTimeout> | null = null;

  function storage(): EditorDraftStorage | null {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  }

  function draftPrefix(): string {
    return editorDraftStoragePrefix(getServerId(), auth.user?.id ?? "");
  }

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
      const saved = readEditorDraft(store, key);
      if (saved && saved.doc !== tab.doc && saved.savedAt > (touchedAt.get(tab.id) ?? 0)) {
        continue;
      }
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

  function markTouched(): void {
    const now = Date.now();
    for (const tab of editor.tabs) {
      const stamp = `${tab.doc}\u0000${tab.eol}\u0000${tab.bom}`;
      const previous = seen.get(tab.id);
      if (previous !== undefined && previous !== stamp) {
        touchedAt.set(tab.id, now);
      }
      seen.set(tab.id, stamp);
    }
  }

  function schedule(): void {
    if (timer !== null) {
      return;
    }
    timer = setTimeout(() => {
      timer = null;
      flush();
    }, EDITOR_DRAFT_DEBOUNCE_MS);
  }

  function dropPrompt(item: EditorDraftPrompt): void {
    prompts.value = prompts.value.filter((entry) => entry !== item);
  }

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
          baseEtag: draft.etag,
          loadedEtag: tab.etag,
          baseDoc: tab.doc,
          conflict: draft.etag !== tab.etag,
        },
      ];
    }
  }

  function restore(): void {
    const item = prompt.value;
    if (!item) {
      return;
    }
    const tab = editor.tabs.find((entry) => entry.id === item.tabId);
    const stillFresh =
      tab !== undefined &&
      tab.kind === "file" &&
      tab.status === "ready" &&
      !editor.isDirty(tab) &&
      tab.etag === item.loadedEtag &&
      tab.doc === item.baseDoc;
    if (!stillFresh) {
      checked.delete(item.tabId);
      dropPrompt(item);
      checkDrafts();
      return;
    }
    editor.restoreDraft(item.tabId, {
      doc: item.doc,
      eol: item.eol,
      bom: item.bom,
      etag: item.conflict && item.baseEtag !== "" ? item.baseEtag : undefined,
    });
    dropPrompt(item);
  }

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
      markTouched();
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
