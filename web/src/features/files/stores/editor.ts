import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  describeFilesError,
  filesErrorKey,
  getContent,
  putContent,
} from "@/features/files/api/files";
import type { EditorEol, EditorLanguageId } from "@/features/files/utils/editor";
import {
  detectLanguage,
  rebasedPath,
  serializeDoc,
  splitDoc,
  tabTitle,
} from "@/features/files/utils/editor";
import { joinPath } from "@/features/files/utils/files";

/** EditorTabStatus is the load outcome for one open document. */
export type EditorTabStatus = "loading" | "ready" | "large" | "binary" | "error";

/**
 * EditorTab is one open document. `doc` is the live editor text (BOM stripped,
 * EOL normalised to LF); `original` is the last loaded/saved text, so
 * `doc !== original` is the dirty condition without a separate flag. `eol` and
 * `bom` reproduce the file's original bytes on save. `large`/`binary` tabs
 * carry no text (the server refused a 413/415 read) and render a
 * download-only state.
 */
export interface EditorTab {
  id: string;
  root: string;
  path: string;
  name: string;
  doc: string;
  original: string;
  etag: string;
  eol: EditorEol;
  bom: boolean;
  size: number;
  mode: number;
  language: EditorLanguageId;
  status: EditorTabStatus;
  /** error is the localized message for a "error" status tab. */
  error: string;
  saving: boolean;
}

/** SaveOutcome is the result of one save attempt. */
export type SaveOutcome = "saved" | "conflict" | "error" | "blocked";

let tabSeq = 0;

/** nextTabId mints a stable per-session tab id. */
function nextTabId(): string {
  tabSeq += 1;
  return `tab-${Date.now().toString(36)}-${tabSeq}`;
}

/**
 * Code editor tab store (P18 step 8). Owns the open documents, the active tab,
 * the wrap toggle and the 409 conflict prompt. It is deliberately standalone:
 * every call takes the server id and active team as arguments, so it never
 * imports the files-browser store (which imports this one, one-way, for tab
 * sync on rename/move/delete).
 *
 * Byte fidelity: a document is decoded by {@link splitDoc} on load and
 * re-encoded by {@link serializeDoc} on save, so an unedited CRLF or
 * BOM-prefixed file round-trips byte-identical. Dirty is derived from
 * `doc !== original`, so a save only clears it when the document did not change
 * while the request was in flight.
 */
