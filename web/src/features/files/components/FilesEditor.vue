<script setup lang="ts">
import {
  CaseSensitive,
  History,
  Keyboard,
  PanelLeft,
  Save,
  SaveAll,
  Search,
  WrapText,
  X,
} from "@lucide/vue";
import type { DropdownOption } from "naive-ui";
import {
  NAlert,
  NButton,
  NDropdown,
  NEmpty,
  NModal,
  NSpin,
  useMessage,
} from "naive-ui";
import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { CSSProperties, InjectionKey } from "vue";
import { useI18n } from "vue-i18n";
import { routerKey } from "vue-router";
import type { Router } from "vue-router";

import EditorCloseTabsDialog from "@/features/files/components/EditorCloseTabsDialog.vue";
import EditorConflictDialog from "@/features/files/components/EditorConflictDialog.vue";
import EditorDraftDialog from "@/features/files/components/EditorDraftDialog.vue";
import EditorEncodingDialog from "@/features/files/components/EditorEncodingDialog.vue";
import EditorEncodingUnsupportedDialog from "@/features/files/components/EditorEncodingUnsupportedDialog.vue";
import EditorExplorer from "@/features/files/components/EditorExplorer.vue";
import EditorHistoryDialog from "@/features/files/components/EditorHistoryDialog.vue";
import EditorGotoLine from "@/features/files/components/EditorGotoLine.vue";
import EditorSaveAsDialog from "@/features/files/components/EditorSaveAsDialog.vue";
import EditorSearchBar from "@/features/files/components/EditorSearchBar.vue";
import EditorSettingsPopover from "@/features/files/components/EditorSettingsPopover.vue";
import EditorShortcutsDialog from "@/features/files/components/EditorShortcutsDialog.vue";
import EditorStatusBar from "@/features/files/components/EditorStatusBar.vue";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import { useFileDownload } from "@/features/files/composables/useFileDownload";
import { createEditorSession } from "@/features/files/composables/useCodeMirror";
import type { EditorSession } from "@/features/files/composables/useCodeMirror";
import { useEditorDrafts } from "@/features/files/composables/useEditorDrafts";
import { useEditorFind } from "@/features/files/composables/useEditorFind";
import { createMergeSession } from "@/features/files/composables/useMergeView";
import type { MergeSession } from "@/features/files/composables/useMergeView";
import {
  describeFilesError,
  listFileVersions,
  restoreFileVersion,
} from "@/features/files/api/files";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import type { FilesVersion } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useEditorWindowStore } from "@/features/files/stores/editorWindow";
import { EDITOR_WINDOW_CHROME_MARGIN } from "@/features/files/schemas/editorWindow";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import { useFilesStore } from "@/features/files/stores/files";
import {
  encodingLabel,
  scrollTabIntoView,
  tabsToClose,
} from "@/features/files/utils/editor";
import type {
  EditorEncoding,
  EditorEol,
  EditorLanguageId,
  TabCloseScope,
} from "@/features/files/utils/editor";
import { editorResizeLimits } from "@/features/files/utils/editorResize";
import { useTeamsStore } from "@/features/teams";
import { useCopyText } from "@/shared/composables/useCopyText";
import FloatingWindow from "@/shared/ui/FloatingWindow.vue";
import type { WindowBox } from "@/shared/window/geometry";
import { formatBytes, formatDateTime } from "@/shared/utils/format";

interface Props {
  serverId?: string;
}

const props = withDefaults(defineProps<Props>(), { serverId: "" });

const { t } = useI18n();
const message = useMessage();
// The host shell always provides a router, but unit mounts may not: the
// global dirty guard only registers when one is injected (JUS-232).
const router = inject<Router | null>(routerKey as InjectionKey<Router | null>, null);
const editor = useEditorStore();
const editorWindow = useEditorWindowStore();
const editorSettings = useEditorSettingsStore();
const files = useFilesStore();
const teams = useTeamsStore();
const { download } = useFileDownload();
// activeServerId is the file's server: the page prop while the browser owns
// it, the editor store once the window outlives the page (JUS-232 app host).
const activeServerId = computed<string>(() =>
  props.serverId !== "" ? props.serverId : editor.serverId,
);
// Unsaved drafts survive a reload/crash; reopening a file offers Restore /
// Discard (JUS-229). Kept out of the store so this component stays a view.
const {
  prompt: draftPrompt,
  restore: restoreDraftPrompt,
  discard: discardDraftPrompt,
} = useEditorDrafts(() => activeServerId.value);

/** sessions holds one live CodeMirror instance per opened tab. */
const sessions = new Map<string, EditorSession>();
/** hosts maps a tab id onto its editor host element. */
const hosts = new Map<string, HTMLElement>();
/** sessionLanguages remembers the grammar a live session was last set to. */
const sessionLanguages = new Map<string, EditorLanguageId>();
/** sessionHosts remembers the host a session was created against. */
const sessionHosts = new Map<string, HTMLElement>();
/** creating holds the in-flight session build per tab (rapid re-entry guard). */
const creating = new Map<string, Promise<void>>();
/** mergeSessions holds one live MergeView per open history diff tab. */
const mergeSessions = new Map<string, MergeSession>();
/** mergeHosts maps a diff tab id onto its diff host element. */
const mergeHosts = new Map<string, HTMLElement>();
/** mergeCreating holds the in-flight diff build per tab. */
const mergeCreating = new Map<string, Promise<void>>();
/** mergeSources remembers the two docs a live merge view was built from, so a
 *  refreshed diff tab rebuilds instead of showing stale sides. */
const mergeSources = new Map<string, { base: string; current: string }>();
const cursor = ref<{ line: number; column: number }>({ line: 1, column: 1 });
// Window geometry (JUS-232) lives in the app-level host store so it survives
// route navigation and reloads: `editorWindow.minimized` collapses the window
// into the rail icon, the window stays mounted (v-show) so every live session
// survives. The template reads the store directly.
// explorerOpen toggles the in-editor file explorer; explorerWidth is its
// dragged width (the panel clamps it to 200–480 px on the way back).
const explorerOpen = ref(true);
const explorerWidth = ref(260);
// offset is the window's dragged top-left position, or null while it still sits
// at the CSS default (pinned bottom-right). It is kept in the host store while
// the window stays open and cleared on a dock restore, a narrow viewport, or a
// close-all.
const offset = computed<{ x: number; y: number } | null>(() =>
  editorWindow.x !== null && editorWindow.y !== null
    ? { x: editorWindow.x, y: editorWindow.y }
    : null,
);
// viewport mirrors the window size for display-only fitting: the persisted
// size is never rewritten by a resize event, only fitted on screen.
const viewport = ref({
  width: typeof window === "undefined" ? 0 : window.innerWidth,
  height: typeof window === "undefined" ? 0 : window.innerHeight,
});
// closeAllOpen arms the "close all with unsaved files" confirmation.
const closeAllOpen = ref(false);
// historyOpen shows the version-history dialog for the active file tab.
const historyOpen = ref(false);
// encodingOpen shows the save-encoding dialog for the active file tab.
const encodingOpen = ref(false);
// shortcutsOpen shows the read-only shortcut sheet (one instance).
const shortcutsOpen = ref(false);
// surfaceTrigger remembers the control that opened the current dialog, so
// closing it returns keyboard focus there instead of dropping it on <body>.
const surfaceTrigger = ref<HTMLElement | null>(null);
// historyDialog is the dialog instance, so a restore can refresh its list.
const historyDialog = ref<InstanceType<typeof EditorHistoryDialog> | null>(null);
// historyCounts caches the revision count per tab for the status bar. A file
// whose history cannot be read simply shows no count.
const historyCounts = ref<Record<string, number>>({});

// Tab management (JUS-220): one controlled dropdown serves the tab context
// menu, and one close prompt guards every path that would drop a dirty buffer.
const tabsElement = ref<HTMLElement | null>(null);
const tabMenuOpen = ref(false);
const tabMenuX = ref(0);
const tabMenuY = ref(0);
const tabMenuId = ref("");
/** pendingClose holds the tab ids a close command is about to close; null = no prompt. */
const pendingClose = ref<string[] | null>(null);
/** pendingKeepId is the tab a scoped close (others/right) keeps active. */
const pendingKeepId = ref("");
const closeSaving = ref(false);
const { copyText } = useCopyText();

