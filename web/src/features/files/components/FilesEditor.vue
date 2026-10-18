<script setup lang="ts">
import {
  CaseSensitive,
  Code,
  Minus,
  Save,
  SaveAll,
  Search,
  WrapText,
  X,
} from "@lucide/vue";
import type { DropdownOption } from "naive-ui";
import {
  NButton,
  NDropdown,
  NEmpty,
  NModal,
  useMessage,
} from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { CSSProperties } from "vue";
import { useI18n } from "vue-i18n";
import { onBeforeRouteLeave, onBeforeRouteUpdate } from "vue-router";

import EditorCloseTabsDialog from "@/features/files/components/EditorCloseTabsDialog.vue";
import EditorConflictDialog from "@/features/files/components/EditorConflictDialog.vue";
import EditorLanguagePicker from "@/features/files/components/EditorLanguagePicker.vue";
import EditorSaveAsDialog from "@/features/files/components/EditorSaveAsDialog.vue";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import { useFileDownload } from "@/features/files/composables/useFileDownload";
import { createEditorSession } from "@/features/files/composables/useCodeMirror";
import type { EditorSession } from "@/features/files/composables/useCodeMirror";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import {
  clampEditorOffset,
  scrollTabIntoView,
  tabsToClose,
} from "@/features/files/utils/editor";
import type { EditorLanguageId, TabCloseScope } from "@/features/files/utils/editor";
import { useTeamsStore } from "@/features/teams";
import { useCopyText } from "@/shared/composables/useCopyText";
import { formatBytes } from "@/shared/utils/format";

interface Props {
  serverId: string;
}

const props = defineProps<Props>();

const { t } = useI18n();
const message = useMessage();
const editor = useEditorStore();
const files = useFilesStore();
const teams = useTeamsStore();
const { download } = useFileDownload();

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
const cursor = ref<{ line: number; column: number }>({ line: 1, column: 1 });
// minimized collapses the window into the bottom dock. The window stays
// mounted (v-show) so every live session and buffer survives the toggle.
const minimized = ref(false);
// dockButton is the dock control that receives focus when the window collapses.
const dockButton = ref<HTMLButtonElement | null>(null);
// dockElement is the fixed dock band; its measured height is published as a CSS
// variable so the fill page can reserve room for it.
const dockElement = ref<HTMLElement | null>(null);

// Below this width the floating window nearly fills the viewport, so a dragged
// offset cannot be meaningful: it is reset to the pinned default.
const NARROW_VIEWPORT = 640;

// offset is the window's dragged top-left position, or null while it still sits
// at the CSS default (pinned bottom-right). It is kept while the window stays
// open and cleared on a dock restore, a narrow viewport, or a close-all.
const offset = ref<{ x: number; y: number } | null>(null);
// windowElement is the floating window, measured for drag clamping.
const windowElement = ref<HTMLElement | null>(null);
// dragState tracks one in-flight title-bar drag (null when idle).
let dragState: {
  pointerId: number;
  startX: number;
  startY: number;
  originX: number;
  originY: number;
} | null = null;
// closeAllOpen arms the "close all with unsaved files" confirmation.
const closeAllOpen = ref(false);

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

/** dockVisible is true while the collapsed dock is on screen. */
const dockVisible = computed(() => editor.tabs.length > 0 && minimized.value);

/**
 * windowStyle applies a dragged position. Until the window is dragged the CSS
 * default (pinned bottom-right) applies, so a fresh open is always as designed.
 */
const windowStyle = computed<CSSProperties>(() => {
  const position = offset.value;
  if (!position) {
    return {};
  }
  return {
    left: `${position.x}px`,
    top: `${position.y}px`,
    right: "auto",
    bottom: "auto",
  };
});

/**
 * syncDockSpace publishes the dock height as `--ed-dock-h` (consumed by the
 * fill page's bottom padding, AppLayout `.page--fill`) or clears it when the
 * dock is hidden. The dock is `v-show`-hidden (display:none) while the window
 * is open, so it is only measured once it is actually laid out.
 */
async function syncDockSpace(): Promise<void> {
  await nextTick();
  const element = dockElement.value;
  if (dockVisible.value && element) {
    document.documentElement.style.setProperty("--ed-dock-h", `${element.offsetHeight}px`);
    return;
  }
  document.documentElement.style.removeProperty("--ed-dock-h");
}

watch(dockVisible, () => void syncDockSpace(), { immediate: true });
onBeforeUnmount(() => {
  document.documentElement.style.removeProperty("--ed-dock-h");
});

