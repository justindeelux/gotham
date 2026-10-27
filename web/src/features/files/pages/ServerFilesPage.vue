<script setup lang="ts">
import { Server as ServerIcon } from "@lucide/vue";
import { NAlert, NSelect } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import type { Server } from "@/features/servers/api/servers";
import { getServer, isApiError } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers";
import { useFilesStore } from "@/features/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { provideFilesPane } from "@/features/files/stores/filesPane";
import { isRootRelativePath } from "@/features/files/utils/deepLink";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const serversStore = useServersStore();
const files = useFilesStore();
const editor = useEditorStore("left");
provideFilesPane("left");

const serverId = computed<string>(() => String(route.params.id ?? ""));
const server = ref<Server | null>(null);

const serverName = computed<string>(() => {
  if (server.value && server.value.id === serverId.value) {
    return server.value.name;
  }
  const cached = serversStore.servers.find((item) => item.id === serverId.value);
  return cached?.name ?? serverId.value;
});

const nodeOptions = computed<
  Array<{ label: string; value: string; disabled?: boolean }>
>(() => {
  const options = serversStore.servers.map((item) => ({
    label:
      item.status === "ready"
        ? item.name
        : `${item.name} · ${String(t(`servers.status.${item.status}`))}`,
    value: item.id,
    disabled: item.status !== "ready",
  }));
  if (!options.some((option) => option.value === serverId.value)) {
    options.unshift({
      label: serverName.value,
      value: serverId.value,
      disabled: false,
    });
  }
  return options;
});

async function loadServer(): Promise<void> {
  const cached = serversStore.servers.find((item) => item.id === serverId.value);
  if (cached) {
    server.value = cached;
    return;
  }
  server.value = null;
  try {
    server.value = await getServer(serverId.value);
  } catch {
    server.value = null;
  }
}

watch(serverId, () => void loadServer(), { immediate: true });

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

async function switchNode(next: string | number | null): Promise<void> {
  const id = String(next ?? "");
  if (id === "" || id === serverId.value) {
    return;
  }
  if (editor.hasDirty && (await editor.confirmDiscard()) !== "leave") {
    return;
  }
  void router.push({ name: "server-files", params: { id } });
}
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
    <FilesBrowser class="server-files__browser" :server-id="serverId">
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
      </template>
    </FilesBrowser>
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

.server-files__browser {
  flex: 1 1 auto;
  min-height: 0;
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

}
</style>