/**
 * displayedSize is the fitted window size for the current viewport (see
 * displayGeometry, which already honors the side and chrome margins): the
 * single source for rendering, aria and resize seeds, so a resize always
 * starts where the user sees the window.
 */
const displayedSize = computed<{ width: number; height: number }>(() => {
  const fitted = editorWindow.displayGeometry(viewport.value.width, viewport.value.height);
  return { width: fitted.width, height: fitted.height };
});

/**
 * resizeLimits is the resize band for the current viewport: the shared frame
 * clamps every grip/keyboard step against it (JUS-275).
 */
const resizeLimits = computed(() =>
  editorResizeLimits(viewport.value.width, viewport.value.height),
);

/** resizeValueText keeps the slider semantics honest for screen readers. */
const resizeValueText = computed<string>(
  () => `${displayedSize.value.width} by ${displayedSize.value.height} pixels`,
);

/**
 * windowStyle applies the displayed size and dragged position. Until the
 * window is dragged the CSS default (pinned bottom-right) applies, so a
 * fresh open is always as designed. The chrome margin rides along as a
 * custom property so the stylesheet never duplicates the pixel value.
 */
const windowStyle = computed<CSSProperties>(() => {
  const fitted = editorWindow.displayGeometry(viewport.value.width, viewport.value.height);
  const style: Record<string, string> = {
    width: `${displayedSize.value.width}px`,
    height: `${displayedSize.value.height}px`,
    "--ed-window-chrome": `${EDITOR_WINDOW_CHROME_MARGIN}px`,
  };
  const position = offset.value;
  if (position && fitted.x !== null && fitted.y !== null) {
    style.left = `${fitted.x}px`;
    style.top = `${fitted.y}px`;
    style.right = "auto";
    style.bottom = "auto";
  }
  return style as CSSProperties;
});

const readyTabs = computed(() =>
  editor.tabs.filter((tab) => tab.kind === "file" && tab.status === "ready"),
);

/** readyDiffTabs are the read-only history diffs with a loaded MergeView. */
const readyDiffTabs = computed(() =>
  editor.tabs.filter((tab) => tab.kind === "diff" && tab.status === "ready"),
);

/** activeTab is the store's focused tab (convenience for the template). */
const activeTab = computed(() => editor.activeTab);

// Custom find/replace and go-to-line surfaces (JUS-231). One instance serves
// the whole window; the composable keeps each tab's query and restores it on a
// switch, while the matches live in that tab's CodeMirror search state.
const {
  findOpen,
  replaceExpanded,
  focusReplace: findFocusReplace,
  gotoOpen,
  query: findQuery,
  replacement: findReplacement,
  caseSensitive: findCaseSensitive,
  wholeWord: findWholeWord,
  regexp: findRegexp,
  total: findTotal,
  current: findCurrent,
  capped: findCapped,
  gotoCurrent,
  gotoTotal,
  canReplace: canReplaceInTab,
  patternError: findPatternError,
  openFind,
  closeFind,
  toggleReplace,
  setQuery: setFindQuery,
  setReplacement: setFindReplacement,
  toggleCase,
  toggleWord,
  toggleRegex,
  next: nextMatch,
  previous: previousMatch,
  replaceMatch,
  replaceEveryMatch,
  openGotoLine,
  closeGotoLine,
  jumpToLine,
  refresh: refreshFind,
  queueStatus: queueFindStatus,
} = useEditorFind({
  session: () =>
    sessions.get(editor.activeId) ?? mergeSessions.get(editor.activeId) ?? null,
  tabId: () => editor.activeId,
  canReplace: () =>
    activeTab.value?.kind === "file" &&
    activeTab.value.status === "ready" &&
    !activeTab.value.truncated &&
    files.canWrite,
});

/** historyTarget is the editable tab the history dialog acts on, if any. */
const historyTarget = computed<EditorTab | null>(() =>
  activeTab.value?.kind === "file" ? activeTab.value : null,
);

/** historyCount is the active file's cached revision count. */
const historyCount = computed<number>(() =>
  activeTab.value ? (historyCounts.value[activeTab.value.id] ?? 0) : 0,
);

/** pendingCloseDirty is the dirty subset of the tabs awaiting a close prompt. */
const pendingCloseDirty = computed<EditorTab[]>(() => {
  const ids = pendingClose.value ?? [];
  return editor.tabs.filter((tab) => ids.includes(tab.id) && editor.isDirty(tab));
});

/** pendingClosePaths feeds the close prompt's unsaved-file list. */
const pendingClosePaths = computed<string[]>(() =>
  pendingCloseDirty.value.map((tab) => tab.path),
);

/** tabMenuOptions is the context menu for the tab it was opened on. */
const tabMenuOptions = computed<DropdownOption[]>(() => {
  const tabs = editor.tabs;
  const index = tabs.findIndex((tab) => tab.id === tabMenuId.value);
  if (index === -1) {
    return [];
  }
  return [
    { label: String(t("files.editor.menuClose")), key: "close" },
    {
      label: String(t("files.editor.menuCloseOthers")),
      key: "others",
      disabled: tabs.length < 2,
    },
    {
      label: String(t("files.editor.menuCloseRight")),
      key: "right",
      disabled: index === tabs.length - 1,
    },
    { label: String(t("files.editor.menuCloseAll")), key: "all" },
    { type: "divider", key: "divider" },
    { label: String(t("files.editor.menuCopyPath")), key: "copyPath" },
  ];
});

// A dirty buffer must never be dropped by a destructive path — a server or
// team switch, or a user-initiated logout — without an explicit confirmation
// (beforeunload still covers a browser close/reload; forced sign-outs reset
// directly). Route navigation never drops buffers: the window mounts once in
// the app shell and outlives the page. The confirmation dialog renders once
// here and is shared through the editor store, so the account menu's logout
// path prompts through the same dialog.
// A server switch moves the browser to another node while this window may
// hold that node's dirty tabs: confirm before dropping them, and browse back
// when the user stays (mirroring the team-switch revert below). A discard
// resets the window so the next open starts clean on the new server.
watch(
  () => files.serverId,
  (next) => {
    if (!next || next === editor.serverId || !editor.hasDirty) {
      return;
    }
    void editor.confirmDiscard().then((outcome) => {
      if (outcome === "leave") {
        editor.reset();
        return;
      }
      // "stay" browses back to the tab's server; "aborted" (a forced sign-out
      // landing mid-prompt) does nothing.
      if (outcome === "stay" && editor.serverId !== "") {
        void router?.push({ name: "server-files", params: { id: editor.serverId } });
      }
    });
  },
);

// A team switch changes the caller's role and the server's visibility; ask
// before dropping dirty buffers, and revert the selection if the user stays.
// Reverting writes activeTeamId back, which re-triggers this watcher: swallow
// that echo (teamRevertTarget) instead of prompting again forever.
let teamRevertTarget: string | null = null;
watch(
  () => teams.activeTeamId,
  async (next, prev) => {
    if (next === teamRevertTarget) {
      teamRevertTarget = null;
      return;
    }
    if (prev === undefined || next === prev) {
      return;
    }
    const outcome = await editor.confirmDiscard();
    if (outcome === "stay") {
      teamRevertTarget = prev;
      teams.selectTeam(prev);
      return;
    }
    // "aborted" (a forced sign-out landing mid-prompt) selects nothing: the
    // session teardown already reset the stores.
    if (outcome === "leave") {
      editor.reset();
    }
  },
);

// A tab switch, or a tab finishing its load, creates the CodeMirror instance
// the first time a ready tab is shown.
watch(
  () => [editor.activeId, editor.activeTab?.status] as const,
  async () => {
    await nextTick();
    await ensureSession();
    await ensureMerge();
    refreshFind();
    void refreshHistoryCount();
  },
  { flush: "post" },
);