const readyTabs = computed(() =>
  editor.tabs.filter((tab) => tab.status === "ready"),
);

/** activeTab is the store's focused tab (convenience for the template). */
const activeTab = computed(() => editor.activeTab);

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

/**
 * dockLabel is the dock control's accessible name. The unsaved state is folded
 * in because an `aria-label` overrides any child text a screen reader would
 * otherwise read.
 */
const dockLabel = computed<string>(() => {
  const name = activeTab.value?.name ?? String(t("files.editor.heading"));
  return editor.hasDirty
    ? String(t("files.editor.restoreUnsaved", { name }))
    : String(t("files.editor.restore", { name }));
});

// A dirty buffer must never be dropped by a route change, a server switch or
// a team switch without an explicit confirmation (beforeunload still covers a
// browser close/reload).
const leaveOpen = ref(false);
let leaveResolve: ((_leave: boolean) => void) | null = null;
// leaveResolved records that the user explicitly allowed a discard (clicked
// "Discard and leave"). An unmount without it keeps dirty buffers in the store.
let leaveResolved = false;

/** confirmDiscard asks the user before discarding dirty buffers. */
function confirmDiscard(): Promise<boolean> {
  if (!editor.hasDirty) {
    return Promise.resolve(true);
  }
  return new Promise<boolean>((resolve) => {
    leaveResolve = resolve;
    leaveOpen.value = true;
  });
}

/** resolveLeave settles the pending confirmation. */
function resolveLeave(leave: boolean): void {
  leaveOpen.value = false;
  if (leave) {
    leaveResolved = true;
  }
  leaveResolve?.(leave);
  leaveResolve = null;
}

onBeforeRouteLeave(async () => confirmDiscard());
onBeforeRouteUpdate(async () => confirmDiscard());

watch(
  () => props.serverId,
  () => {
    // A route update (server switch) resets the tabs; a discard resolved for
    // that navigation must not linger and auto-approve a later implicit
    // unmount.
    leaveResolved = false;
    editor.reset();
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
    if (!(await confirmDiscard())) {
      teamRevertTarget = prev;
      teams.selectTeam(prev);
      return;
    }
    editor.reset();
  },
);

