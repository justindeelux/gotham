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

export type EditorTabStatus =
  | "loading"
  | "ready"
  | "large"
  | "binary"
  | "mixed"
  | "error";

export interface EditorTab {
  id: string;
  kind: EditorTabKind;
  root: string;
  path: string;
  name: string;
  doc: string;
  original: string;
  etag: string;
  eol: EditorEol;
  originalEol?: EditorEol;
  bom: boolean;
  encoding: EditorEncoding;
  size: number;
  mode: number;
  truncated: boolean;
  language: EditorLanguageId;
  languageManual: boolean;
  status: EditorTabStatus;
  error: string;
  saving: boolean;
  diff?: EditorDiff;
}

export type EditorTabKind = "file" | "diff";

export interface EditorDiff {
  versionId: string;
  base: string;
  current: string;
}

export type SaveOutcome =
  | "saved"
  | "conflict"
  | "error"
  | "blocked"
  | "unsupported";

export interface EncodingWarning {
  tabId: string;
  encoding: EditorEncoding;
  characters: string[];
}

export type ChangeEncodingOutcome =
  | "saved"
  | "pending"
  | "conflict"
  | "error"
  | "blocked"
  | "unsupported";

export interface SaveReport {
  tab: EditorTab;
  outcome: SaveOutcome;
}

export type LeaveOutcome = "leave" | "stay" | "aborted";

let tabSeq = 0;

function nextTabId(): string {
  tabSeq += 1;
  return `tab-${Date.now().toString(36)}-${tabSeq}`;
}