// Drop sessions whose tab was closed, pull external doc changes (a reload)
// back into a live instance, and re-apply the grammar after a rename/save-as
// retargets a tab.
watch(
  () => editor.tabs,
  () => {
    const live = new Set(editor.tabs.map((tab) => tab.id));
    for (const [id, session] of sessions) {
      if (!live.has(id)) {
        session.destroy();
        sessions.delete(id);
        sessionLanguages.delete(id);
        sessionHosts.delete(id);
      }
    }
    for (const [id, session] of mergeSessions) {
      if (!live.has(id)) {
        session.destroy();
        mergeSessions.delete(id);
        mergeHosts.delete(id);
        mergeSources.delete(id);
      }
    }
    for (const tab of editor.tabs) {
      if (tab.kind !== "diff") {
        continue;
      }
      const session = mergeSessions.get(tab.id);
      if (!session) {
        continue;
      }
      const source = mergeSources.get(tab.id);
      const stale =
        tab.status !== "ready" ||
        !tab.diff ||
        !source ||
        source.base !== tab.diff.base ||
        source.current !== tab.diff.current;
      if (!stale) {
        continue;
      }
      session.destroy();
      mergeSessions.delete(tab.id);
      mergeSources.delete(tab.id);
      if (editor.activeId === tab.id && tab.status === "ready") {
        void ensureMerge();
      }
    }
    for (const tab of editor.tabs) {
      const session = sessions.get(tab.id);
      if (!session || tab.status !== "ready") {
        continue;
      }
      if (session.getDoc() !== tab.doc) {
        session.setDoc(tab.doc);
      }
      if (sessionLanguages.get(tab.id) !== tab.language) {
        session.setLanguage(tab.language);
        sessionLanguages.set(tab.id, tab.language);
      }
    }
  },
  { deep: true },
);

// Editor settings are one global preference applied to every open instance.
watch(
  () => editorSettings.settings,
  (settings) => {
    for (const session of sessions.values()) {
      session.setSettings(settings);
    }
    for (const session of mergeSessions.values()) {
      session.setSettings(settings);
    }
  },
  { deep: true },
);

// A role or writable-root change flips the write gate; a live session must
// follow it, or a demoted caller could still type into an already-open buffer.
// A read-only tail stays read-only whatever the role.
watch(
  () => files.canWrite,
  (canWrite) => {
    for (const [id, session] of sessions) {
      const tab = editor.tabs.find((item) => item.id === id);
      session.setReadOnly(!canWrite || tab?.truncated === true);
    }
  },
);

// Opening or focusing any file from the browser while the window is collapsed
// reopens it, so a click always lands somewhere visible. openFile() focuses the
// tab it opens — new or already open — which is exactly an activeId change.
watch(
  () => editor.activeId,
  () => {
    if (editorWindow.minimized) {
      restoreWindow();
    }
  },
);

// With many tabs open, the strip scrolls horizontally. Keep the active tab
// visible after every switch/open so a newly opened file never lands off-screen.
watch(
  () => [editor.activeId, editor.tabs.length] as const,
  () => void nextTick(() => revealActiveTab()),
  { immediate: true },
);

// The floating window unmounts with its last tab (close-all, or closing the
// last tab individually), so any dragged position is meaningless afterwards:
// clear it here so the next open starts at the pinned default again.
watch(
  () => editor.tabs.length,
  (count) => {
    if (count === 0) {
      resetWindowPosition();
    }
  },
);

/** setHost stores a tab's host element from its template ref. */
function setHost(id: string, el: unknown): void {
  if (el instanceof HTMLElement) {
    hosts.set(id, el);
  } else {
    hosts.delete(id);
  }
}

/** setMergeHost stores a diff tab's host element from its template ref. */
function setMergeHost(id: string, el: unknown): void {
  if (el instanceof HTMLElement) {
    mergeHosts.set(id, el);
  } else {
    mergeHosts.delete(id);
  }
}

/** ensureMerge creates the active diff tab's read-only merge view once. */
async function ensureMerge(): Promise<void> {
  const tab = editor.activeTab;
  if (!tab || tab.kind !== "diff" || tab.status !== "ready" || !tab.diff) {
    return;
  }
  const host = mergeHosts.get(tab.id);
  if (!host || mergeSessions.has(tab.id)) {
    return;
  }
  const pending = mergeCreating.get(tab.id);
  if (pending) {
    await pending;
    return;
  }
  const tabId = tab.id;
  const base = tab.diff.base;
  const current = tab.diff.current;
  const build = createMergeSession({
    parent: host,
    base,
    current,
    language: tab.language,
    settings: editorSettings.settings,
    onFind: () => openFind(false),
    onReplace: () => openFind(true),
    onGoToLine: () => openGotoLine(),
  }).then((session) => {
    if (editor.tabs.some((item) => item.id === tabId)) {
      mergeSessions.set(tabId, session);
      mergeSources.set(tabId, { base, current });
      // A settings change during the lazy chunk load must still land on the
      // fresh diff view (the global watcher ran before it existed).
      session.setSettings(editorSettings.settings);
    } else {
      session.destroy();
    }
  });
  mergeCreating.set(tabId, build);
  try {
    await build;
  } finally {
    mergeCreating.delete(tabId);
  }
}

/** refreshHistoryCount caches one file tab's revision count for the footer. */
async function refreshHistoryCount(tab: EditorTab | null = activeTab.value): Promise<void> {
  if (!tab || tab.kind !== "file" || tab.status !== "ready") {
    return;
  }
  try {
    const versions = await listFileVersions(
      activeServerId.value,
      teams.activeTeamId,
      tab.root,
      tab.path,
    );
    historyCounts.value = { ...historyCounts.value, [tab.id]: versions.length };
  } catch {
    // History is optional: a 503 or a deleted path simply omits the count.
  }
}

/** openHistory shows the version-history dialog for the active file. */
function openHistory(): void {
  // A truncated tab is a read-only tail: a restore would overwrite the whole
  // file from a view the user never saw.
  if (!historyTarget.value || historyTarget.value.truncated) {
    return;
  }
  historyOpen.value = true;
}

/** openDiff opens (or reuses) a read-only diff tab for one revision. */
async function openDiff(version: FilesVersion): Promise<void> {
  const tab = historyTarget.value;
  if (!tab) {
    return;
  }
  await editor.openDiff(
    activeServerId.value,
    teams.activeTeamId,
    tab.root,
    tab.path,
    version.id,
    `${tab.name} · ${formatDateTime(version.created_at)}`,
  );
  const opened = editor.activeTab;
  if (opened?.status === "error") {
    message.error(opened.error || String(t("files.editor.historyLoadFailed")));
    return;
  }
  historyOpen.value = false;
}

/**
 * restoreVersion writes one revision back over the file, reloads the open tab
 * and refreshes the dialog. A dirty tab was already confirmed by the dialog.
 */
async function restoreVersion(version: FilesVersion): Promise<void> {
  const tab = historyTarget.value;
  if (!tab || tab.truncated) {
    return;
  }
  try {
    await restoreFileVersion(
      activeServerId.value,
      teams.activeTeamId,
      tab.root,
      tab.path,
      version.id,
    );
    // Drop any diff tab of this file: it compared a revision against the
    // content the restore just replaced, so it is stale now.
    for (const item of editor.tabs) {
      if (item.kind === "diff" && item.root === tab.root && item.path === tab.path) {
        editor.close(item.id);
      }
    }
    await editor.reload(tab.id, teams.activeTeamId);
    message.success(String(t("files.editor.historyRestored", { name: tab.name })));
    void files.refresh();
    await refreshHistoryCount(editor.tabs.find((item) => item.id === tab.id) ?? null);
    await historyDialog.value?.reload();
  } catch (err) {
    message.error(describeFilesError(err));
  }
}

/** isTabFocused reports whether DOM focus is on a tablist button. */
function isTabFocused(): boolean {
  const active = document.activeElement;
  return active instanceof HTMLElement && active.getAttribute("role") === "tab";
}

