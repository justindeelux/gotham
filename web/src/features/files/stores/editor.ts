import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createFile,
  deleteEntry,
  describeFilesError,
  filesErrorKey,
  getContent,
  getFileVersion,
  putContent,
} from "@/features/files/api/files";
import type { EditorEol, EditorEncoding, EditorLanguageId } from "@/features/files/utils/editor";
import {
  detectLanguage,
  normalizeEditorEncoding,
  pathWithin,
  rebasedPath,
  serializeDoc,
  splitDoc,
  tabTitle,
  unrepresentableChars,
} from "@/features/files/utils/editor";
import { joinPath } from "@/features/files/utils/files";

/** EditorTabStatus is the load outcome for one open document. */
export type EditorTabStatus =
  | "loading"
  | "ready"
  | "large"
  | "binary"
  | "mixed"
  | "error";

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
  /** kind is "file" for an editable document, "diff" for a read-only history view. */
  kind: EditorTabKind;
  root: string;
  path: string;
  name: string;
  doc: string;
  original: string;
  etag: string;
  eol: EditorEol;
  bom: boolean;
  /** encoding is the charset the document is saved in (detected on load). */
  encoding: EditorEncoding;
  size: number;
  mode: number;
  language: EditorLanguageId;
  /** languageManual is true once the status-bar picker overrode the detected mode. */
  languageManual: boolean;
  status: EditorTabStatus;
  /** error is the localized message for a "error" status tab. */
  error: string;
  saving: boolean;
  /** diff carries the two read-only sides of a history diff tab. */
  diff?: EditorDiff;
}

/** EditorTabKind distinguishes an editable file from a read-only history diff. */
export type EditorTabKind = "file" | "diff";

/** EditorDiff is one stored revision (base) against the live body (current). */
export interface EditorDiff {
  versionId: string;
  base: string;
  current: string;
}

/** SaveOutcome is the result of one save attempt. */
export type SaveOutcome =
  | "saved"
  | "conflict"
  | "error"
  | "blocked"
  /** "unsupported" means the buffer holds characters the save encoding cannot
   *  represent; the encoding warning is armed instead of losing them. */
  | "unsupported";

/** EncodingWarning is a save blocked by characters the target cannot hold. */
export interface EncodingWarning {
  tabId: string;
  encoding: EditorEncoding;
  /** characters lists the distinct offending characters, for the prompt. */
  characters: string[];
}

/** SaveReport is one tab's outcome in a Save All run. */
export interface SaveReport {
  tab: EditorTab;
  outcome: SaveOutcome;
}

let tabSeq = 0;

/** nextTabId mints a stable per-session tab id. */
function nextTabId(): string {
  tabSeq += 1;
  return `tab-${Date.now().toString(36)}-${tabSeq}`;
}

