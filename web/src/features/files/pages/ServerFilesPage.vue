<script setup lang="ts">
import { PanelLeft, Server as ServerIcon, X } from "@lucide/vue";
import { useElementSize } from "@vueuse/core";
import { NAlert, NButton, NSelect } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import { useResizeHandle } from "@/features/files/composables/useResizeHandle";
import { useFilesStore } from "@/features/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { provideFilesPane } from "@/features/files/stores/filesPane";
import { useFilesPaneSession } from "@/features/files/stores/paneSession";
import type { FilesPaneId } from "@/features/files/stores/paneSession";
import { isRootRelativePath } from "@/features/files/utils/deepLink";
import {
  filesTabsStorageKey,
  readFilesTabsSnapshot,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";
import { useAuthStore } from "@/features/auth";
import { isApiError } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const serversStore = useServersStore();
const auth = useAuthStore();
const paneSession = useFilesPaneSession();
const files = useFilesStore("left");
const rightEditor = useEditorStore("right");
provideFilesPane("left");

const serverId = computed<string>(() => String(route.params.id ?? ""));

function nodeOptionsFor(id: string): Array<{
  label: string;
  value: string;
  disabled?: boolean;
}> {
  const options = serversStore.servers.map((item) => ({
    label:
      item.status === "ready"
        ? item.name
        : `${item.name} · ${String(t(`servers.status.${item.status}`))}`,
    value: item.id,
    disabled: item.status !== "ready",
  }));
  if (!options.some((option) => option.value === id)) {
    options.unshift({
      label:
        serversStore.servers.find((item) => item.id === id)?.name ?? id,
      value: id,
      disabled: false,
    });
  }
  return options;
}

const nodeOptions = computed(() => nodeOptionsFor(serverId.value));

const splitEnabled = computed<boolean>(() => paneSession.splitEnabled);
const rightServerId = computed<string>(
  () => paneSession.secondaryServerId || serverId.value,
);
const rightNodeOptions = computed(() => nodeOptionsFor(rightServerId.value));

const appliedDeepLink = ref("");
const deepLinkNotice = ref<string | null>(null);

function deepLinkKey(root: string, path: string): string {
  return `${serverId.value}|${root}|${path}`;
}

const currentRoots = computed(() =>
  files.rootsServerId === serverId.value ? files.roots : [],
);

const fallbackRoot = computed<string>(
  () => files.activeTab?.rootName || currentRoots.value[0]?.name || "",
);

async function applyDeepLink(): Promise<void> {
  const rawRoot = typeof route.query.root === "string" ? route.query.root : "";
  const rawPath = typeof route.query.path === "string" ? route.query.path : "";
  if (rawRoot === "") {
    deepLinkNotice.value = null;
    return;
  }
  const roots = currentRoots.value;
  if (roots.length === 0) {
    return;
  }
  if (!roots.some((root) => root.name === rawRoot)) {
    deepLinkNotice.value = t("files.deepLink.rootUnavailable", {
      root: rawRoot,
      path: rawPath,
      fallback: fallbackRoot.value,
    });
    return;
  }
  if (!isRootRelativePath(rawPath)) {
    deepLinkNotice.value = t("files.deepLink.invalidPath", {
      root: rawRoot,
      path: rawPath,
      fallback: fallbackRoot.value,
    });
    return;
  }
  deepLinkNotice.value = null;
  const key = deepLinkKey(rawRoot, rawPath);
  if (appliedDeepLink.value === key) {
    return;
  }
  appliedDeepLink.value = key;
  await files.openAt(rawRoot, rawPath);
  if (isApiError(files.errorRaw) && files.errorRaw.status === 404) {
    deepLinkNotice.value = t("files.deepLink.notFound", {
      root: rawRoot,
      path: rawPath,
    });
  }
}

watch(
  [
    () => files.rootsServerId,
    () => files.roots.map((root) => root.name).join(","),
    () => route.query.root,
    () => route.query.path,
    serverId,
  ],
  () => void applyDeepLink(),
  { immediate: true },
);

watch(serverId, () => {
  appliedDeepLink.value = "";
  deepLinkNotice.value = null;
});

watch(
  [() => files.activeTab.rootName, () => files.activeTab.path],
  () => {
    const { rootName: root, path } = files.activeTab;
    if (root === "") {
      return;
    }
    const rawRoot = typeof route.query.root === "string" ? route.query.root : "";
    const rawPath = typeof route.query.path === "string" ? route.query.path : "";
    if (rawRoot !== "" && appliedDeepLink.value !== deepLinkKey(rawRoot, rawPath)) {
      return;
    }
    if (rawRoot === root && rawPath === path) {
      return;
    }
    void router.replace({ query: { ...route.query, root, path } });
  },
);

onMounted(() => {
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => undefined);
  }
});