/** ensureSession creates the active tab's editor instance on first view. */
async function ensureSession(): Promise<void> {
  const tab = editor.activeTab;
  if (!tab || tab.kind !== "file" || tab.status !== "ready") {
    return;
  }
  const host = hosts.get(tab.id);
  if (!host) {
    return;
  }
  const existing = sessions.get(tab.id);
  if (existing) {
    // The host stays mounted across a status switch now, but if it was ever
    // replaced (unmount/remount) the session is bound to a detached node and
    // must be rebuilt against the live one.
    if (sessionHosts.get(tab.id) === host) {
      // Only pull focus into the editor on an explicit open (browser click).
      // While the user is on a tab button, re-focusing here would steal
      // ArrowLeft/ArrowRight from the roving tablist focus.
      if (!isTabFocused()) {
        existing.focus();
      }
      return;
    }
    existing.destroy();
    sessions.delete(tab.id);
    sessionLanguages.delete(tab.id);
    sessionHosts.delete(tab.id);
  }
  const pending = creating.get(tab.id);
  if (pending) {
    await pending;
    return;
  }
  const tabId = tab.id;
  const build = createEditorSession({
    parent: host,
    doc: tab.doc,
    language: tab.language,
    ariaLabel: String(t("files.editor.contentLabel", { name: tab.name })),
    readOnly: !files.canWrite || tab.truncated,
    settings: editorSettings.settings,
    onChange: (doc) => {
      editor.setDoc(tabId, doc);
      // Coalesce the recount; a scan per transaction would block typing.
      queueFindStatus();
    },
    onCursor: (line, column) => {
      if (editor.activeId === tabId) {
        cursor.value = { line, column };
      }
    },
    onSave: () => void saveTab(tabId),
    onSaveAll: () => void saveAllTabs(),
    onCloseTab: () => requestCloseTabs([tabId]),
    onToggleWrap: () => toggleWrap(),
    onNextTab: () => cycleTab(1),
    onPreviousTab: () => cycleTab(-1),
    onFind: () => openFind(false),
    onReplace: () => openFind(true),
    onGoToLine: () => openGotoLine(),
    onFindNext: () => {
      openFind(false);
      nextMatch();
    },
    onFindPrevious: () => {
      openFind(false);
      previousMatch();
    },
  }).then((session) => {
    // A rapid tab switch while the chunk loaded: keep the instance only if the
    // tab is still open.
    if (editor.tabs.some((item) => item.id === tabId)) {
      sessions.set(tabId, session);
      sessionHosts.set(tabId, host);
      sessionLanguages.set(tabId, tab.language);
      // A settings change during the lazy chunk load must still land on the
      // fresh instance (the global watcher ran before it existed).
      session.setSettings(editorSettings.settings);
      session.requestMeasure();
      session.focus();
    } else {
      session.destroy();
    }
  });
  creating.set(tabId, build);
  try {
    await build;
  } finally {
    creating.delete(tabId);
  }
}

/** saveTab saves one tab and reports the outcome. */
async function saveTab(id: string): Promise<void> {
  const tab = editor.tabs.find((item) => item.id === id);
  // Ctrl/Cmd+S is a no-op on a read-only or clean tab: never rewrite a clean
  // file (which would normalise it) and never fire a write the server refuses.
  if (
    !tab ||
    tab.saving ||
    tab.status !== "ready" ||
    !files.canWrite ||
    !editor.isDirty(tab)
  ) {
    return;
  }
  const outcome = await editor.save(id, teams.activeTeamId);
  if (outcome === "saved") {
    message.success(String(t("files.editor.saved", { name: tab.name })));
    void refreshHistoryCount(tab);
  } else if (outcome === "error") {
    message.error(tab.error || String(t("files.editor.saveFailed", { name: tab.name })));
  }
  // "conflict" and "unsupported" arm their own prompts; "blocked" is a no-op
  // (loading/read-only tab).
}

/** saveFailureText names the tab and, when known, its server error. */
function saveFailureText(tab: EditorTab): string {
  return tab.error
    ? String(
        t("files.editor.saveAllFailedDetail", {
          name: tab.name,
          error: tab.error,
        }),
      )
    : String(t("files.editor.saveAllFailed", { name: tab.name }));
}

/**
 * saveAllTabs saves every dirty tab (Ctrl/Cmd+Shift+S). A failed tab keeps its
 * dirty marker and reports its own error; a run where everything saved gets a
 * single success toast.
 */
async function saveAllTabs(): Promise<void> {
  if (!files.canWrite || !editor.hasDirty) {
    return;
  }
  // A second Ctrl+Shift+S while the first run is in flight would write the same
  // buffers twice.
  if (editor.dirtyTabs.some((tab) => tab.saving)) {
    return;
  }
  const reports = await editor.saveAll(teams.activeTeamId);
  let saved = 0;
  for (const report of reports) {
    if (report.outcome === "saved") {
      saved += 1;
      void refreshHistoryCount(report.tab);
      continue;
    }
    if (report.outcome === "conflict") {
      message.warning(
        String(t("files.editor.conflictFile", { name: report.tab.name })),
      );
      continue;
    }
    if (report.outcome === "unsupported") {
      // The encoding prompt names the tab and offers a UTF-8 save.
      continue;
    }
    message.error(saveFailureText(report.tab));
  }
  if (saved > 0 && saved === reports.length) {
    message.success(String(t("files.editor.saveAllDone")));
  }
}

/** downloadActive hands a large or binary tab's file to the browser. */
async function downloadActive(): Promise<void> {
  const tab = activeTab.value;
  if (!tab) {
    return;
  }
  await download(activeServerId.value, teams.activeTeamId, tab.root, directoryOf(tab.path), {
    name: tab.name,
  });
}

/** directoryOf returns the parent path of a root-relative path. */
function directoryOf(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}

/** toggleWrap flips the wrap preference. */
function toggleWrap(): void {
  editorSettings.update({ wrap: !editorSettings.settings.wrap });
}

/** pickLanguage overrides the active tab's grammar without touching history. */
function pickLanguage(language: EditorLanguageId): void {
  const tab = activeTab.value;
  if (tab) {
    editor.setLanguage(tab.id, language);
  }
}

/**
 * pickEol switches the active tab's line ending. The buffer text is untouched;
 * the tab turns dirty and the next save writes the new terminators.
 */
function pickEol(eol: EditorEol): void {
  const tab = activeTab.value;
  if (tab) {
    editor.setEol(tab.id, eol);
  }
}

/** pickTabSize applies the indentation width to every open editor. */
function pickTabSize(size: number): void {
  editorSettings.update({ tabSize: size as EditorSettings["tabSize"] });
}

/** encodingWarningTab is the open tab the unsupported-characters prompt names. */
const encodingWarningTab = computed<EditorTab | null>(
  () =>
    editor.tabs.find((tab) => tab.id === editor.encodingWarning?.tabId) ?? null,
);

/** dialogOpen is true while any editor dialog owns the keyboard. */
const dialogOpen = computed<boolean>(
  () =>
    shortcutsOpen.value ||
    historyOpen.value ||
    encodingOpen.value ||
    closeAllOpen.value ||
    editor.leaveOpen ||
    pendingClose.value !== null ||
    editor.encodingWarning !== null,
);

// Capture the trigger before a dialog steals focus, and give it back after the
// last one closes. NModal traps focus while open; this covers the return leg.
watch(dialogOpen, (open, wasOpen) => {
  if (open && !wasOpen) {
    const active = document.activeElement;
    surfaceTrigger.value =
      active instanceof HTMLElement && !active.closest(".n-modal") ? active : null;
    return;
  }
  if (!open && wasOpen && surfaceTrigger.value) {
    const target = surfaceTrigger.value;
    surfaceTrigger.value = null;
    void nextTick(() => {
      if (target.isConnected && document.activeElement === document.body) {
        target.focus();
      }
    });
  }
});

/**
 * pickEncoding changes the active tab's save charset. A clean tab is re-saved
 * immediately so the file itself moves to the new encoding (an unmodified
 * legacy file must not keep its old bytes under a new label); a dirty tab
 * applies it on the next save.
 */
async function pickEncoding(encoding: EditorEncoding): Promise<void> {
  const tab = activeTab.value;
  if (!tab || tab.kind !== "file" || !files.canWrite) {
    return;
  }
  const outcome = await editor.changeEncoding(
    tab.id,
    teams.activeTeamId,
    encoding,
  );
  if (outcome === "saved") {
    message.success(
      String(t("files.editor.encodingChanged", { encoding: encodingLabel(encoding) })),
    );
    void refreshHistoryCount(tab);
  } else if (outcome === "pending") {
    message.info(
      String(
        t("files.editor.encodingPending", {
          encoding: encodingLabel(encoding),
        }),
      ),
    );
  } else if (outcome === "error") {
    message.error(saveFailureText(tab));
  }
  // "conflict"/"unsupported" arm their own prompts; the store has already
  // reverted the label to the charset on disk when the re-save did not land.
}

/** useUtf8AndSave retries the blocked save as UTF-8, keeping the characters. */
async function useUtf8AndSave(): Promise<void> {
  const warning = editor.encodingWarning;
  const tab = encodingWarningTab.value;
  if (!warning || !tab) {
    editor.dismissEncodingWarning();
    return;
  }
  const outcome = await editor.saveAsUtf8(warning.tabId, teams.activeTeamId);
  if (outcome === "saved") {
    message.success(String(t("files.editor.saved", { name: tab.name })));
    void refreshHistoryCount(tab);
  } else if (outcome === "error") {
    message.error(saveFailureText(tab));
  }
}