export const useEditorStore = defineStore("filesEditor", () => {
  const serverId = ref("");
  const tabs = ref<EditorTab[]>([]);
  const activeId = ref("");
  const wrap = ref(false);
  /** conflictId is the tab whose save met a stale-etag 409. */
  const conflictId = ref<string | null>(null);
  /** saveAsId is the tab awaiting a "save as copy" name. */
  const saveAsId = ref<string | null>(null);

  /** activeTab resolves the focused tab, if any. */
  const activeTab = computed<EditorTab | null>(
    () => tabs.value.find((tab) => tab.id === activeId.value) ?? null,
  );

  /** dirtyTabs are the open documents with unsaved changes. */
  const dirtyTabs = computed<EditorTab[]>(() => tabs.value.filter(isDirty));

  /** hasDirty reports at least one unsaved document (beforeunload gate). */
  const hasDirty = computed<boolean>(() => dirtyTabs.value.length > 0);

  /** conflictTab is the tab the conflict modal acts on. */
  const conflictTab = computed<EditorTab | null>(
    () => tabs.value.find((tab) => tab.id === conflictId.value) ?? null,
  );

  /** saveAsTab is the tab the save-as-copy dialog acts on. */
  const saveAsTab = computed<EditorTab | null>(
    () => tabs.value.find((tab) => tab.id === saveAsId.value) ?? null,
  );

  /** isDirty reports whether one tab has unsaved changes. */
  function isDirty(tab: EditorTab): boolean {
    return tab.status === "ready" && tab.doc !== tab.original;
  }

  /** tabFor finds an open tab for one root-relative path. */
  function tabFor(root: string, path: string): EditorTab | null {
    return tabs.value.find((tab) => tab.root === root && tab.path === path) ?? null;
  }

  /** activate focuses one tab. */
  function activate(id: string): void {
    if (tabs.value.some((tab) => tab.id === id)) {
      activeId.value = id;
    }
  }

  /**
   * openFile focuses an already-open tab or opens a new one and loads its
   * content. A 413/415 read yields a download-only tab instead of an error.
   */
  async function openFile(
    nextServerId: string,
    teamId: string,
    root: string,
    path: string,
  ): Promise<void> {
    if (serverId.value !== nextServerId) {
      reset();
      serverId.value = nextServerId;
    }
    const existing = tabFor(root, path);
    if (existing) {
      activeId.value = existing.id;
      return;
    }
    const tab: EditorTab = {
      id: nextTabId(),
      root,
      path,
      name: tabTitle(path),
      doc: "",
      original: "",
      etag: "",
      eol: "\n",
      bom: false,
      size: 0,
      mode: 0,
      language: detectLanguage(path),
      status: "loading",
      error: "",
      saving: false,
    };
    tabs.value = [...tabs.value, tab];
    activeId.value = tab.id;
    // Read the reactive proxy back so load()'s mutations are tracked (the raw
    // object pushed above is not reactive on its own).
    const live = tabs.value.find((item) => item.id === tab.id);
    if (live) {
      await load(live, teamId);
    }
  }

  /** load reads one tab's content and applies the decoded shape. */
  async function load(tab: EditorTab, teamId: string): Promise<void> {
    try {
      const content = await getContent(serverId.value, teamId, tab.root, tab.path);
      const shape = splitDoc(content.content);
      Object.assign(tab, {
        doc: shape.doc,
        original: shape.doc,
        eol: shape.eol,
        bom: shape.bom,
        etag: content.etag,
        size: content.size,
        mode: content.mode,
        status: "ready",
        error: "",
      });
    } catch (err) {
      const key = filesErrorKey(err);
      if (key === "files.errors.tooLarge") {
        tab.status = "large";
        tab.error = describeFilesError(err);
      } else if (key === "files.errors.notEditable") {
        tab.status = "binary";
        tab.error = describeFilesError(err);
      } else {
        tab.status = "error";
        tab.error = describeFilesError(err);
      }
    }
  }

  /** setDoc applies an editor change to one tab. */
  function setDoc(id: string, doc: string): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab && tab.status === "ready") {
      tab.doc = doc;
    }
  }

  /**
   * save writes one tab. On a stale-etag 409 it arms the conflict prompt and
   * returns "conflict"; the caller offers reload / overwrite / save-as-copy.
   * `original` advances to the exact text sent, so a change made during the
   * request keeps the tab dirty.
   */
  async function save(
    id: string,
    teamId: string,
    overwrite = false,
  ): Promise<SaveOutcome> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.status !== "ready") {
      return "blocked";
    }
    const sentDoc = tab.doc;
    tab.saving = true;
    try {
      const entry = await putContent(serverId.value, teamId, {
        root: tab.root,
        path: tab.path,
        content: serializeDoc({ doc: sentDoc, eol: tab.eol, bom: tab.bom }),
        etag: tab.etag,
        overwrite,
      });
      tab.etag = entry.etag;
      tab.size = entry.size;
      tab.mode = entry.mode;
      tab.original = sentDoc;
      if (conflictId.value === id) {
        conflictId.value = null;
      }
      return "saved";
    } catch (err) {
      if (filesErrorKey(err) === "files.errors.conflict") {
        conflictId.value = id;
        return "conflict";
      }
      tab.error = describeFilesError(err);
      return "error";
    } finally {
      tab.saving = false;
    }
  }

  /** reload discards local edits and re-reads the file (conflict "reload"). */
  async function reload(id: string, teamId: string): Promise<void> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab) {
      return;
    }
    if (conflictId.value === id) {
      conflictId.value = null;
    }
    tab.status = "loading";
    await load(tab, teamId);
  }

  /** overwrite force-saves over a concurrent change (conflict "overwrite"). */
  async function overwrite(id: string, teamId: string): Promise<SaveOutcome> {
    return save(id, teamId, true);
  }

  /** dismissConflict closes the conflict prompt without a choice. */
  function dismissConflict(): void {
    conflictId.value = null;
  }

  /** openSaveAs arms the save-as-copy dialog for one conflicting tab. */
  function openSaveAs(id: string): void {
    saveAsId.value = id;
    if (conflictId.value === id) {
      conflictId.value = null;
    }
  }

  /** cancelSaveAs closes the save-as-copy dialog. */
  function cancelSaveAs(): void {
    saveAsId.value = null;
  }

  /**
   * saveAsCopy writes the tab's buffer to a sibling file `name` and retargets
   * the tab onto the copy. The caller validates `name` and refuses a name that
   * already exists in the directory (a force-overwrite would lose data).
   */
  async function saveAsCopy(
    id: string,
    teamId: string,
    name: string,
  ): Promise<SaveOutcome> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.status !== "ready") {
      return "blocked";
    }
    const target = joinPath(directoryOf(tab.path), name);
    tab.saving = true;
    try {
      const entry = await putContent(serverId.value, teamId, {
        root: tab.root,
        path: target,
        content: serializeDoc({ doc: tab.doc, eol: tab.eol, bom: tab.bom }),
        overwrite: true,
      });
      tab.path = target;
      tab.name = tabTitle(target);
      tab.language = detectLanguage(target);
      tab.etag = entry.etag;
      tab.size = entry.size;
      tab.mode = entry.mode;
      tab.original = tab.doc;
      if (conflictId.value === id) {
        conflictId.value = null;
      }
      saveAsId.value = null;
      return "saved";
    } catch (err) {
      tab.error = describeFilesError(err);
      return "error";
    } finally {
      tab.saving = false;
    }
  }

  /** close closes one tab, discarding its buffer. */
  function close(id: string): void {
    const index = tabs.value.findIndex((tab) => tab.id === id);
    if (index === -1) {
      return;
    }
    tabs.value = tabs.value.filter((tab) => tab.id !== id);
    if (conflictId.value === id) {
      conflictId.value = null;
    }
    if (saveAsId.value === id) {
      saveAsId.value = null;
    }
    if (activeId.value === id) {
      activeId.value = tabs.value[Math.min(index, tabs.value.length - 1)]?.id ?? "";
    }
  }

  /** closeAll closes every tab. */
  function closeAll(): void {
    tabs.value = [];
    activeId.value = "";
    conflictId.value = null;
    saveAsId.value = null;
  }

  /** setWrap toggles soft wrapping for every editor pane. */
  function setWrap(value: boolean): void {
    wrap.value = value;
  }

  /**
   * blockingNames returns the names among `names` (in directory `dir`) that
   * have unsaved changes, so a rename/move/delete can be refused before it
   * drops the user's edits onto the floor.
   */
  function blockingNames(root: string, dir: string, names: string[]): string[] {
    return names.filter((name) => {
      const tab = tabFor(root, joinPath(dir, name));
      return tab !== null && isDirty(tab);
    });
  }

  /** applyRename retargets an open tab after a file was renamed. */
  function applyRename(root: string, dir: string, oldName: string, newName: string): void {
    const tab = tabFor(root, joinPath(dir, oldName));
    if (!tab) {
      return;
    }
    tab.path = joinPath(dir, newName);
    tab.name = newName;
    tab.language = detectLanguage(newName);
  }

  /** applyMove rebases every open tab under one moved entry. */
  function applyMove(root: string, fromDir: string, toDir: string, names: string[]): void {
    for (const name of names) {
      const from = joinPath(fromDir, name);
      const to = joinPath(toDir, name);
      for (const tab of tabs.value) {
        if (tab.root !== root) {
          continue;
        }
        const next = rebasedPath(tab.path, from, to);
        if (next !== null) {
          tab.path = next;
          tab.name = tabTitle(next);
          tab.language = detectLanguage(next);
        }
      }
    }
  }

  /** applyDelete closes the tabs for deleted entries (and their subtrees). */
  function applyDelete(root: string, dir: string, names: string[]): void {
    const closing = tabs.value.filter(
      (tab) =>
        tab.root === root &&
        names.some((name) => {
          const target = joinPath(dir, name);
          return tab.path === target || tab.path.startsWith(`${target}/`);
        }),
    );
    for (const tab of closing) {
      close(tab.id);
    }
  }

  /** reset clears every tab and unbinds the server (server switch/sign-out). */
  function reset(): void {
    closeAll();
    serverId.value = "";
    wrap.value = false;
  }

  return {
    // state
    serverId,
    tabs,
    activeId,
    wrap,
    conflictId,
    saveAsId,
    // derived
    activeTab,
    conflictTab,
    saveAsTab,
    dirtyTabs,
    hasDirty,
    // actions
    isDirty,
    tabFor,
    activate,
    openFile,
    setDoc,
    save,
    reload,
    overwrite,
    openSaveAs,
    cancelSaveAs,
    dismissConflict,
    saveAsCopy,
    close,
    closeAll,
    setWrap,
    blockingNames,
    applyRename,
    applyMove,
    applyDelete,
    reset,
  };
});

/** directoryOf returns the parent path of a root-relative file path. */
function directoryOf(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}