function switchNode(next: string | number | null): void {
  const id = String(next ?? "");
  if (id === "" || id === serverId.value) {
    return;
  }
  void router.push({ name: "server-files", params: { id } });
}

async function switchRightNode(next: string | number | null): Promise<void> {
  const id = String(next ?? "");
  if (id === "" || id === rightServerId.value) {
    return;
  }
  if (rightEditor.hasDirty) {
    const outcome = await rightEditor.confirmDiscard();
    if (outcome !== "leave") {
      return;
    }
    rightEditor.reset();
  }
  paneSession.setSecondaryServerId(id);
}

const panesEl = ref<HTMLElement | null>(null);
const panesSize = useElementSize(panesEl);
const NARROW_WORKSPACE_PX = 900;
const isNarrow = computed<boolean>(
  () => panesSize.width.value > 0 && panesSize.width.value < NARROW_WORKSPACE_PX,
);
const leftPercent = ref(50);
const panesStyle = computed<Record<string, string>>(() => ({
  gridTemplateColumns:
    splitEnabled.value && !isNarrow.value
      ? `${String(leftPercent.value)}fr 12px ${String(100 - leftPercent.value)}fr`
      : "",
}));
const splitter = useResizeHandle({
  width: () =>
    (panesSize.width.value * leftPercent.value) / 100,
  min: 280,
  max: Number.MAX_SAFE_INTEGER,
  onChange: (width) => {
    if (panesSize.width.value <= 0) {
      return;
    }
    const percent = (width / panesSize.width.value) * 100;
    leftPercent.value = Math.min(75, Math.max(25, Math.round(percent)));
  },
});

function toggleSplit(): void {
  if (splitEnabled.value) {
    void closeSecondary();
    return;
  }
  seedSecondary();
  leftPercent.value = 50;
  paneSession.enableSplit(serverId.value);
}

function seedSecondary(): void {
  if (paneSession.secondaryServerId !== "") {
    return;
  }
  const store = (() => {
    try {
      return window.localStorage;
    } catch {
      return null;
    }
  })();
  if (store === null) {
    return;
  }
  const snapshot = files.collectTabsSnapshot();
  if (snapshot === null) {
    return;
  }
  const key = filesTabsStorageKey(serverId.value, auth.user?.id ?? "", "right");
  if (key === "" || readFilesTabsSnapshot(store, key) !== null) {
    return;
  }
  writeFilesTabsSnapshot(store, key, snapshot);
}

async function closeSecondary(): Promise<void> {
  if (rightEditor.hasDirty) {
    const outcome = await rightEditor.confirmDiscard();
    if (outcome !== "leave") {
      return;
    }
  }
  rightEditor.reset();
  paneSession.disableSplit();
}

function showPane(pane: FilesPaneId): boolean {
  return !splitEnabled.value || !isNarrow.value || paneSession.activePane === pane;
}

function switchPane(pane: FilesPaneId): void {
  paneSession.activate(pane);
}

const PANES: readonly FilesPaneId[] = ["left", "right"];
</script>