/** resetWindowPosition returns the window to its pinned default position. */
function resetWindowPosition(): void {
  editorWindow.resetPosition();
}

/** onWindowResize records the viewport for display-only fitting. */
function onWindowResize(): void {
  viewport.value = { width: window.innerWidth, height: window.innerHeight };
}

/** onWindowDrag applies a title-bar drag, clamped by the store. */
function onWindowDrag(x: number, y: number): void {
  editorWindow.setPosition(
    x,
    y,
    displayedSize.value.width,
    displayedSize.value.height,
  );
}

/** onWindowResize applies a grip resize; the store keeps the opposite edge. */
function onWindowResizeMove(box: WindowBox): void {
  editorWindow.resizeTo(box, viewport.value.width, viewport.value.height);
}

/** onWindowResizeBy applies one keyboard resize step from the slider. */
function onWindowResizeBy(deltaWidth: number, deltaHeight: number): void {
  editorWindow.resizeBy(
    deltaWidth,
    deltaHeight,
    viewport.value.width,
    viewport.value.height,
  );
}

/** minimizeWindow collapses the window into the rail icon and moves focus there. */
function minimizeWindow(): void {
  editorWindow.setMinimized(true);
  void nextTick(() => {
    document.querySelector<HTMLElement>(".rail-btn--editor")?.focus();
  });
}

/**
 * onWindowEsc closes only the topmost surface. A dialog, popover, menu or the
 * find bar owns Escape while it is open, so the window must never collapse
 * underneath it.
 */
function onWindowEsc(event: KeyboardEvent): void {
  if (event.defaultPrevented) {
    return;
  }
  if (tabMenuOpen.value) {
    tabMenuOpen.value = false;
    return;
  }
  if (dialogOpen.value) {
    return;
  }
  // A teleported follower (settings popover, select menu) or the CodeMirror
  // autocomplete tooltip is not tracked by dialogOpen; while one is mounted it
  // consumes Escape itself.
  if (document.querySelector(".n-popover, .n-dropdown, .cm-tooltip") !== null) {
    return;
  }
  // The find bar and go-to-line popover are surfaces too: Escape closes the one
  // on screen before the window itself collapses.
  if (findOpen.value) {
    closeFind();
    return;
  }
  if (gotoOpen.value) {
    closeGotoLine();
    return;
  }
  minimizeWindow();
}

/** restoreWindow reopens the window and returns focus to the active tab. */
function restoreWindow(): void {
  editorWindow.setMinimized(false);
  // The persisted position is kept (clamped to the viewport on resize), so
  // the window reappears where the user left it. Focusing happens in the
  // minimized watcher below, so the rail icon's restore click shares it.
}

/** focusRestoredWindow lands focus back on the active tab after a restore. */
function focusRestoredWindow(): void {
  const tab = document.getElementById(`ed-tab-${editor.activeId}`);
  if (tab) {
    tab.focus();
  } else {
    sessions.get(editor.activeId)?.focus();
  }
  // The window was display:none while collapsed; re-measure the live host.
  sessions.get(editor.activeId)?.requestMeasure();
  mergeSessions.get(editor.activeId)?.requestMeasure();
  revealActiveTab();
}

// Any restore (the title-bar control, the rail icon, or opening a file while
// collapsed) returns focus to the active tab and re-measures the live host,
// which was display:none while the window was collapsed.
watch(
  () => editorWindow.minimized,
  (next, prev) => {
    if (prev === true && next === false) {
      void nextTick(() => focusRestoredWindow());
    }
  },
);

/** focusTab activates one tab and moves DOM focus onto its tab button. */
function focusTab(index: number): void {
  const tab = editor.tabs[index];
  if (!tab) {
    return;
  }
  editor.activate(tab.id);
  void nextTick(() => {
    document.getElementById(`ed-tab-${tab.id}`)?.focus();
  });
}

/** revealActiveTab scrolls the tab strip so the active tab stays in view. */
function revealActiveTab(): void {
  const strip = tabsElement.value;
  const id = editor.activeId;
  if (!strip || !id) {
    return;
  }
  const tab = document.getElementById(`ed-tab-${id}`);
  if (!tab) {
    return;
  }
  const stripRect = strip.getBoundingClientRect();
  const tabRect = tab.getBoundingClientRect();
  strip.scrollLeft = scrollTabIntoView(
    { left: stripRect.left, right: stripRect.right, scrollLeft: strip.scrollLeft },
    { left: tabRect.left, right: tabRect.right },
  );
}

/** cycleTab moves the active tab by one position (Ctrl+Tab / Ctrl+Shift+Tab). */
function cycleTab(delta: number): void {
  const count = editor.tabs.length;
  if (count < 2) {
    return;
  }
  const index = editor.tabs.findIndex((tab) => tab.id === editor.activeId);
  editor.activate(editor.tabs[(index + delta + count) % count]!.id);
}

/** openTabMenu opens the tab context menu at the pointer. */
function openTabMenu(event: MouseEvent, id: string): void {
  tabMenuId.value = id;
  tabMenuX.value = event.clientX;
  tabMenuY.value = event.clientY;
  tabMenuOpen.value = true;
}

/** onTabMouseDown cancels the browser's middle-click autoscroll. */
function onTabMouseDown(event: MouseEvent): void {
  if (event.button === 1) {
    event.preventDefault();
  }
}

/** onTabAuxClick closes a tab on a middle click. */
function onTabAuxClick(event: MouseEvent, id: string): void {
  if (event.button !== 1) {
    return;
  }
  event.preventDefault();
  requestCloseTabs([id]);
}

/** onTabMenuSelect runs one context-menu command on the targeted tab. */
function onTabMenuSelect(key: string): void {
  tabMenuOpen.value = false;
  const id = tabMenuId.value;
  const tab = editor.tabs.find((item) => item.id === id);
  if (!tab) {
    return;
  }
  if (key === "copyPath") {
    void copyText(tab.path, tab.path);
    return;
  }
  const scope: TabCloseScope = key === "close" ? "self" : (key as TabCloseScope);
  requestCloseTabs(
    tabsToClose(editor.tabs, scope, id),
    scope === "self" || scope === "all" ? "" : id,
  );
}

/**
 * requestCloseTabs closes the given tabs after a prompt when any of them is
 * dirty. Nothing is skipped silently: a mixed selection prompts for the dirty
 * ones and closes the rest with them once a choice is made.
 */
function requestCloseTabs(ids: string[], keepId = ""): void {
  // One prompt at a time: a second close request (Alt+W, middle click, menu)
  // must not retarget the dialog the user is already answering.
  if (pendingClose.value !== null || closeSaving.value) {
    return;
  }
  const closing = editor.tabs.filter((tab) => ids.includes(tab.id));
  if (closing.length === 0) {
    return;
  }
  pendingKeepId.value = keepId;
  if (closing.some((tab) => editor.isDirty(tab))) {
    pendingClose.value = closing.map((tab) => tab.id);
    return;
  }
  settleClose(closing.map((tab) => tab.id));
}

/** settleClose closes ids and keeps a scoped close's target active. */
function settleClose(ids: string[]): void {
  const keep = pendingKeepId.value;
  editor.closeMany(ids);
  if (keep && editor.tabs.some((tab) => tab.id === keep)) {
    editor.activate(keep);
  }
  pendingKeepId.value = "";
}

/** cancelClose abandons the close prompt and keeps every tab. */
function cancelClose(): void {
  pendingClose.value = null;
  pendingKeepId.value = "";
}

