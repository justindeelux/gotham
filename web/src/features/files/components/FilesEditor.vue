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
import AppTooltip from "@/shared/ui/AppTooltip.vue";
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
import { useFilesPaneSession } from "@/features/files/stores/paneSession";
import type { FilesPaneId } from "@/features/files/stores/paneSession";
import type { EditorTab } from "@/features/files/stores/editor";
import { useEditorWindowStore } from "@/features/files/stores/editorWindow";
import { EDITOR_WINDOW_CHROME_MARGIN } from "@/features/files/schemas/editorWindow";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import { provideFilesPane } from "@/features/files/stores/filesPane";
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
import { useCopyText } from "@/shared/composables/useCopyText";
import FloatingWindow from "@/shared/ui/FloatingWindow.vue";
import type { WindowBox } from "@/shared/window/geometry";
import { formatBytes, formatDateTime } from "@/shared/utils/format";

interface Props {
  serverId?: string;
  pane?: FilesPaneId;
}

const props = withDefaults(defineProps<Props>(), { serverId: "" });

const { t } = useI18n();
const message = useMessage();
// global dirty guard only registers when one is injected (JUS-232).
const router = inject<Router | null>(routerKey as InjectionKey<Router | null>, null);
const paneId =
  props.pane ?? useFilesPaneSession().activePane;
const filesPane = provideFilesPane(paneId);
const editor = filesPane.editor;
const editorWindow = useEditorWindowStore();
const editorSettings = useEditorSettingsStore();
const files = filesPane.files;
const { download } = useFileDownload();
const activeServerId = computed<string>(() =>
  props.serverId !== "" ? props.serverId : editor.serverId,
);
const {
  prompt: draftPrompt,
  restore: restoreDraftPrompt,
  discard: discardDraftPrompt,
} = useEditorDrafts(() => activeServerId.value, paneId);

const sessions = new Map<string, EditorSession>();
const hosts = new Map<string, HTMLElement>();
const sessionLanguages = new Map<string, EditorLanguageId>();
const sessionHosts = new Map<string, HTMLElement>();
const creating = new Map<string, Promise<void>>();
const mergeSessions = new Map<string, MergeSession>();
const mergeHosts = new Map<string, HTMLElement>();
const mergeCreating = new Map<string, Promise<void>>();
const mergeSources = new Map<string, { base: string; current: string }>();
const cursor = ref<{ line: number; column: number }>({ line: 1, column: 1 });
const explorerOpen = ref(true);
const explorerWidth = ref(260);
const offset = computed<{ x: number; y: number } | null>(() =>
  editorWindow.x !== null && editorWindow.y !== null
    ? { x: editorWindow.x, y: editorWindow.y }
    : null,
);
const viewport = ref({
  width: typeof window === "undefined" ? 0 : window.innerWidth,
  height: typeof window === "undefined" ? 0 : window.innerHeight,
});
const closeAllOpen = ref(false);
const historyOpen = ref(false);
const encodingOpen = ref(false);
const shortcutsOpen = ref(false);
const surfaceTrigger = ref<HTMLElement | null>(null);
const historyDialog = ref<InstanceType<typeof EditorHistoryDialog> | null>(null);
const historyCounts = ref<Record<string, number>>({});

const tabsElement = ref<HTMLElement | null>(null);
const tabMenuOpen = ref(false);
const tabMenuX = ref(0);
const tabMenuY = ref(0);
const tabMenuId = ref("");
const pendingClose = ref<string[] | null>(null);
const pendingKeepId = ref("");
const closeSaving = ref(false);
const { copyText } = useCopyText();

const displayedSize = computed<{ width: number; height: number }>(() => {
  const fitted = editorWindow.displayGeometry(viewport.value.width, viewport.value.height);
  return { width: fitted.width, height: fitted.height };
});

const resizeLimits = computed(() =>
  editorResizeLimits(viewport.value.width, viewport.value.height),
);

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

const readyDiffTabs = computed(() =>
  editor.tabs.filter((tab) => tab.kind === "diff" && tab.status === "ready"),
);

const activeTab = computed(() => editor.activeTab);

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

