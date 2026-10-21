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
import { isRootRelativePath } from "@/features/files/utils/deepLink";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const serversStore = useServersStore();
const files = useFilesStore();

const serverId = computed<string>(() => String(route.params.id ?? ""));
const server = ref<Server | null>(null);

/**
 * serverName prefers a detail row for the *current* server id, then the cached
 * list row, then the raw id. It is keyed on serverId so a route-param change
 * can never reuse the previous server's name.
 */
const serverName = computed<string>(() => {
  if (server.value && server.value.id === serverId.value) {
    return server.value.name;
  }
  const cached = serversStore.servers.find((item) => item.id === serverId.value);
  return cached?.name ?? serverId.value;
});

/**
 * nodeOptions feeds the bottom node switcher (JUS-119) with every managed
 * server. The current node is inserted first when the list has not loaded yet,
 * so the switcher always names the node on screen. A node whose agent is not
 * ready is listed with its status but not selectable — switching to it could
 * only show the browser's error state.
 */
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

/** loadServer resolves the current server's name as the route param changes. */
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
    // The browser surfaces its own errors; the node bar just shows the id.
    server.value = null;
  }
}

watch(serverId, () => void loadServer(), { immediate: true });

/**
 * appliedDeepLink pins the target the browser already opened: the watcher reruns
 * when the roots or the query change, and re-opening the same directory would
 * reload it for nothing.
 */
const appliedDeepLink = ref("");
/** deepLinkNotice explains a rejected link and where the browser landed. */
const deepLinkNotice = ref<string | null>(null);

/**
 * currentRoots is empty until the CURRENT server's root list has loaded: a
 * leftover list from the previous node must never validate a deep link.
 */
const currentRoots = computed(() =>
  files.rootsServerId === serverId.value ? files.roots : [],
);

/**
 * fallbackRoot names the root the browser actually shows when a link is
 * rejected: the active tab (which may be restored from persistence), then the
 * node's first root.
 */
const fallbackRoot = computed<string>(
  () => files.activeTab?.rootName || currentRoots.value[0]?.name || "",
);

/**
 * applyDeepLink opens a `?root=&path=` target once the node's roots are
 * loaded. The notice distinguishes why a link was not opened: a root the node
 * does not expose, a malformed/escaping path, or a directory the node reports
 * as missing. The agent still enforces its own confinement and the File
 * Manager its authorization.
 */
async function applyDeepLink(): Promise<void> {
  const rawRoot = typeof route.query.root === "string" ? route.query.root : "";
  const rawPath = typeof route.query.path === "string" ? route.query.path : "";
  if (rawRoot === "") {
    deepLinkNotice.value = null;
    return;
  }
  const roots = currentRoots.value;
  if (roots.length === 0) {
    // Roots are still loading (or belong to another server): wait for the
    // watch below instead of rejecting a link that may be valid.
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
  const key = `${serverId.value}|${rawRoot}|${rawPath}`;
  if (appliedDeepLink.value === key) {
    return;
  }
  appliedDeepLink.value = key;
  await files.openAt(rawRoot, rawPath);
  // The target was well-formed and root-valid, but the node may not have the
  // directory: the browser keeps its own not-found state; the notice names the
  // link so the reason is clear.
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

// A node switch replaces the root list; a notice for the previous node's link
// must not linger over the new browser.
watch(serverId, () => {
  appliedDeepLink.value = "";
  deepLinkNotice.value = null;
});

onMounted(() => {
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => undefined);
  }
});

/**
 * switchNode navigates to another agent node's files. The files store resets on
 * the server-id change, so the browser reloads that node's default root — the
 * path never carries over from the previous node.
 */
function switchNode(next: string | number | null): void {
  const id = String(next ?? "");
  if (id === "" || id === serverId.value) {
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
      <!-- JUS-134/JUS-206 round 4: the agent-node selector decides the whole
           listing, so it rides at the right end of the tab row (passed through
           the header-tools slot). No visible label — the group keeps the
           accessible name through aria-label. -->
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

/* The browser owns the internal scroll; it takes every row the editor and the
   page padding leave (JUS-118). */
.server-files__browser {
  flex: 1 1 auto;
  min-height: 0;
}

.server-files__deep-link {
  flex: 0 0 auto;
}

/* Agent-node switcher (JUS-119/134, JUS-206 round 4): names the node on
   screen and lets the user jump to another node's files. It rides at the
   right end of the tab row (JUS-159 context bar). */
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
    /* Floor the shrink (the JUS-159 context bar is tighter than the old card
       header): the 120px basis needs the min reset to shrink at all, but never
       below a usable target. */
    min-width: 96px;
    flex: 0 1 120px;
  }

}
</style>