/** saveAndClose saves the pending dirty tabs, then closes what saved. */
async function saveAndClose(): Promise<void> {
  const ids = pendingClose.value ?? [];
  if (ids.length === 0 || closeSaving.value) {
    return;
  }
  // A tab already writing would be skipped by saveAll and then closed with its
  // in-flight (or newly typed) text unsaved.
  if (editor.tabs.some((tab) => ids.includes(tab.id) && tab.saving)) {
    return;
  }
  closeSaving.value = true;
  try {
    const reports = await editor.saveAll(teams.activeTeamId, ids);
    const failed = new Set<string>();
    for (const report of reports) {
      if (report.outcome === "saved") {
        // A save writes the text it was handed; edits typed while it was in
        // flight keep the tab dirty, so it must stay open instead of being
        // closed as if everything was persisted.
        if (!editor.isDirty(report.tab)) {
          continue;
        }
        failed.add(report.tab.id);
        continue;
      }
      failed.add(report.tab.id);
      if (report.outcome === "conflict") {
        message.warning(
          String(t("files.editor.conflictFile", { name: report.tab.name })),
        );
        continue;
      }
      if (report.outcome === "unsupported") {
        // Keep the tab open; the encoding prompt offers a UTF-8 save.
        continue;
      }
      message.error(saveFailureText(report.tab));
    }
    // A failed tab stays open and dirty; everything else still closes.
    settleClose(ids.filter((id) => !failed.has(id)));
    pendingClose.value = null;
  } finally {
    closeSaving.value = false;
  }
}

/** discardAndClose drops the pending buffers and closes their tabs. */
function discardAndClose(): void {
  const ids = pendingClose.value ?? [];
  pendingClose.value = null;
  settleClose(ids);
}

/** onEditorKeydown handles the editor-wide tab shortcuts. */
function onEditorKeydown(event: KeyboardEvent): void {
  if (
    event.defaultPrevented ||
    event.isComposing ||
    editor.tabs.length === 0 ||
    editorWindow.minimized ||
    pendingClose.value !== null ||
    closeSaving.value
  ) {
    return;
  }
  // A modal (conflict, save-as, discard) owns the keyboard while it is open.
  if (event.target instanceof Element && event.target.closest(".n-modal")) {
    return;
  }
  const mod = event.metaKey || event.ctrlKey;
  if (mod && event.shiftKey && event.code === "KeyS") {
    event.preventDefault();
    void saveAllTabs();
    return;
  }
  if (event.altKey && !mod && event.code === "KeyW") {
    event.preventDefault();
    if (editor.activeId) {
      requestCloseTabs([editor.activeId]);
    }
  }
}

/** focusCloseButton returns focus to the title-bar Close control. */
function focusCloseButton(): void {
  document.querySelector<HTMLElement>(".ed-window-close")?.focus();
}

/** returnFocusToBrowser moves focus off a closing window back to the browser. */
function returnFocusToBrowser(): void {
  const target =
    document.querySelector<HTMLElement>(".fm-search input") ??
    document.querySelector<HTMLElement>(".fm-label");
  target?.focus();
}

/** closeAllAndReturnFocus closes every tab and lands focus back on the page. */
function closeAllAndReturnFocus(): void {
  editor.closeAll();
  void nextTick(() => returnFocusToBrowser());
}

/**
 * closeAllTabs closes the whole editor. When any buffer is dirty it asks first
 * (the dialog lists the unsaved files); with nothing dirty it closes at once,
 * with no dialog.
 */
function closeAllTabs(): void {
  if (editor.hasDirty) {
    closeAllOpen.value = true;
    return;
  }
  closeAllAndReturnFocus();
}

/** confirmCloseAll discards the dirty buffers and closes every tab. */
function confirmCloseAll(): void {
  closeAllOpen.value = false;
  closeAllAndReturnFocus();
}

/** cancelCloseAll keeps the buffers and returns focus to the Close control. */
function cancelCloseAll(): void {
  closeAllOpen.value = false;
  void nextTick(() => focusCloseButton());
}

/** onTabKeydown implements the tablist roving focus (Left/Right/Home/End). */
function onTabKeydown(event: KeyboardEvent, index: number): void {
  const count = editor.tabs.length;
  if (count === 0) {
    return;
  }
  if (event.key === "ArrowRight") {
    event.preventDefault();
    focusTab((index + 1) % count);
  } else if (event.key === "ArrowLeft") {
    event.preventDefault();
    focusTab((index - 1 + count) % count);
  } else if (event.key === "Home") {
    event.preventDefault();
    focusTab(0);
  } else if (event.key === "End") {
    event.preventDefault();
    focusTab(count - 1);
  } else if (event.key === "Delete") {
    // The inline × is presentational (a tablist may only own tabs); Delete on a
    // focused tab is the keyboard path, next to the Alt+W binding.
    event.preventDefault();
    requestCloseTabs([editor.tabs[index]!.id]);
  }
}

/** onBeforeUnload guards a reload/close while a document is dirty. */
function onBeforeUnload(event: BeforeUnloadEvent): void {
  if (!editor.hasDirty) {
    return;
  }
  event.preventDefault();
  // Older browsers need returnValue set to show the confirmation.
  event.returnValue = "";
}

onMounted(() => {
  window.addEventListener("beforeunload", onBeforeUnload);
  window.addEventListener("resize", onWindowResize);
  window.addEventListener("keydown", onEditorKeydown);
  onWindowResize();
  void refreshHistoryCount();
});

onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", onBeforeUnload);
  window.removeEventListener("resize", onWindowResize);
  window.removeEventListener("keydown", onEditorKeydown);
  for (const session of sessions.values()) {
    session.destroy();
  }
  sessions.clear();
  sessionLanguages.clear();
  sessionHosts.clear();
  for (const session of mergeSessions.values()) {
    session.destroy();
  }
  mergeSessions.clear();
  mergeHosts.clear();
  mergeSources.clear();
  // An implicit unmount must never drop a dirty buffer (logout and session
  // changes reset the stores explicitly): reset only when nothing is dirty.
  // The store outlives the component, so a remount restores the tabs.
  if (!editor.hasDirty) {
    editor.reset();
  }
});
</script>