/**
 * Code editor tab store (P18 step 8). Owns the open documents, the active tab
 * and the 409 conflict prompt. It is deliberately standalone: every call takes
 * the server id and active team as arguments, so it never imports the
 * files-browser store (which imports this one, one-way, for tab sync on
 * rename/move/delete).
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
  /**
   * conflictQueue is the ordered list of tabs whose save met a stale-etag 409:
   * Save All can raise several at once, and the prompt walks them one by one.
   */
  const conflictQueue = ref<string[]>([]);
  /** conflictId is the queue head — the conflict prompt currently on screen. */
  const conflictId = computed<string | null>(() => conflictQueue.value[0] ?? null);
  /** saveAsId is the tab awaiting a "save as copy" name. */
  const saveAsId = ref<string | null>(null);
  /** encodingWarning is the save blocked by unrepresentable characters. */
  const encodingWarning = ref<EncodingWarning | null>(null);

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

  /** isDirty reports whether one editable tab has unsaved changes. */
  function isDirty(tab: EditorTab): boolean {
    return (
      tab.kind === "file" && tab.status === "ready" && tab.doc !== tab.original
    );
  }

  /** addConflict queues a conflicting tab behind any prompt already waiting. */
  function addConflict(id: string): void {
    if (!conflictQueue.value.includes(id)) {
      conflictQueue.value = [...conflictQueue.value, id];
    }
  }

  /** clearConflict drops one tab from the conflict queue (prompt resolved). */
  function clearConflict(id: string): void {
    conflictQueue.value = conflictQueue.value.filter((item) => item !== id);
  }

  /** tabFor finds an open tab for one root-relative path. */
  function tabFor(root: string, path: string): EditorTab | null {
    return (
      tabs.value.find(
        (tab) => tab.kind === "file" && tab.root === root && tab.path === path,
      ) ?? null
    );
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
      kind: "file",
      root,
      path,
      name: tabTitle(path),
      doc: "",
      original: "",
      etag: "",
      eol: "\n",
      bom: false,
      encoding: "utf-8",
      size: 0,
      mode: 0,
      language: detectLanguage(path),
      languageManual: false,
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

  /**
   * loadDiff fills a diff tab from the stored revision and the live server body.
   * Re-reading on reuse keeps the right side current when the file changed
   * since the tab was first opened. `title` is the caller-localized tab label.
   */
  async function loadDiff(
    tab: EditorTab,
    teamId: string,
    versionId: string,
    title: string,
  ): Promise<void> {
    tab.status = "loading";
    tab.error = "";
    try {
      // The base is the stored revision; the right side is the live server body
      // (a dirty local buffer is not the file the diff compares against).
      const [version, current] = await Promise.all([
        getFileVersion(serverId.value, teamId, tab.root, tab.path, versionId),
        getContent(serverId.value, teamId, tab.root, tab.path),
      ]);
      tab.diff = { versionId, base: version.content, current: current.content };
      tab.etag = current.etag;
      tab.size = version.version.size;
      tab.name = title;
      tab.status = "ready";
    } catch (err) {
      tab.diff = undefined;
      tab.status = "error";
      tab.error = describeFilesError(err);
    }
  }

  /**
   * openDiff focuses an existing read-only diff tab for one revision or opens a
   * new one. A reused tab is re-read, so its right side reflects the current
   * file. A history tab is never editable or savable. A body that cannot be
   * read (the file grew past the editor limit, or was removed) yields an error
   * tab instead of a blank diff.
   */
  async function openDiff(
    nextServerId: string,
    teamId: string,
    root: string,
    path: string,
    versionId: string,
    title: string,
  ): Promise<void> {
    if (serverId.value !== nextServerId) {
      reset();
      serverId.value = nextServerId;
    }
    const existing = tabs.value.find(
      (tab) =>
        tab.kind === "diff" &&
        tab.root === root &&
        tab.path === path &&
        tab.diff?.versionId === versionId,
    );
    if (existing) {
      activeId.value = existing.id;
      await loadDiff(existing, teamId, versionId, title);
      return;
    }
    const tab: EditorTab = {
      id: nextTabId(),
      kind: "diff",
      root,
      path,
      name: title,
      doc: "",
      original: "",
      etag: "",
      eol: "\n",
      bom: false,
      encoding: "utf-8",
      size: 0,
      mode: 0,
      language: detectLanguage(path),
      languageManual: false,
      status: "loading",
      error: "",
      saving: false,
    };
    tabs.value = [...tabs.value, tab];
    activeId.value = tab.id;
    const live = tabs.value.find((item) => item.id === tab.id);
    if (!live) {
      return;
    }
    await loadDiff(live, teamId, versionId, title);
  }

  /** load reads one tab's content and applies the decoded shape. */
  async function load(tab: EditorTab, teamId: string): Promise<void> {
    try {
      const content = await getContent(serverId.value, teamId, tab.root, tab.path);
      const shape = splitDoc(content.content);
      const encoding = normalizeEditorEncoding(content.encoding);
      Object.assign(tab, {
        doc: shape.doc,
        original: shape.doc,
        eol: shape.eol,
        // A BOM belongs to UTF-8; a legacy body holding U+FEFF would be
        // refused on save, so it is never re-applied there.
        bom: shape.bom && encoding === "utf-8",
        encoding,
        etag: content.etag,
        size: content.size,
        mode: content.mode,
        // A body with mixed terminators cannot round-trip, so it is shown
        // read-only (download only) instead of being silently normalised.
        status: shape.faithful ? "ready" : "mixed",
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
   * setLanguage overrides the detected grammar for one tab. The live session
   * reconfigures its language compartment, so the undo history is untouched;
   * a later rename keeps the manual choice instead of re-detecting.
   */
  function setLanguage(id: string, language: EditorLanguageId): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab) {
      tab.language = language;
      tab.languageManual = true;
    }
  }

  /**
   * setEncoding changes one tab's save charset. The caller re-saves a clean tab
   * so the file itself moves to the new encoding instead of the label only.
   */
  function setEncoding(id: string, encoding: EditorEncoding): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab && tab.kind === "file") {
      tab.encoding = encoding;
      if (encodingWarning.value?.tabId === id) {
        encodingWarning.value = null;
      }
    }
  }

  /** dismissEncodingWarning closes the unrepresentable-characters prompt. */
  function dismissEncodingWarning(): void {
    encodingWarning.value = null;
  }

  /**
   * saveAsUtf8 switches the warned tab to UTF-8 and retries the save, so a
   * character the legacy charset cannot hold is kept instead of dropped.
   */
  async function saveAsUtf8(id: string, teamId: string): Promise<SaveOutcome> {
    setEncoding(id, "utf-8");
    return save(id, teamId);
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
    if (!tab || tab.kind !== "file" || tab.status !== "ready") {
      return "blocked";
    }
    const sentDoc = tab.doc;
    const characters = unrepresentableChars(sentDoc, tab.encoding);
    if (characters.length > 0) {
      // Never let the server substitute '?': stop before the write and let the
      // caller offer a UTF-8 save.
      encodingWarning.value = { tabId: id, encoding: tab.encoding, characters };
      return "unsupported";
    }
    tab.saving = true;
    try {
      const entry = await putContent(serverId.value, teamId, {
        root: tab.root,
        path: tab.path,
        content: serializeDoc({
          doc: sentDoc,
          eol: tab.eol,
          bom: tab.bom,
          encoding: tab.encoding,
        }),
        etag: tab.etag,
        overwrite,
        encoding: tab.encoding,
      });
      tab.etag = entry.etag;
      tab.size = entry.size;
      tab.mode = entry.mode;
      tab.original = sentDoc;
      tab.error = "";
      clearConflict(id);
      return "saved";
    } catch (err) {
      if (filesErrorKey(err) === "files.errors.conflict") {
        addConflict(id);
        return "conflict";
      }
      tab.error = describeFilesError(err);
      return "error";
    } finally {
      tab.saving = false;
    }
  }

  /**
   * saveAll saves every dirty tab in parallel (optionally only those in
   * `ids`), skipping a tab that already has a save in flight so Mod-S cannot
   * race Save All into a second write of the same buffer. `original` advances
   * only on a successful write, so a failed or conflicting tab keeps its dirty
   * marker while the rest are saved.
   */
  async function saveAll(teamId: string, ids?: string[]): Promise<SaveReport[]> {
    const targets = dirtyTabs.value.filter(
      (tab) => !tab.saving && (!ids || ids.includes(tab.id)),
    );
    return Promise.all(
      targets.map(async (tab) => ({ tab, outcome: await save(tab.id, teamId) })),
    );
  }

  /** reload discards local edits and re-reads the file (conflict "reload"). */
  async function reload(id: string, teamId: string): Promise<void> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.kind !== "file") {
      return;
    }
    clearConflict(id);
    tab.status = "loading";
    await load(tab, teamId);
  }

  /** overwrite force-saves over a concurrent change (conflict "overwrite"). */
  async function overwrite(id: string, teamId: string): Promise<SaveOutcome> {
    return save(id, teamId, true);
  }

  /** dismissConflict closes the current prompt and shows the next queued one. */
  function dismissConflict(): void {
    conflictQueue.value = conflictQueue.value.slice(1);
  }

  /** openSaveAs arms the save-as-copy dialog for one conflicting tab. */
  function openSaveAs(id: string): void {
    saveAsId.value = id;
    clearConflict(id);
  }

  /** cancelSaveAs closes the save-as-copy dialog. */
  function cancelSaveAs(): void {
    saveAsId.value = null;
  }

  /**
   * saveAsCopy writes the tab's buffer to a sibling file `name` and retargets
   * the tab onto the copy. It never force-overwrites: the target is reserved
   * with an exclusive create first (409 when it already exists → "conflict"),
   * then the buffer is written under that fresh etag. `original` advances to
   * the exact text sent, so an edit made while the copy is in flight keeps the
   * tab dirty.
   */
  async function saveAsCopy(
    id: string,
    teamId: string,
    name: string,
  ): Promise<SaveOutcome> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.kind !== "file" || tab.status !== "ready") {
      return "blocked";
    }
    const target = joinPath(directoryOf(tab.path), name);
    const sentDoc = tab.doc;
    tab.saving = true;
    let reserved = false;
    try {
      // Reserve the name exclusively: the /content route has no
      // exclusive-create, so this is the guard against clobbering an existing
      // file (a plain PUT with no etag would be refused 428).
      let etag = "";
      try {
        const created = await createFile(serverId.value, teamId, {
          root: tab.root,
          path: target,
        });
        etag = created.etag;
        reserved = true;
      } catch (err) {
        if (filesErrorKey(err) === "files.errors.conflict") {
          return "conflict";
        }
        throw err;
      }
      const entry = await putContent(serverId.value, teamId, {
        root: tab.root,
        path: target,
        content: serializeDoc({
          doc: sentDoc,
          eol: tab.eol,
          bom: tab.bom,
          encoding: tab.encoding,
        }),
        etag,
        encoding: tab.encoding,
      });
      reserved = false;
      tab.path = target;
      tab.name = tabTitle(target);
      if (!tab.languageManual) {
        tab.language = detectLanguage(target);
      }
      tab.etag = entry.etag;
      tab.size = entry.size;
      tab.mode = entry.mode;
      tab.original = sentDoc;
      tab.error = "";
      clearConflict(id);
      saveAsId.value = null;
      return "saved";
    } catch (err) {
      tab.error = describeFilesError(err);
      // Never leave the empty reserved file behind on a failed write.
      if (reserved) {
        await deleteEntry(serverId.value, teamId, tab.root, target, false).catch(
          () => undefined,
        );
      }
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
    clearConflict(id);
    if (encodingWarning.value?.tabId === id) {
      encodingWarning.value = null;
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
    conflictQueue.value = [];
    saveAsId.value = null;
    encodingWarning.value = null;
  }

  /** closeMany closes several tabs, discarding their buffers. */
  function closeMany(ids: string[]): void {
    for (const id of ids) {
      close(id);
    }
  }

  /**
   * blockingNames returns the names among `names` (in directory `dir`) that
   * have unsaved changes in themselves or any descendant, so a
   * rename/move/delete can be refused before it drops the user's edits onto
   * the floor. A directory rename/delete therefore blocks on a dirty file
   * anywhere inside it, not only on an exact-path tab.
   */
  function blockingNames(root: string, dir: string, names: string[]): string[] {
    return names.filter((name) => {
      const target = joinPath(dir, name);
      return tabs.value.some(
        (tab) =>
          tab.root === root &&
          pathWithin(tab.path, target) &&
          isDirty(tab),
      );
    });
  }

  /**
   * applyRename retargets every open tab under a renamed entry. A directory
   * rename rebases its whole subtree (a file rename is the subtree of one),
   * so a later Save never PUTs to the stale old path.
   */
  function applyRename(root: string, dir: string, oldName: string, newName: string): void {
    const from = joinPath(dir, oldName);
    const to = joinPath(dir, newName);
    for (const tab of tabs.value) {
      if (tab.root !== root) {
        continue;
      }
      const next = rebasedPath(tab.path, from, to);
      if (next !== null) {
        tab.path = next;
        tab.name = tabTitle(next);
        if (!tab.languageManual) {
          tab.language = detectLanguage(next);
        }
      }
    }
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
          if (!tab.languageManual) {
            tab.language = detectLanguage(next);
          }
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
  }

  return {
    // state
    serverId,
    tabs,
    activeId,
    conflictId,
    saveAsId,
    encodingWarning,
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
    openDiff,
    setDoc,
    setLanguage,
    setEncoding,
    dismissEncodingWarning,
    saveAsUtf8,
    save,
    saveAll,
    reload,
    overwrite,
    openSaveAs,
    cancelSaveAs,
    dismissConflict,
    saveAsCopy,
    close,
    closeAll,
    closeMany,
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
