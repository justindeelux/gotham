<script setup lang="ts">
import {
  CaseSensitive,
  Save,
  Search,
  WrapText,
  X,
} from "@lucide/vue";
import {
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  useMessage,
} from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import EditorConflictDialog from "@/features/files/components/EditorConflictDialog.vue";
import EditorSaveAsDialog from "@/features/files/components/EditorSaveAsDialog.vue";
import { useFileDownload } from "@/features/files/composables/useFileDownload";
import { createEditorSession } from "@/features/files/composables/useCodeMirror";
import type { EditorSession } from "@/features/files/composables/useCodeMirror";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { languageLabel } from "@/features/files/utils/editor";
import { useTeamsStore } from "@/features/teams";
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
const cursor = ref<{ line: number; column: number }>({ line: 1, column: 1 });

const readyTabs = computed(() =>
  editor.tabs.filter((tab) => tab.status === "ready"),
);

/** activeTab is the store's focused tab (convenience for the template). */
const activeTab = computed(() => editor.activeTab);

watch(
  () => props.serverId,
  () => editor.reset(),
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

// Drop sessions whose tab was closed, and pull external doc changes (a reload)
// back into a live instance.
watch(
  () => editor.tabs,
  () => {
    const live = new Set(editor.tabs.map((tab) => tab.id));
    for (const [id, session] of sessions) {
      if (!live.has(id)) {
        session.destroy();
        sessions.delete(id);
      }
    }
    for (const tab of editor.tabs) {
      const session = sessions.get(tab.id);
      if (session && tab.status === "ready" && session.getDoc() !== tab.doc) {
        session.setDoc(tab.doc);
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

/** setHost stores a tab's host element from its template ref. */
function setHost(id: string, el: unknown): void {
  if (el instanceof HTMLElement) {
    hosts.set(id, el);
  } else {
    hosts.delete(id);
  }
}

/** ensureSession creates the active tab's editor instance on first view. */
async function ensureSession(): Promise<void> {
  const tab = editor.activeTab;
  if (!tab || tab.status !== "ready") {
    return;
  }
  const existing = sessions.get(tab.id);
  if (existing) {
    existing.focus();
    return;
  }
  const host = hosts.get(tab.id);
  if (!host) {
    return;
  }
  const tabId = tab.id;
  const session = await createEditorSession({
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
  });
  // A rapid tab switch while the chunk loaded: keep the instance only if the
  // tab is still open.
  if (editor.tabs.some((item) => item.id === tabId)) {
    sessions.set(tabId, session);
    session.requestMeasure();
    session.focus();
  } else {
    session.destroy();
  }
}

/** saveTab saves one tab and reports the outcome. */
async function saveTab(id: string): Promise<void> {
  const tab = editor.tabs.find((item) => item.id === id);
  if (!tab || tab.saving) {
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

/** onBeforeUnload guards a reload/close while a document is dirty. */
function onBeforeUnload(event: BeforeUnloadEvent): void {
  if (!editor.hasDirty) {
    return;
  }
  event.preventDefault();
  // Older browsers need returnValue set to show the confirmation.
  event.returnValue = "";
}

onMounted(() => window.addEventListener("beforeunload", onBeforeUnload));

onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", onBeforeUnload);
  for (const session of sessions.values()) {
    session.destroy();
  }
  sessions.clear();
  editor.reset();
});
</script>

<template>
  <NCard class="files-editor">
    <template #header>
      <div class="ed-card-head">
        <h3 class="ed-card-title">{{ t("files.editor.heading") }}</h3>
        <span class="small muted">{{ t("files.editor.hint") }}</span>
      </div>
    </template>

    <template v-if="editor.tabs.length > 0">
      <div class="ed-tabs" role="tablist" :aria-label="t('files.editor.heading')">
        <div
          v-for="tab in editor.tabs"
          :key="tab.id"
          class="ed-tab"
          :class="{ 'is-active': tab.id === editor.activeId, 'is-dirty': editor.isDirty(tab) }"
          role="tab"
          :aria-selected="tab.id === editor.activeId"
        >
          <button
            type="button"
            class="ed-tab-name"
            :title="tab.path"
            @click="editor.activate(tab.id)"
          >
            <span v-if="editor.isDirty(tab)" class="ed-dot" aria-hidden="true" />
            {{ tab.name }}
          </button>
          <NPopconfirm
            v-if="editor.isDirty(tab)"
            :positive-text="t('files.editor.closeConfirm')"
            @positive-click="editor.close(tab.id)"
          >
            <template #trigger>
              <button
                type="button"
                class="ed-tab-close"
                :aria-label="t('files.editor.closeDirty', { name: tab.name })"
              >
                <X :size="13" :stroke-width="2" aria-hidden="true" />
              </button>
            </template>
            {{ t("files.editor.closeDirty", { name: tab.name }) }}
          </NPopconfirm>
          <button
            v-else
            type="button"
            class="ed-tab-close"
            :aria-label="t('files.editor.close', { name: tab.name })"
            @click="editor.close(tab.id)"
          >
            <X :size="13" :stroke-width="2" aria-hidden="true" />
          </button>
        </div>
      </div>

      <div v-if="activeTab" class="ed-body">
        <template v-if="activeTab.status === 'ready'">
          <div class="ed-toolbar">
            <NButton
              size="tiny"
              secondary
              :type="editor.wrap ? 'primary' : 'default'"
              :title="editor.wrap ? t('files.editor.unwrap') : t('files.editor.wrap')"
              :aria-pressed="editor.wrap"
              @click="toggleWrap"
            >
              <template #icon>
                <WrapText :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.wrap") }}
            </NButton>
            <NButton size="tiny" secondary :title="t('files.editor.search')" @click="openSearch">
              <template #icon>
                <Search :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.search") }}
            </NButton>
            <NButton size="tiny" secondary :title="t('files.editor.goToLine')" @click="openGoToLine">
              <template #icon>
                <CaseSensitive :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              {{ t("files.editor.goToLine") }}
            </NButton>
            <span class="grow" />
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

          <div
            v-for="tab in readyTabs"
            :key="tab.id"
            v-show="tab.id === editor.activeId"
            :ref="(el) => setHost(tab.id, el)"
            class="ed-host"
            :aria-label="tab.name"
          />
        </template>

        <div v-else-if="activeTab.status === 'loading'" class="ed-state">
          <p class="small muted">{{ t("files.editor.loading", { name: activeTab.name }) }}</p>
        </div>

        <div v-else-if="activeTab.status === 'large'" class="ed-state">
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

        <div v-else-if="activeTab.status === 'binary'" class="ed-state">
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

        <div v-else class="ed-state">
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
          <span v-if="activeTab.status === 'ready'" class="mono">{{ languageLabel(activeTab.language) }}</span>
          <span class="mono">{{ formatBytes(activeTab.size) }}</span>
        </div>
      </div>
    </template>

    <NEmpty v-else :description="t('files.editor.empty')">
      <template #extra>
        <p class="small muted">{{ t("files.editor.emptyHint") }}</p>
      </template>
    </NEmpty>

    <EditorConflictDialog />
    <EditorSaveAsDialog />
  </NCard>
</template>

<style scoped>
.ed-card-head {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.ed-card-title {
  margin: 0;
  font-size: var(--text-lg);
  color: var(--fg-2);
}

.ed-tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
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
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) 0;
}

.ed-host {
  height: 420px;
  max-height: 60vh;
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
  min-height: 220px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-4);
}

.ed-foot {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding-top: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
}

.ed-toolbar .grow,
.ed-foot .grow {
  flex: 1 1 auto;
}

.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-sm);
}
</style>