<template>
  <FloatingWindow
    v-if="editor.tabs.length > 0"
    v-show="!editorWindow.minimized"
    class="ed-window files-editor"
    :style="windowStyle"
    :window-label="t('files.editor.windowLabel')"
    :minimize-label="t('files.editor.minimize')"
    :close-label="t('files.editor.closeWindow')"
    :resize-label="t('files.editor.resize')"
    :resize-value-text="resizeValueText"
    :size="displayedSize"
    :resize-limits="resizeLimits"
    :class-names="{
      bar: 'ed-window-bar',
      min: 'ed-window-min',
      close: 'ed-window-close',
      resize: 'ed-resize',
    }"
    @minimize="minimizeWindow"
    @close="closeAllTabs"
    @drag="onWindowDrag"
    @resize="onWindowResizeMove"
    @resize-by="onWindowResizeBy"
    @keydown.esc="onWindowEsc"
  >
    <template #title>{{ activeTab?.name ?? t("files.editor.heading") }}</template>
    <template #context>
      <span v-if="activeTab" class="ed-window-path mono" :title="activeTab.path">
        {{ activeTab.path }}
      </span>
      <span v-if="editor.hasDirty" class="ed-window-dirty">
        {{ t("files.editor.unsaved") }}
      </span>
    </template>
    <template #actions>
      <NButton
        quaternary
        size="tiny"
        class="ed-window-shortcuts"
        :aria-label="t('files.editor.shortcuts.label')"
        @click="shortcutsOpen = true"
      >
        <template #icon>
          <Keyboard :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NButton
        quaternary
        size="tiny"
        class="ed-window-explorer"
        :aria-label="
          explorerOpen
            ? t('files.editor.explorer.hide')
            : t('files.editor.explorer.show')
        "
        :aria-pressed="explorerOpen"
        @click="explorerOpen = !explorerOpen"
      >
        <template #icon>
          <PanelLeft :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </template>

    <div class="ed-main">
      <EditorExplorer
        v-if="explorerOpen"
        :server-id="activeServerId"
        :width="explorerWidth"
        @resize="(width: number) => (explorerWidth = width)"
      />
      <div class="ed-right">
        <div
          ref="tabsElement"
          class="ed-tabs"
          role="tablist"
          :aria-label="t('files.editor.heading')"
        >
          <div
            v-for="(tab, index) in editor.tabs"
            :key="tab.id"
            class="ed-tab"
            role="presentation"
            :class="{ 'is-active': tab.id === editor.activeId, 'is-dirty': editor.isDirty(tab) }"
            @mousedown="onTabMouseDown"
            @contextmenu.prevent="openTabMenu($event, tab.id)"
            @auxclick="onTabAuxClick($event, tab.id)"
          >
            <button
              type="button"
              class="ed-tab-name"
              role="tab"
              :id="'ed-tab-' + tab.id"
              :aria-selected="tab.id === editor.activeId"
              :aria-controls="'ed-panel-' + tab.id"
              :tabindex="tab.id === editor.activeId ? 0 : -1"
              @click="editor.activate(tab.id)"
              @keydown="onTabKeydown($event, index)"
            >
              <span v-if="editor.isDirty(tab)" class="ed-dot" aria-hidden="true" />
              {{ tab.name }}
            </button>
            <!-- A tablist may only own tabs, so the inline close is
                 presentational: keyboard users close the focused tab with
                 Delete (or Alt+W anywhere in the editor), and the mouse keeps
                 the inline affordance. -->
            <span
              class="ed-tab-close"
              role="presentation"
              aria-hidden="true"
              @click.stop="requestCloseTabs([tab.id])"
            >
              <X :size="13" :stroke-width="2" aria-hidden="true" />
            </span>
          </div>
        </div>

        <div v-if="activeTab" class="ed-body">
          <div v-if="activeTab.status === 'ready' && activeTab.kind === 'file'" class="ed-toolbar">
            <NButton
              size="tiny"
              secondary
              :type="editorSettings.settings.wrap ? 'primary' : 'default'"
              :aria-pressed="editorSettings.settings.wrap"
              @click="toggleWrap"
            >
              <template #icon>
                <WrapText :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.wrap") }}
            </NButton>
            <NButton size="tiny" secondary @click="openFind(false)">
              <template #icon>
                <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.search") }}
            </NButton>
            <NButton size="tiny" secondary @click="openGotoLine">
              <template #icon>
                <CaseSensitive :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.goToLine") }}
            </NButton>
            <EditorSettingsPopover />
            <NButton
              v-if="files.canWrite && !activeTab.truncated"
              size="tiny"
              secondary
              @click="openHistory"
            >
              <template #icon>
                <History :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.history") }}
              <span v-if="historyCount > 0" class="ed-history-count">{{ historyCount }}</span>
            </NButton>
            <span class="grow" />
            <NButton
              v-if="files.canWrite"
              size="small"
              secondary
              :disabled="!editor.hasDirty"
              :loading="editor.dirtyTabs.some((tab) => tab.saving)"
              @click="saveAllTabs"
            >
              <template #icon>
                <SaveAll :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.saveAll") }}
            </NButton>
            <NButton
              v-if="files.canWrite"
              size="small"
              type="primary"
              :loading="activeTab.saving"
              :disabled="!editor.isDirty(activeTab) || activeTab.truncated"
              @click="() => void saveTab(activeTab!.id)"
            >
              <template #icon>
                <Save :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ activeTab.saving ? t("files.editor.saving") : t("files.editor.save") }}
            </NButton>
          </div>

          <div v-if="activeTab.kind === 'diff'" class="ed-toolbar">
            <span class="small muted">{{ t("files.editor.historyDiffReadOnly") }}</span>
            <span class="grow" />
            <NButton size="tiny" secondary @click="openFind(false)">
              <template #icon>
                <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.search") }}
            </NButton>
            <NButton size="tiny" secondary @click="openGotoLine">
              <template #icon>
                <CaseSensitive :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.goToLine") }}
            </NButton>
          </div>

          <!-- A file over the editor ceiling opens as a read-only tail: say so
               and keep the download one click away. -->
          <div v-if="activeTab.truncated" class="ed-truncated">
            <NAlert type="warning" :bordered="false">
              <div class="ed-truncated-body">
                <span>
                  {{ t("files.editor.truncatedHint", { name: activeTab.name, size: formatBytes(activeTab.size) }) }}
                </span>
                <NButton size="tiny" secondary @click="downloadActive">
                  {{ t("files.editor.download", { name: activeTab.name }) }}
                </NButton>
              </div>
            </NAlert>
          </div>

          <!-- Custom find/replace bar (JUS-231): in flow above the content, so
               the status bar keeps its place and the editor shrinks instead. -->
          <EditorSearchBar
            :show="findOpen && activeTab.status === 'ready'"
            :expanded="replaceExpanded"
            :can-replace="canReplaceInTab"
            :query="findQuery"
            :replacement="findReplacement"
            :case-sensitive="findCaseSensitive"
            :whole-word="findWholeWord"
            :regexp="findRegexp"
            :pattern-error="findPatternError"
            :total="findTotal"
            :capped="findCapped"
            :current="findCurrent"
            :focus-replace="findFocusReplace"
            @close="closeFind"
            @update:query="setFindQuery"
            @update:replacement="setFindReplacement"
            @toggle:expanded="toggleReplace"
            @toggle:case="toggleCase"
            @toggle:word="toggleWord"
            @toggle:regex="toggleRegex"
            @next="nextMatch"
            @previous="previousMatch"
            @replace="replaceMatch"
            @replace-all="replaceEveryMatch"
          />

          <div class="ed-panes" :class="{ 'is-hidden': activeTab.status !== 'ready' }">
            <EditorGotoLine
              :show="gotoOpen && activeTab.status === 'ready'"
              :current="gotoCurrent"
              :total="gotoTotal"
              :jump="jumpToLine"
              @close="closeGotoLine"
            />

            <!-- One host per ready tab, kept mounted across a status switch (a
                 loading/large/binary/error tab must not detach a live session). -->
            <div
              v-for="tab in readyTabs"
              :key="tab.id"
              v-show="tab.id === editor.activeId"
              :id="'ed-panel-' + tab.id"
              :ref="(el) => setHost(tab.id, el)"
              class="ed-host"
              role="tabpanel"
              :aria-labelledby="'ed-tab-' + tab.id"
            />

            <!-- History diffs are read-only MergeViews, one host per ready diff tab. -->
            <div
              v-for="tab in readyDiffTabs"
              :key="tab.id"
              v-show="tab.id === editor.activeId"
              :id="'ed-panel-' + tab.id"
              :ref="(el) => setMergeHost(tab.id, el)"
              class="ed-host ed-diff-host"
              role="tabpanel"
              :aria-labelledby="'ed-tab-' + tab.id"
            />
          </div>

          <div
            v-if="activeTab.status === 'loading'"
            class="ed-state"
            role="tabpanel"
            :id="'ed-panel-' + activeTab.id"
            :aria-labelledby="'ed-tab-' + activeTab.id"
          >
            <NSpin size="medium" :aria-label="t('files.editor.loading', { name: activeTab.name })" />
            <p class="small muted">{{ t("files.editor.loading", { name: activeTab.name }) }}</p>
          </div>

          <div
            v-else-if="activeTab.status === 'large'"
            class="ed-state"
            role="tabpanel"
            :id="'ed-panel-' + activeTab.id"
            :aria-labelledby="'ed-tab-' + activeTab.id"
          >
            <NEmpty :description="t('files.editor.largeTitle')">
              <template #extra>
                <p class="small muted">
                  {{ t("files.editor.largeHint", { name: activeTab.name }) }}
                </p>
                <NButton size="small" secondary @click="downloadActive">
                  {{ t("files.editor.download", { name: activeTab.name }) }}
                </NButton>
              </template>
            </NEmpty>
          </div>

          <div
            v-else-if="activeTab.status === 'binary'"
            class="ed-state"
            role="tabpanel"
            :id="'ed-panel-' + activeTab.id"
            :aria-labelledby="'ed-tab-' + activeTab.id"
          >
            <NEmpty :description="t('files.editor.binaryTitle')">
              <template #extra>
                <p class="small muted">
                  {{ t("files.editor.binaryHint", { name: activeTab.name }) }}
                </p>
                <NButton size="small" secondary @click="downloadActive">
                  {{ t("files.editor.download", { name: activeTab.name }) }}
                </NButton>
              </template>
            </NEmpty>
          </div>

          <div
            v-else-if="activeTab.status === 'mixed'"
            class="ed-state"
            role="tabpanel"
            :id="'ed-panel-' + activeTab.id"
            :aria-labelledby="'ed-tab-' + activeTab.id"
          >
            <NEmpty :description="t('files.editor.mixedTitle')">
              <template #extra>
                <p class="small muted">
                  {{ t("files.editor.mixedHint", { name: activeTab.name }) }}
                </p>
                <NButton size="small" secondary @click="downloadActive">
                  {{ t("files.editor.download", { name: activeTab.name }) }}
                </NButton>
              </template>
            </NEmpty>
          </div>

          <div
            v-else-if="activeTab.status === 'error'"
            class="ed-state"
            role="tabpanel"
            :id="'ed-panel-' + activeTab.id"
            :aria-labelledby="'ed-tab-' + activeTab.id"
          >
            <NEmpty :description="t('files.editor.errorTitle', { name: activeTab.name })">
              <template #extra>
                <p class="small muted">{{ activeTab.error }}</p>
              </template>
            </NEmpty>
          </div>

          <EditorStatusBar
            :tab="activeTab"
            :cursor="cursor"
            :history-count="historyCount"
            :can-write="files.canWrite"
            :tab-size="editorSettings.settings.tabSize"
            :dirty="editor.isDirty(activeTab)"
            @select-language="pickLanguage"
            @set-eol="pickEol"
            @set-tab-size="pickTabSize"
            @open-encoding="encodingOpen = true"
            @open-history="openHistory"
          />
        </div>
      </div>
    </div>
  </FloatingWindow>

  <NDropdown
    :show="tabMenuOpen"
    trigger="manual"
    placement="bottom-start"
    :x="tabMenuX"
    :y="tabMenuY"
    :options="tabMenuOptions"
    :menu-props="() => ({ role: 'menu', 'aria-label': t('files.editor.tabMenu') })"
    @update:show="tabMenuOpen = $event"
    @select="onTabMenuSelect"
  />

  <NModal
    class="app-modal"
    :show="editor.leaveOpen"
    preset="card"
    style="width: 440px; max-width: 94vw"
    :title="t('files.editor.leaveTitle')"
    :mask-closable="false"
    :close-on-esc="false"
    @update:show="(value: boolean) => { if (!value) editor.resolveLeave(false); }"
  >
    <p class="small">{{ t("files.editor.leaveMessage") }}</p>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="editor.resolveLeave(false)">{{ t("files.editor.leaveStay") }}</NButton>
        <NButton type="error" @click="editor.resolveLeave(true)">
          {{ t("files.editor.leaveDiscard") }}
        </NButton>
      </div>
    </template>
  </NModal>

  <NModal
    class="app-modal files-editor-close-modal"
    :show="closeAllOpen"
    preset="card"
    style="width: 440px; max-width: 94vw"
    :title="t('files.editor.closeAllTitle')"
    :mask-closable="false"
    :close-on-esc="false"
    @update:show="(value: boolean) => { if (!value) cancelCloseAll(); }"
  >
    <p class="small">{{ t("files.editor.closeAllMessage") }}</p>
    <ul class="ed-unsaved" :aria-label="t('files.editor.closeAllList')">
      <li v-for="tab in editor.dirtyTabs" :key="tab.id" class="mono">{{ tab.path }}</li>
    </ul>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="cancelCloseAll">{{ t("files.editor.closeAllCancel") }}</NButton>
        <NButton type="error" @click="confirmCloseAll">
          {{ t("files.editor.closeAllDiscard") }}
        </NButton>
      </div>
    </template>
  </NModal>

  <EditorShortcutsDialog v-model:show="shortcutsOpen" />
  <EditorCloseTabsDialog
    :show="pendingClose !== null"
    :paths="pendingClosePaths"
    :can-save="files.canWrite"
    :saving="closeSaving"
    @save="saveAndClose"
    @discard="discardAndClose"
    @cancel="cancelClose"
  />
  <EditorDraftDialog
    :prompt="draftPrompt"
    @restore="restoreDraftPrompt"
    @discard="discardDraftPrompt"
  />
  <EditorConflictDialog />
  <EditorSaveAsDialog />
  <EditorEncodingDialog
    v-model:show="encodingOpen"
    :encoding="activeTab?.encoding ?? 'utf-8'"
    @select="(encoding) => void pickEncoding(encoding)"
  />
  <EditorEncodingUnsupportedDialog
    :show="editor.encodingWarning !== null"
    :name="encodingWarningTab?.name ?? ''"
    :encoding="editor.encodingWarning?.encoding ?? 'utf-8'"
    :characters="editor.encodingWarning?.characters ?? []"
    @update:show="(value: boolean) => { if (!value) editor.dismissEncodingWarning(); }"
    @use-utf8="() => void useUtf8AndSave()"
  />
  <EditorHistoryDialog
    ref="historyDialog"
    v-model:show="historyOpen"
    :server-id="activeServerId"
    :team-id="teams.activeTeamId"
    :root="historyTarget?.root ?? ''"
    :path="historyTarget?.path ?? ''"
    :file-name="historyTarget?.name ?? ''"
    :dirty="historyTarget ? editor.isDirty(historyTarget) : false"
    @diff="(version) => void openDiff(version)"
    @restore="(version) => void restoreVersion(version)"
  />