// A tab switch, or a tab finishing its load, creates the CodeMirror instance
// the first time a ready tab is shown.
watch(
  () => [editor.activeId, editor.activeTab?.status] as const,
  async () => {
    await nextTick();
    await ensureSession();
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

// Wrap is a global preference applied to every open instance.
watch(
  () => editor.wrap,
  (wrap) => {
    for (const session of sessions.values()) {
      session.setWrap(wrap);
    }
  },
);

// A role or writable-root change flips the write gate; a live session must
// follow it, or a demoted caller could still type into an already-open buffer.
watch(
  () => files.canWrite,
  (canWrite) => {
    for (const session of sessions.values()) {
      session.setReadOnly(!canWrite);
    }
  },
);

// Opening or focusing any file from the browser while the window is collapsed
// reopens it, so a click always lands somewhere visible. openFile() focuses the
// tab it opens — new or already open — which is exactly an activeId change.
watch(
  () => editor.activeId,
  () => {
    if (minimized.value) {
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

/** isTabFocused reports whether DOM focus is on a tablist button. */
function isTabFocused(): boolean {
  const active = document.activeElement;
  return active instanceof HTMLElement && active.getAttribute("role") === "tab";
}

/** ensureSession creates the active tab's editor instance on first view. */
async function ensureSession(): Promise<void> {
  const tab = editor.activeTab;
  if (!tab || tab.status !== "ready") {
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
    readOnly: !files.canWrite,
    wrap: editor.wrap,
    onChange: (doc) => editor.setDoc(tabId, doc),
    onCursor: (line, column) => {
      if (editor.activeId === tabId) {
        cursor.value = { line, column };
      }
    },
    onSave: () => void saveTab(tabId),
    onSaveAll: () => void saveAllTabs(),
  }).then((session) => {
    // A rapid tab switch while the chunk loaded: keep the instance only if the
    // tab is still open.
    if (editor.tabs.some((item) => item.id === tabId)) {
      sessions.set(tabId, session);
      sessionHosts.set(tabId, host);
      sessionLanguages.set(tabId, tab.language);
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
  } else if (outcome === "error") {
    message.error(tab.error || String(t("files.editor.saveFailed", { name: tab.name })));
  }
  // "conflict" arms the modal; "blocked" is a no-op (loading/read-only tab).
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
      continue;
    }
    if (report.outcome === "conflict") {
      message.warning(String(t("files.editor.conflictTitle")));
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
  await download(props.serverId, teams.activeTeamId, tab.root, directoryOf(tab.path), {
    name: tab.name,
  });
}

/** directoryOf returns the parent path of a root-relative path. */
function directoryOf(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}

/** openSearch opens the find/replace panel on the active instance. */
function openSearch(): void {
  sessions.get(editor.activeId)?.openSearch();
}

/** openGoToLine opens the go-to-line prompt on the active instance. */
function openGoToLine(): void {
  sessions.get(editor.activeId)?.goToLine();
}

/** toggleWrap flips the wrap preference. */
function toggleWrap(): void {
  editor.setWrap(!editor.wrap);
}

/** pickLanguage overrides the active tab's grammar without touching history. */
function pickLanguage(language: EditorLanguageId): void {
  const tab = activeTab.value;
  if (tab) {
    editor.setLanguage(tab.id, language);
  }
}

/** resetWindowPosition returns the window to its pinned default position. */
function resetWindowPosition(): void {
  offset.value = null;
  dragState = null;
}

/** onWindowResize re-clamps a dragged window; a narrow viewport resets it. */
function onWindowResize(): void {
  if (window.innerWidth < NARROW_VIEWPORT) {
    resetWindowPosition();
    return;
  }
  const element = windowElement.value;
  const position = offset.value;
  if (!element || !position) {
    return;
  }
  const rect = element.getBoundingClientRect();
  offset.value = clampEditorOffset(
    position.x,
    position.y,
    rect.width,
    rect.height,
    window.innerWidth,
    window.innerHeight,
  );
}

/**
 * onBarPointerDown starts a title-bar drag. The press must land on the bar
 * itself: a press on a control (minimize/close) is that control's click, never
 * a drag. Pointer capture keeps the drag tracking even outside the bar. The
 * position is only pinned on the first move, so a bare click never re-anchors
 * the window away from its bottom-right default.
 */
function onBarPointerDown(event: PointerEvent): void {
  if (event.button !== 0) {
    return;
  }
  const bar = event.currentTarget;
  const element = windowElement.value;
  if (!(bar instanceof HTMLElement) || !element) {
    return;
  }
  // A press on a control (minimize/close) is a click, never a drag. The
  // control's icon is an SVG element, so the test must accept any Element.
  if (event.target instanceof Element && event.target.closest("button")) {
    return;
  }
  const rect = element.getBoundingClientRect();
  dragState = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    originX: rect.left,
    originY: rect.top,
  };
  bar.setPointerCapture(event.pointerId);
}

/** onBarPointerMove drags the window by the pointer delta, clamped on screen. */
function onBarPointerMove(event: PointerEvent): void {
  if (!dragState || event.pointerId !== dragState.pointerId) {
    return;
  }
  const element = windowElement.value;
  if (!element) {
    return;
  }
  const rect = element.getBoundingClientRect();
  offset.value = clampEditorOffset(
    dragState.originX + (event.clientX - dragState.startX),
    dragState.originY + (event.clientY - dragState.startY),
    rect.width,
    rect.height,
    window.innerWidth,
    window.innerHeight,
  );
}

/** onBarPointerUp ends the drag (also on pointercancel). */
function onBarPointerUp(event: PointerEvent): void {
  if (dragState && event.pointerId === dragState.pointerId) {
    dragState = null;
  }
}

/** minimizeWindow collapses the window into the dock and moves focus there. */
function minimizeWindow(): void {
  minimized.value = true;
  void nextTick(() => dockButton.value?.focus());
}

/**
 * onWindowEsc minimizes the window on Escape. A child that already handled the
 * key (CodeMirror's search panel) calls preventDefault, so it is left alone.
 * The leave/conflict/save-as dialogs are teleported to body, so their Escape
 * never bubbles through the window and their own handling is unaffected.
 */
function onWindowEsc(event: KeyboardEvent): void {
  if (!event.defaultPrevented) {
    minimizeWindow();
  }
}

/** restoreWindow reopens the window and returns focus to the active tab. */
function restoreWindow(): void {
  minimized.value = false;
  // A dock restore puts the window back at its pinned default, so it never
  // reappears stranded at a stale dragged position.
  resetWindowPosition();
  void nextTick(() => {
    const tab = document.getElementById(`ed-tab-${editor.activeId}`);
    if (tab) {
      tab.focus();
    } else {
      sessions.get(editor.activeId)?.focus();
    }
    // The window was display:none while collapsed; re-measure the live host.
    sessions.get(editor.activeId)?.requestMeasure();
    revealActiveTab();
  });
}

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
  closeSaving.value = true;
  try {
    const reports = await editor.saveAll(teams.activeTeamId, ids);
    const failed = new Set<string>();
    for (const report of reports) {
      if (report.outcome === "saved") {
        continue;
      }
      failed.add(report.tab.id);
      if (report.outcome === "conflict") {
        message.warning(String(t("files.editor.conflictTitle")));
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
    minimized.value
  ) {
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
    return;
  }
  // Ctrl+Tab is reserved by some browsers, but it works wherever the page can
  // claim it (Firefox, kiosk/desktop shells).
  if (event.ctrlKey && !event.metaKey && event.key === "Tab") {
    event.preventDefault();
    cycleTab(event.shiftKey ? -1 : 1);
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
  // An implicit unmount (a team switch, or losing the read role) must never
  // drop a dirty buffer: reset only when nothing is dirty, or the user
  // resolved the discard prompt. The store outlives the component, so a
  // remount restores the tabs.
  if (!editor.hasDirty || leaveResolved) {
    editor.reset();
  }
});
</script>

<template>
  <section
    v-if="editor.tabs.length > 0"
    v-show="!minimized"
    ref="windowElement"
    class="ed-window files-editor"
    :style="windowStyle"
    role="region"
    :aria-label="t('files.editor.windowLabel')"
    @keydown.esc="onWindowEsc"
  >
    <header
      class="ed-window-bar"
      @pointerdown="onBarPointerDown"
      @pointermove="onBarPointerMove"
      @pointerup="onBarPointerUp"
      @pointercancel="onBarPointerUp"
      @lostpointercapture="onBarPointerUp"
    >
      <span class="ed-window-title">{{ activeTab?.name ?? t("files.editor.heading") }}</span>
      <span v-if="activeTab" class="ed-window-path mono" :title="activeTab.path">
        {{ activeTab.path }}
      </span>
      <span v-if="editor.hasDirty" class="ed-window-dirty">
        {{ t("files.editor.unsaved") }}
      </span>
      <span class="grow" />
      <NButton
        quaternary
        size="tiny"
        class="ed-window-min"
        :aria-label="t('files.editor.minimize')"
        @click="minimizeWindow"
      >
        <template #icon>
          <Minus :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NButton
        quaternary
        size="tiny"
        class="ed-window-close"
        :aria-label="t('files.editor.closeWindow')"
        @click="closeAllTabs"
      >
        <template #icon>
          <X :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </header>

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
        :class="{ 'is-active': tab.id === editor.activeId, 'is-dirty': editor.isDirty(tab) }"
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
        <button
          type="button"
          class="ed-tab-close"
          :aria-label="t('files.editor.close', { name: tab.name })"
          @click="requestCloseTabs([tab.id])"
        >
          <X :size="13" :stroke-width="2" aria-hidden="true" />
        </button>
      </div>
    </div>

    <div v-if="activeTab" class="ed-body">
      <div v-if="activeTab.status === 'ready'" class="ed-toolbar">
        <NButton
          size="tiny"
          secondary
          :type="editor.wrap ? 'primary' : 'default'"
          :aria-pressed="editor.wrap"
          @click="toggleWrap"
        >
          <template #icon>
            <WrapText :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("files.editor.wrap") }}
        </NButton>
        <NButton size="tiny" secondary @click="openSearch">
          <template #icon>
            <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("files.editor.search") }}
        </NButton>
        <NButton size="tiny" secondary @click="openGoToLine">
          <template #icon>
            <CaseSensitive :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("files.editor.goToLine") }}
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
          :disabled="!editor.isDirty(activeTab)"
          @click="() => void saveTab(activeTab!.id)"
        >
          <template #icon>
            <Save :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ activeTab.saving ? t("files.editor.saving") : t("files.editor.save") }}
        </NButton>
      </div>

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

      <div
        v-if="activeTab.status === 'loading'"
        class="ed-state"
        role="tabpanel"
        :id="'ed-panel-' + activeTab.id"
        :aria-labelledby="'ed-tab-' + activeTab.id"
      >
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

      <div class="ed-foot">
        <span class="grow">
          {{ editor.isDirty(activeTab) ? t("files.editor.unsaved") : files.canWrite ? "" : t("files.editor.readOnly") }}
        </span>
        <span v-if="activeTab.status === 'ready'" class="mono">
          {{ t("files.editor.lineColumn", { line: cursor.line, column: cursor.column }) }}
        </span>
        <span v-if="activeTab.status === 'ready'" class="mono">{{ t("files.editor.encoding") }}</span>
        <span v-if="activeTab.status === 'ready'" class="mono">{{ activeTab.eol === "\r\n" ? "CRLF" : "LF" }}</span>
        <EditorLanguagePicker
          v-if="activeTab.status === 'ready'"
          :language="activeTab.language"
          @select="pickLanguage"
        />
        <span class="mono">{{ formatBytes(activeTab.size) }}</span>
      </div>
    </div>
  </section>

  <div
    v-if="editor.tabs.length > 0"
    v-show="minimized"
    ref="dockElement"
    class="ed-dock"
    role="toolbar"
    :aria-label="t('files.editor.dockLabel')"
  >
    <button
      ref="dockButton"
      type="button"
      class="ed-dock-item"
      :aria-label="dockLabel"
      @click="restoreWindow"
    >
      <Code :size="14" :stroke-width="1.8" aria-hidden="true" />
      <span class="ed-dock-name">{{ activeTab?.name ?? t("files.editor.heading") }}</span>
      <span v-if="editor.hasDirty" class="ed-dock-dirty" aria-hidden="true" />
    </button>
  </div>

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
    :show="leaveOpen"
    preset="card"
    style="width: 440px; max-width: 94vw"
    :title="t('files.editor.leaveTitle')"
    :mask-closable="false"
    :close-on-esc="false"
    @update:show="(value: boolean) => { if (!value) resolveLeave(false); }"
  >
    <p class="small">{{ t("files.editor.leaveMessage") }}</p>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="resolveLeave(false)">{{ t("files.editor.leaveStay") }}</NButton>
        <NButton type="error" @click="resolveLeave(true)">
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

  <EditorCloseTabsDialog
    :show="pendingClose !== null"
    :paths="pendingClosePaths"
    :can-save="files.canWrite"
    :saving="closeSaving"
    @save="saveAndClose"
    @discard="discardAndClose"
    @cancel="cancelClose"
  />
  <EditorConflictDialog />
  <EditorSaveAsDialog />