const historyTarget = computed<EditorTab | null>(() =>
  activeTab.value?.kind === "file" ? activeTab.value : null,
);

const historyCount = computed<number>(() =>
  activeTab.value ? (historyCounts.value[activeTab.value.id] ?? 0) : 0,
);

const historyTooltip = computed<string>(() =>
  historyCount.value > 0
    ? t("files.editor.historyWithCount", { count: historyCount.value })
    : t("files.editor.history"),
);

const pendingCloseDirty = computed<EditorTab[]>(() => {
  const ids = pendingClose.value ?? [];
  return editor.tabs.filter((tab) => ids.includes(tab.id) && editor.isDirty(tab));
});

const pendingClosePaths = computed<string[]>(() =>
  pendingCloseDirty.value.map((tab) => tab.path),
);

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
      if (outcome === "stay" && editor.serverId !== "") {
        void router?.push({ name: "server-files", params: { id: editor.serverId } });
      }
    });
  },
);



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

watch(
  () => files.canWrite,
  (canWrite) => {
    for (const [id, session] of sessions) {
      const tab = editor.tabs.find((item) => item.id === id);
      session.setReadOnly(!canWrite || tab?.truncated === true);
    }
  },
);

watch(
  () => editor.activeId,
  () => {
    if (editorWindow.minimized) {
      restoreWindow();
    }
  },
);

watch(
  () => [editor.activeId, editor.tabs.length] as const,
  () => void nextTick(() => revealActiveTab()),
  { immediate: true },
);

watch(
  () => editor.tabs.length,
  (count) => {
    if (count === 0) {
      resetWindowPosition();
    }
  },
);

function setHost(id: string, el: unknown): void {
  if (el instanceof HTMLElement) {
    hosts.set(id, el);
  } else {
    hosts.delete(id);
  }
}

function setMergeHost(id: string, el: unknown): void {
  if (el instanceof HTMLElement) {
    mergeHosts.set(id, el);
  } else {
    mergeHosts.delete(id);
  }
}

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

async function refreshHistoryCount(tab: EditorTab | null = activeTab.value): Promise<void> {
  if (!tab || tab.kind !== "file" || tab.status !== "ready") {
    return;
  }
  try {
    const versions = await listFileVersions(
      activeServerId.value,
      tab.root,
      tab.path,
    );
    historyCounts.value = { ...historyCounts.value, [tab.id]: versions.length };
  } catch {
  }
}

function openHistory(): void {
  if (!historyTarget.value || historyTarget.value.truncated) {
    return;
  }
  historyOpen.value = true;
}