<template>
  <div class="server-files">
    <NAlert
      v-if="deepLinkNotice"
      class="server-files__deep-link"
      type="warning"
      :show-icon="true"
    >
      {{ deepLinkNotice }}
    </NAlert>

    <div v-if="splitEnabled && isNarrow" class="server-files__switch" role="group" :aria-label="t('files.split.panes')">
      <NButton
        v-for="pane in PANES"
        :key="pane"
        size="tiny"
        :secondary="paneSession.activePane !== pane"
        :aria-pressed="paneSession.activePane === pane"
        :aria-label="t(pane === 'left' ? 'files.split.showLeft' : 'files.split.showRight')"
        @click="switchPane(pane)"
      >
        {{ t(pane === "left" ? "files.split.leftPane" : "files.split.rightPane") }}
      </NButton>
    </div>

    <div
      ref="panesEl"
      class="server-files__panes"
      :class="{ 'is-split': splitEnabled, 'is-narrow': splitEnabled && isNarrow }"
      :style="panesStyle"
    >
      <section
        v-show="showPane('left')"
        class="server-files__pane"
        :aria-label="t('files.split.leftPane')"
      >
        <FilesBrowser class="server-files__browser" pane="left" :server-id="serverId">
          <template #header-tools>
            <span class="fm-node" role="group" :aria-label="t('files.browser.switchNode')">
              <ServerIcon class="fm-node__icon" :size="15" :stroke-width="1.8" aria-hidden="true" />
              <NSelect
                class="fm-node__select"
                size="small"
                :value="serverId"
                :options="nodeOptions"
                :aria-label="t('files.browser.switchNode')"
                @update:value="switchNode"
              />
            </span>
            <NButton
              size="small"
              class="server-files__split"
              :aria-pressed="splitEnabled"
              :aria-label="splitEnabled ? t('files.split.toggleOff') : t('files.split.toggleOn')"
              @click="toggleSplit"
            >
              <template #icon>
                <PanelLeft :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
              <span class="fm-btn-label">{{ splitEnabled ? t("files.split.toggleOff") : t("files.split.toggleOn") }}</span>
            </NButton>
          </template>
        </FilesBrowser>
      </section>

      <div
        v-if="splitEnabled && !isNarrow"
        class="server-files__divider"
        role="separator"
        aria-orientation="vertical"
        :aria-label="t('files.split.divider')"
        :aria-valuemin="25"
        :aria-valuemax="75"
        :aria-valuenow="leftPercent"
        tabindex="0"
        @pointerdown="splitter.onPointerDown"
        @pointermove="splitter.onPointerMove"
        @pointerup="splitter.onPointerUp"
        @pointercancel="splitter.onPointerUp"
        @keydown="splitter.onKeydown"
      ></div>

      <section
        v-if="splitEnabled"
        v-show="showPane('right')"
        class="server-files__pane"
        :aria-label="t('files.split.rightPane')"
      >
        <FilesBrowser class="server-files__browser" pane="right" :server-id="rightServerId">
          <template #header-tools>
            <span class="fm-node" role="group" :aria-label="t('files.split.switchRightNode')">
              <ServerIcon class="fm-node__icon" :size="15" :stroke-width="1.8" aria-hidden="true" />
              <NSelect
                class="fm-node__select"
                size="small"
                :value="rightServerId"
                :options="rightNodeOptions"
                :aria-label="t('files.split.switchRightNode')"
                @update:value="switchRightNode"
              />
            </span>
            <NButton
              size="small"
              class="server-files__close-pane"
              :aria-label="t('files.split.closeRight')"
              @click="closeSecondary"
            >
              <template #icon>
                <X :size="14" :stroke-width="1.8" aria-hidden="true" />
              </template>
            </NButton>
          </template>
        </FilesBrowser>
      </section>
    </div>
  </div>
</template>

<style scoped>
.server-files {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  gap: var(--space-3);
  min-height: 0;
}

.server-files__switch {
  display: flex;
  gap: var(--space-2);
  flex: 0 0 auto;
}

.server-files__panes {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  flex: 1 1 auto;
  gap: 0;
  min-height: 0;
  min-width: 0;
}

.server-files__panes.is-narrow {
  grid-template-columns: minmax(0, 1fr);
}

.server-files__pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}

.server-files__pane + .server-files__pane {
  padding-left: 0;
}

.server-files__browser {
  flex: 1 1 auto;
  min-height: 0;
}

.server-files__divider {
  align-self: stretch;
  cursor: col-resize;
  border-left: 1px solid var(--border);
  border-right: 1px solid var(--border);
  background: var(--surface-warm);
  border-radius: var(--radius-sm);
}

.server-files__divider:hover,
.server-files__divider:focus-visible {
  background: var(--accent);
}

.server-files__deep-link {
  flex: 0 0 auto;
}

.fm-node {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.fm-node__icon {
  color: var(--muted);
  flex: 0 0 auto;
}

.fm-node__select {
  width: 180px;
  min-width: 140px;
}

@media (max-width: 640px) {
  .fm-node__select {
    width: auto;
    min-width: 96px;
    flex: 0 1 120px;
  }

  .server-files__split .fm-btn-label {
    display: none;
  }
}
</style>