</template>

<style scoped>
.ed-window {
  position: fixed;
  right: var(--space-6);
  bottom: var(--space-4);
  display: flex;
  flex-direction: column;
  width: min(1024px, calc(100vw - var(--space-6) * 2));
  height: min(720px, calc(100vh - 104px));
  height: min(720px, calc(100dvh - 104px));
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--elev-raised);
  overflow: hidden;
  z-index: 70;
}

.ed-window-bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2) var(--space-1) var(--space-4);
  border-bottom: 1px solid var(--border);
  background: var(--surface-warm);
  /* The bar is the drag handle: a grab cursor, no text selection, and no
     touch scroll fighting the drag. */
  cursor: grab;
  user-select: none;
  touch-action: none;
}

.ed-window-bar:active {
  cursor: grabbing;
}

.ed-window-title {
  font-weight: 600;
  color: var(--fg-2);
  white-space: nowrap;
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

/* Bottom app-dock: one item while the editor window is collapsed; a click
   restores it. Fixed so it survives any page scroll, but constrained to the
   main content column so it never covers the ServerRail foot or the sidebar
   MeCard (both sticky at the same bottom edge). */
.ed-dock {
  position: fixed;
  left: calc(var(--rail-w) + var(--sidebar-w));
  right: 0;
  bottom: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  background: var(--surface-warm);
  border-top: 1px solid var(--border);
}

/* At <=1024px the sidebar column leaves the flow and the rail becomes a
   drawer (AppLayout grid), so the content column starts at x=0 and the dock
   must too (JUS-135). */
@media (max-width: 1024px) {
  .ed-dock {
    left: 0;
  }
}

.ed-dock-item {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  max-width: 320px;
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--fg);
  font: inherit;
  cursor: pointer;
}

.ed-dock-item:hover {
  border-color: var(--accent);
}

.ed-dock-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ed-dock-dirty {
  flex: 0 0 auto;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--warn);
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

.ed-state {
  display: grid;
  place-items: center;
  flex: 1 1 auto;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-4);
}

.ed-foot {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding-top: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
}

.ed-window-bar .grow,
.ed-toolbar .grow,
.ed-foot .grow {
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