export const useEditorStore = defineStore("filesEditor", () => {
  const serverId = ref("");
  const tabs = ref<EditorTab[]>([]);
  const activeId = ref("");
  const conflictQueue = ref<string[]>([]);
  const conflictId = computed<string | null>(() => conflictQueue.value[0] ?? null);
  const saveAsId = ref<string | null>(null);
  const leaveOpen = ref(false);
  let leaveResolve: ((_outcome: LeaveOutcome) => void) | null = null;
  let leavePromise: Promise<LeaveOutcome> | null = null;
  const encodingWarnings = ref<EncodingWarning[]>([]);
  const encodingWarning = computed<EncodingWarning | null>(
    () => encodingWarnings.value[0] ?? null,
  );
  const encodingWarningCount = computed<number>(() => encodingWarnings.value.length);

  const activeTab = computed<EditorTab | null>(
    () => tabs.value.find((tab) => tab.id === activeId.value) ?? null,
  );

  const dirtyTabs = computed<EditorTab[]>(() => tabs.value.filter(isDirty));

  const hasDirty = computed<boolean>(() => dirtyTabs.value.length > 0);

  const conflictTab = computed<EditorTab | null>(
    () => tabs.value.find((tab) => tab.id === conflictId.value) ?? null,
  );

  const saveAsTab = computed<EditorTab | null>(
    () => tabs.value.find((tab) => tab.id === saveAsId.value) ?? null,
  );

  function isDirty(tab: EditorTab): boolean {
    return (
      tab.kind === "file" &&
      tab.status === "ready" &&
      !tab.truncated &&
      (tab.doc !== tab.original ||
        (tab.originalEol !== undefined && tab.eol !== tab.originalEol))
    );
  }

  function addConflict(id: string): void {
    if (!conflictQueue.value.includes(id)) {
      conflictQueue.value = [...conflictQueue.value, id];
    }
  }

  function clearConflict(id: string): void {
    conflictQueue.value = conflictQueue.value.filter((item) => item !== id);
  }

  function tabFor(root: string, path: string): EditorTab | null {
    return (
      tabs.value.find(
        (tab) => tab.kind === "file" && tab.root === root && tab.path === path,
      ) ?? null
    );
  }

  function activate(id: string): void {
    if (tabs.value.some((tab) => tab.id === id)) {
      activeId.value = id;
    }
  }

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
      truncated: false,
      language: detectLanguage(path),
      languageManual: false,
      status: "loading",
      error: "",
      saving: false,
    };
    tabs.value = [...tabs.value, tab];
    activeId.value = tab.id;
    const live = tabs.value.find((item) => item.id === tab.id);
    if (live) {
      await load(live, teamId);
    }
  }

  async function loadDiff(
    tab: EditorTab,
    teamId: string,
    versionId: string,
    title: string,
  ): Promise<void> {
    tab.status = "loading";
    tab.error = "";
    try {
      const [version, current] = await Promise.all([
        getFileVersion(serverId.value, teamId, tab.root, tab.path, versionId),
        getContent(serverId.value, teamId, tab.root, tab.path),
      ]);
      if (current.truncated === true) {
        tab.diff = undefined;
        tab.status = "error";
        tab.error = describeFilesError({ message: "truncated", status: 413 });
        return;
      }
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
      truncated: false,
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

  async function load(tab: EditorTab, teamId: string): Promise<void> {
    try {
      const content = await getContent(serverId.value, teamId, tab.root, tab.path);
      const shape = splitDoc(content.content);
      const encoding = normalizeEditorEncoding(content.encoding);
      Object.assign(tab, {
        doc: shape.doc,
        original: shape.doc,
        eol: shape.eol,
        originalEol: shape.eol,
        bom: shape.bom && encoding === "utf-8",
        encoding,
        etag: content.etag,
        size: content.size,
        mode: content.mode,
        truncated: content.truncated === true,
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

  function setDoc(id: string, doc: string): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab && tab.status === "ready" && !tab.truncated) {
      tab.doc = doc;
    }
  }

  function restoreDraft(
    id: string,
    draft: { doc: string; eol: EditorEol; bom: boolean; etag?: string },
  ): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab && tab.kind === "file" && tab.status === "ready") {
      tab.doc = draft.doc;
      tab.eol = draft.eol;
      tab.bom = draft.bom;
      if (draft.etag !== undefined) {
        tab.etag = draft.etag;
      }
    }
  }

  function setLanguage(id: string, language: EditorLanguageId): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab) {
      tab.language = language;
      tab.languageManual = true;
    }
  }

  function setEol(id: string, eol: EditorEol): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab && tab.kind === "file" && tab.status === "ready" && tab.eol !== eol) {
      tab.eol = eol;
    }
  }

  function setEncoding(id: string, encoding: EditorEncoding): void {
    const tab = tabs.value.find((item) => item.id === id);
    if (tab && tab.kind === "file") {
      tab.encoding = encoding;
      dropEncodingWarnings(id);
    }
  }

  function armEncodingWarning(warning: EncodingWarning): void {
    const rest = encodingWarnings.value.filter(
      (item) => item.tabId !== warning.tabId,
    );
    encodingWarnings.value = [...rest, warning];
  }

  function dropEncodingWarnings(tabId: string): void {
    encodingWarnings.value = encodingWarnings.value.filter(
      (item) => item.tabId !== tabId,
    );
  }

  function dismissEncodingWarning(): void {
    encodingWarnings.value = encodingWarnings.value.slice(1);
  }

  async function changeEncoding(
    id: string,
    teamId: string,
    encoding: EditorEncoding,
  ): Promise<ChangeEncodingOutcome> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.kind !== "file" || tab.status !== "ready" || tab.truncated) {
      return "blocked";
    }
    if (tab.encoding === encoding) {
      return "blocked";
    }
    const previous = tab.encoding;
    const wasClean = !isDirty(tab);
    setEncoding(id, encoding);
    if (!wasClean) {
      return "pending";
    }
    const outcome = await save(id, teamId);
    if (outcome !== "saved") {
      tab.encoding = previous;
    }
    return outcome;
  }

  async function saveAsUtf8(id: string, teamId: string): Promise<SaveOutcome> {
    setEncoding(id, "utf-8");
    return save(id, teamId);
  }

  async function save(
    id: string,
    teamId: string,
    overwrite = false,
  ): Promise<SaveOutcome> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.kind !== "file" || tab.status !== "ready" || tab.truncated) {
      return "blocked";
    }
    const sentDoc = tab.doc;
    const sentEol = tab.eol;
    const characters = unrepresentableChars(sentDoc, tab.encoding);
    if (characters.length > 0) {
      armEncodingWarning({ tabId: id, encoding: tab.encoding, characters });
      return "unsupported";
    }
    tab.saving = true;
    try {
      const entry = await putContent(serverId.value, teamId, {
        root: tab.root,
        path: tab.path,
        content: serializeDoc({
          doc: sentDoc,
          eol: sentEol,
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
      tab.originalEol = sentEol;
      tab.error = "";
      clearConflict(id);
      return "saved";
    } catch (err) {
      const key = filesErrorKey(err);
      if (key === "files.errors.conflict") {
        addConflict(id);
        return "conflict";
      }
      tab.error = describeFilesError(err);
      if (key === "files.errors.unrepresentable") {
        armEncodingWarning({ tabId: id, encoding: tab.encoding, characters: [] });
        return "unsupported";
      }
      return "error";
    } finally {
      tab.saving = false;
    }
  }

  async function saveAll(teamId: string, ids?: string[]): Promise<SaveReport[]> {
    const targets = dirtyTabs.value.filter(
      (tab) => !tab.saving && (!ids || ids.includes(tab.id)),
    );
    return Promise.all(
      targets.map(async (tab) => ({ tab, outcome: await save(tab.id, teamId) })),
    );
  }

  async function reload(id: string, teamId: string): Promise<void> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.kind !== "file") {
      return;
    }
    clearConflict(id);
    tab.status = "loading";
    await load(tab, teamId);
  }

  async function overwrite(id: string, teamId: string): Promise<SaveOutcome> {
    return save(id, teamId, true);
  }

  function dismissConflict(): void {
    conflictQueue.value = conflictQueue.value.slice(1);
  }

  function openSaveAs(id: string): void {
    saveAsId.value = id;
    clearConflict(id);
  }

  function cancelSaveAs(): void {
    saveAsId.value = null;
  }

  async function saveAsCopy(
    id: string,
    teamId: string,
    name: string,
  ): Promise<SaveOutcome> {
    const tab = tabs.value.find((item) => item.id === id);
    if (!tab || tab.kind !== "file" || tab.status !== "ready" || tab.truncated) {
      return "blocked";
    }
    const target = joinPath(directoryOf(tab.path), name);
    const sentDoc = tab.doc;
    const sentEol = tab.eol;
    tab.saving = true;
    let reserved = false;
    try {
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
          eol: sentEol,
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
      tab.originalEol = sentEol;
      tab.error = "";
      clearConflict(id);
      saveAsId.value = null;
      return "saved";
    } catch (err) {
      tab.error = describeFilesError(err);
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

  function close(id: string): void {
    const index = tabs.value.findIndex((tab) => tab.id === id);
    if (index === -1) {
      return;
    }
    tabs.value = tabs.value.filter((tab) => tab.id !== id);
    clearConflict(id);
    dropEncodingWarnings(id);
    if (saveAsId.value === id) {
      saveAsId.value = null;
    }
    if (activeId.value === id) {
      activeId.value = tabs.value[Math.min(index, tabs.value.length - 1)]?.id ?? "";
    }
  }

  function closeAll(): void {
    tabs.value = [];
    activeId.value = "";
    conflictQueue.value = [];
    saveAsId.value = null;
    encodingWarnings.value = [];
  }

  function closeMany(ids: string[]): void {
    for (const id of ids) {
      close(id);
    }
  }

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

  function reset(): void {
    abortLeave();
    closeAll();
    serverId.value = "";
  }

  function confirmDiscard(): Promise<LeaveOutcome> {
    if (!hasDirty.value) {
      return Promise.resolve("leave");
    }
    if (leavePromise) {
      return leavePromise;
    }
    leavePromise = new Promise<LeaveOutcome>((resolve) => {
      leaveResolve = resolve;
      leaveOpen.value = true;
    });
    return leavePromise;
  }

  function resolveLeave(leave: boolean): void {
    leaveOpen.value = false;
    leaveResolve?.(leave ? "leave" : "stay");
    leaveResolve = null;
    leavePromise = null;
  }

  function abortLeave(): void {
    if (leavePromise === null) {
      return;
    }
    leaveOpen.value = false;
    leaveResolve?.("aborted");
    leaveResolve = null;
    leavePromise = null;
  }

  return {
    serverId,
    tabs,
    activeId,
    conflictId,
    saveAsId,
    encodingWarning,
    leaveOpen,
    activeTab,
    conflictTab,
    saveAsTab,
    dirtyTabs,
    hasDirty,
    encodingWarningCount,
    isDirty,
    tabFor,
    activate,
    openFile,
    openDiff,
    setDoc,
    restoreDraft,
    setLanguage,
    setEol,
    setEncoding,
    changeEncoding,
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
    confirmDiscard,
    resolveLeave,
    abortLeave,
  };
});

function directoryOf(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}