</template>

<style scoped>
.ed-window {
  right: var(--space-6);
  bottom: var(--space-4);
  width: min(1331px, calc(100vw - var(--space-6) * 2));
  height: min(936px, calc(100vh - var(--ed-window-chrome, 104px)));
  height: min(936px, calc(100dvh - var(--ed-window-chrome, 104px)));
  /* The minimum yields to narrow viewports so the window always fits with
     its side margins; the maximum caps the inline size to the same margins
     (the inline style owns the real value, the caps only trim overflow). */
  min-width: min(480px, calc(100vw - var(--space-6) * 2));
  min-height: min(320px, calc(100vh - var(--space-6) * 2));
  max-width: calc(100vw - var(--space-6) * 2);
  max-height: calc(100vh - var(--space-6) * 2);
  max-height: calc(100dvh - var(--space-6) * 2);
  z-index: 70;
}

.ed-window-path {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

.ed-window-dirty {
  color: var(--warn-ink);
  font-size: var(--text-xs);
  white-space: nowrap;
}

.ed-main {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
}

/* The editor column keeps the tab strip, toolbar, pane and footer while the
   explorer panel sits beside it; min-width:0 lets the pane shrink instead of
   pushing the sidebar off the window. */
.ed-right {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
}

.ed-tabs {
  flex: 0 0 auto;
  display: flex;
  align-items: stretch;
  gap: 2px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
}

.ed-body {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
  padding: 0 var(--space-3) var(--space-3);
}

/* The panes share one positioned box so the go-to-line popover can sit above
   the active editor without entering the flex flow. */
.ed-panes {
  position: relative;
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
}

/* A loading/large/binary/error tab owns the content area; the ready hosts stay
   mounted but must not split the space with its state panel. */
.ed-panes.is-hidden {
  display: none;
}

.ed-tab {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 0 var(--space-1) 0 var(--space-3);
  border: 1px solid transparent;
  border-bottom: none;
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  color: var(--muted);
  white-space: nowrap;
}

.ed-tab.is-active {
  background: var(--surface-warm);
  border-color: var(--border);
  color: var(--fg-2);
}

.ed-tab-name {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) 0;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  cursor: pointer;
}

.ed-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--warn);
}

.ed-tab-close {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border: 0;
  border-radius: 4px;
  background: none;
  color: var(--muted);
  cursor: pointer;
}

.ed-tab-close:hover {
  background: var(--border);
  color: var(--fg-2);
}

.ed-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) 0;
}

.ed-truncated {
  flex: 0 0 auto;
  padding-bottom: var(--space-2);
}

.ed-truncated-body {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}

.ed-host {
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}

.ed-host :deep(.cm-editor) {
  height: 100%;
}

.ed-host :deep(.cm-editor.cm-focused) {
  outline: none;
}

/* The merge view stacks its two panes; both must fill the tab host. */
.ed-diff-host :deep(.cm-mergeView),
.ed-diff-host :deep(.cm-mergeViewEditors) {
  height: 100%;
}

.ed-diff-host :deep(.cm-mergeViewEditors) {
  display: flex;
  flex-direction: row;
  align-items: stretch;
}

.ed-history-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 var(--space-1);
  border-radius: var(--radius-pill);
  background: var(--accent-softer);
  color: var(--fg);
  font-size: var(--text-xs);
  font-variant-numeric: tabular-nums;
}

.ed-state {
  display: grid;
  place-items: center;
  flex: 1 1 auto;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-4);
}

.ed-toolbar .grow {
  flex: 1 1 auto;
}

.mono {
  font-family: var(--font-mono);
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}

.ed-unsaved {
  margin: var(--space-2) 0 0;
  padding-left: var(--space-4);
  max-height: 40vh;
  overflow-y: auto;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.ed-unsaved li {
  overflow-wrap: anywhere;
}

.small {
  font-size: var(--text-sm);
}
</style>