async function openDiff(version: FilesVersion): Promise<void> {
  const tab = historyTarget.value;
  if (!tab) {
    return;
  }
  await editor.openDiff(
    activeServerId.value,
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

async function restoreVersion(version: FilesVersion): Promise<void> {
  const tab = historyTarget.value;
  if (!tab || tab.truncated) {
    return;
  }
  try {
    await restoreFileVersion(
      activeServerId.value,
      tab.root,
      tab.path,
      version.id,
    );
    for (const item of editor.tabs) {
      if (item.kind === "diff" && item.root === tab.root && item.path === tab.path) {
        editor.close(item.id);
      }
    }
    await editor.reload(tab.id);
    message.success(String(t("files.editor.historyRestored", { name: tab.name })));
    void files.refresh();
    await refreshHistoryCount(editor.tabs.find((item) => item.id === tab.id) ?? null);
    await historyDialog.value?.reload();
  } catch (err) {
    message.error(describeFilesError(err));
  }
}

function isTabFocused(): boolean {
  const active = document.activeElement;
  return active instanceof HTMLElement && active.getAttribute("role") === "tab";
}

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
    if (sessionHosts.get(tab.id) === host) {
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
    if (editor.tabs.some((item) => item.id === tabId)) {
      sessions.set(tabId, session);
      sessionHosts.set(tabId, host);
      sessionLanguages.set(tabId, tab.language);
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

async function saveTab(id: string): Promise<void> {
  const tab = editor.tabs.find((item) => item.id === id);
  if (
    !tab ||
    tab.saving ||
    tab.status !== "ready" ||
    !files.canWrite ||
    !editor.isDirty(tab)
  ) {
    return;
  }
  const outcome = await editor.save(id);
  if (outcome === "saved") {
    message.success(String(t("files.editor.saved", { name: tab.name })));
    void refreshHistoryCount(tab);
  } else if (outcome === "error") {
    message.error(tab.error || String(t("files.editor.saveFailed", { name: tab.name })));
  }
}

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

async function saveAllTabs(): Promise<void> {
  if (!files.canWrite || !editor.hasDirty) {
    return;
  }
  if (editor.dirtyTabs.some((tab) => tab.saving)) {
    return;
  }
  const reports = await editor.saveAll();
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
      continue;
    }
    message.error(saveFailureText(report.tab));
  }
  if (saved > 0 && saved === reports.length) {
    message.success(String(t("files.editor.saveAllDone")));
  }
}

async function downloadActive(): Promise<void> {
  const tab = activeTab.value;
  if (!tab) {
    return;
  }
  await download(activeServerId.value, tab.root, directoryOf(tab.path), {
    name: tab.name,
  });
}

function directoryOf(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}

function toggleWrap(): void {
  editorSettings.update({ wrap: !editorSettings.settings.wrap });
}

function pickLanguage(language: EditorLanguageId): void {
  const tab = activeTab.value;
  if (tab) {
    editor.setLanguage(tab.id, language);
  }
}

function pickEol(eol: EditorEol): void {
  const tab = activeTab.value;
  if (tab) {
    editor.setEol(tab.id, eol);
  }
}

function pickTabSize(size: number): void {
  editorSettings.update({ tabSize: size as EditorSettings["tabSize"] });
}

const encodingWarningTab = computed<EditorTab | null>(
  () =>
    editor.tabs.find((tab) => tab.id === editor.encodingWarning?.tabId) ?? null,
);

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

async function pickEncoding(encoding: EditorEncoding): Promise<void> {
  const tab = activeTab.value;
  if (!tab || tab.kind !== "file" || !files.canWrite) {
    return;
  }
  const outcome = await editor.changeEncoding(
    tab.id,
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
}

async function useUtf8AndSave(): Promise<void> {
  const warning = editor.encodingWarning;
  const tab = encodingWarningTab.value;
  if (!warning || !tab) {
    editor.dismissEncodingWarning();
    return;
  }
  const outcome = await editor.saveAsUtf8(warning.tabId);
  if (outcome === "saved") {
    message.success(String(t("files.editor.saved", { name: tab.name })));
    void refreshHistoryCount(tab);
  } else if (outcome === "error") {
    message.error(saveFailureText(tab));
  }
}

function resetWindowPosition(): void {
  editorWindow.resetPosition();
}

function onWindowResize(): void {
  viewport.value = { width: window.innerWidth, height: window.innerHeight };
}

function onWindowDrag(x: number, y: number): void {
  editorWindow.setPosition(
    x,
    y,
    displayedSize.value.width,
    displayedSize.value.height,
  );
}

function onWindowResizeMove(box: WindowBox): void {
  editorWindow.resizeTo(box, viewport.value.width, viewport.value.height);
}

function onWindowResizeBy(deltaWidth: number, deltaHeight: number): void {
  editorWindow.resizeBy(
    deltaWidth,
    deltaHeight,
    viewport.value.width,
    viewport.value.height,
  );
}

function minimizeWindow(): void {
  editorWindow.setMinimized(true);
  void nextTick(() => {
    document.querySelector<HTMLElement>(".rail-btn--editor")?.focus();
  });
}

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
  if (document.querySelector(".n-popover, .n-dropdown, .cm-tooltip") !== null) {
    return;
  }
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

function restoreWindow(): void {
  editorWindow.setMinimized(false);
}

function focusRestoredWindow(): void {
  const tab = document.getElementById(`ed-tab-${editor.activeId}`);
  if (tab) {
    tab.focus();
  } else {
    sessions.get(editor.activeId)?.focus();
  }
  sessions.get(editor.activeId)?.requestMeasure();
  mergeSessions.get(editor.activeId)?.requestMeasure();
  revealActiveTab();
}

watch(
  () => editorWindow.minimized,
  (next, prev) => {
    if (prev === true && next === false) {
      void nextTick(() => focusRestoredWindow());
    }
  },
);

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

function cycleTab(delta: number): void {
  const count = editor.tabs.length;
  if (count < 2) {
    return;
  }
  const index = editor.tabs.findIndex((tab) => tab.id === editor.activeId);
  editor.activate(editor.tabs[(index + delta + count) % count]!.id);
}

function openTabMenu(event: MouseEvent, id: string): void {
  tabMenuId.value = id;
  tabMenuX.value = event.clientX;
  tabMenuY.value = event.clientY;
  tabMenuOpen.value = true;
}

function onTabMouseDown(event: MouseEvent): void {
  if (event.button === 1) {
    event.preventDefault();
  }
}

function onTabAuxClick(event: MouseEvent, id: string): void {
  if (event.button !== 1) {
    return;
  }
  event.preventDefault();
  requestCloseTabs([id]);
}

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

function requestCloseTabs(ids: string[], keepId = ""): void {
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

function settleClose(ids: string[]): void {
  const keep = pendingKeepId.value;
  editor.closeMany(ids);
  if (keep && editor.tabs.some((tab) => tab.id === keep)) {
    editor.activate(keep);
  }
  pendingKeepId.value = "";
}

function cancelClose(): void {
  pendingClose.value = null;
  pendingKeepId.value = "";
}

async function saveAndClose(): Promise<void> {
  const ids = pendingClose.value ?? [];
  if (ids.length === 0 || closeSaving.value) {
    return;
  }
  if (editor.tabs.some((tab) => ids.includes(tab.id) && tab.saving)) {
    return;
  }
  closeSaving.value = true;
  try {
    const reports = await editor.saveAll(ids);
    const failed = new Set<string>();
    for (const report of reports) {
      if (report.outcome === "saved") {
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
        continue;
      }
      message.error(saveFailureText(report.tab));
    }
    settleClose(ids.filter((id) => !failed.has(id)));
    pendingClose.value = null;
  } finally {
    closeSaving.value = false;
  }
}

function discardAndClose(): void {
  const ids = pendingClose.value ?? [];
  pendingClose.value = null;
  settleClose(ids);
}

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

function focusCloseButton(): void {
  document.querySelector<HTMLElement>(".ed-window-close")?.focus();
}

function returnFocusToBrowser(): void {
  const target =
    document.querySelector<HTMLElement>(".fm-search input") ??
    document.querySelector<HTMLElement>(".fm-label");
  target?.focus();
}

function closeAllAndReturnFocus(): void {
  editor.closeAll();
  void nextTick(() => returnFocusToBrowser());
}

function closeAllTabs(): void {
  if (editor.hasDirty) {
    closeAllOpen.value = true;
    return;
  }
  closeAllAndReturnFocus();
}

function confirmCloseAll(): void {
  closeAllOpen.value = false;
  closeAllAndReturnFocus();
}

function cancelCloseAll(): void {
  closeAllOpen.value = false;
  void nextTick(() => focusCloseButton());
}

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
    event.preventDefault();
    requestCloseTabs([editor.tabs[index]!.id]);
  }
}

function onBeforeUnload(event: BeforeUnloadEvent): void {
  if (!editor.hasDirty) {
    return;
  }
  event.preventDefault();
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
    :resize-value-text="
      t('files.editor.resizeValue', {
        width: displayedSize.width,
        height: displayedSize.height,
      })
    "
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
            <AppTooltip :label="t('files.editor.wrap')">
              <NButton
                size="tiny"
                secondary
                :type="editorSettings.settings.wrap ? 'primary' : 'default'"
                :aria-pressed="editorSettings.settings.wrap"
                :aria-label="t('files.editor.wrap')"
                @click="toggleWrap"
              >
                <template #icon>
                  <WrapText :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                {{ t("files.editor.wrap") }}
              </NButton>
            </AppTooltip>
            <AppTooltip :label="t('files.editor.search')">
              <NButton
                size="tiny"
                secondary
                :aria-label="t('files.editor.search')"
                @click="openFind(false)"
              >
                <template #icon>
                  <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                {{ t("files.editor.search") }}
              </NButton>
            </AppTooltip>
            <AppTooltip :label="t('files.editor.goToLine')">
              <NButton
                size="tiny"
                secondary
                :aria-label="t('files.editor.goToLine')"
                @click="openGotoLine"
              >
                <template #icon>
                  <CaseSensitive :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                {{ t("files.editor.goToLine") }}
              </NButton>
            </AppTooltip>
            <EditorSettingsPopover />
            <AppTooltip v-if="files.canWrite && !activeTab.truncated" :label="historyTooltip">
              <NButton
                size="tiny"
                secondary
                :aria-label="t('files.editor.history')"
                @click="openHistory"
              >
                <template #icon>
                  <History :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                {{ t("files.editor.history") }}
                <span v-if="historyCount > 0" class="ed-history-count">{{ historyCount }}</span>
              </NButton>
            </AppTooltip>
            <div class="ed-toolbar-actions">
              <AppTooltip v-if="files.canWrite" :label="t('files.editor.saveAll')">
                <NButton
                  size="small"
                  secondary
                  :aria-label="t('files.editor.saveAll')"
                  :disabled="!editor.hasDirty"
                  :loading="editor.dirtyTabs.some((tab) => tab.saving)"
                  @click="saveAllTabs"
                >
                  <template #icon>
                    <SaveAll :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                  {{ t("files.editor.saveAll") }}
                </NButton>
              </AppTooltip>
              <AppTooltip
                v-if="files.canWrite"
                :label="activeTab.saving ? t('files.editor.saving') : t('files.editor.save')"
              >
                <NButton
                  size="small"
                  type="primary"
                  :loading="activeTab.saving"
                  :disabled="!editor.isDirty(activeTab) || activeTab.truncated"
                  :aria-label="activeTab.saving ? t('files.editor.saving') : t('files.editor.save')"
                  @click="() => void saveTab(activeTab!.id)"
                >
                  <template #icon>
                    <Save :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                  {{ activeTab.saving ? t("files.editor.saving") : t("files.editor.save") }}
                </NButton>
              </AppTooltip>
            </div>
          </div>

          <div v-if="activeTab.kind === 'diff'" class="ed-toolbar">
            <span class="ed-diff-note small muted">{{ t("files.editor.historyDiffReadOnly") }}</span>
            <span class="grow" />
            <AppTooltip :label="t('files.editor.search')">
              <NButton
                size="tiny"
                secondary
                :aria-label="t('files.editor.search')"
                @click="openFind(false)"
              >
                <template #icon>
                  <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                {{ t("files.editor.search") }}
              </NButton>
            </AppTooltip>
            <AppTooltip :label="t('files.editor.goToLine')">
              <NButton
                size="tiny"
                secondary
                :aria-label="t('files.editor.goToLine')"
                @click="openGotoLine"
              >
                <template #icon>
                  <CaseSensitive :size="14" :stroke-width="1.8" aria-hidden="true" />
                </template>
                {{ t("files.editor.goToLine") }}
              </NButton>
            </AppTooltip>
          </div>

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
  position: fixed;
  right: var(--space-6);
  bottom: var(--space-4);
  width: min(1331px, calc(100vw - var(--space-6) * 2));
  height: min(936px, calc(100vh - var(--ed-window-chrome, 104px)));
  height: min(936px, calc(100dvh - var(--ed-window-chrome, 104px)));
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

.ed-right {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  container-type: inline-size;
  container-name: editor;
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

.ed-panes {
  position: relative;
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
}

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
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  row-gap: var(--space-1);
  padding: var(--space-2) 0;
}

.ed-toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-left: auto;
}

.ed-diff-note {
  min-width: 0;
  overflow-wrap: anywhere;
}

@container editor (max-width: 320px) {
  .ed-toolbar :deep(.n-button__content) {
    display: none;
  }
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
