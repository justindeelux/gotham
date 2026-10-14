<script setup lang="ts">
import { Server as ServerIcon } from "@lucide/vue";
import { NSelect } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import FilesEditor from "@/features/files/components/FilesEditor.vue";
import { useFilesStore } from "@/features/files/stores/files";
import type { Server } from "@/features/servers/api/servers";
import { getServer } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers";

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
    <FilesBrowser class="server-files__browser" :server-id="serverId">
      <!-- JUS-134: the agent-node selector decides the whole listing, so it
           sits next to the Root selector in the browser header (passed through
           the header-tools slot) instead of a bar at the page bottom. -->
      <template #header-tools>
        <span class="fm-node" role="group" :aria-label="t('files.browser.switchNode')">
          <ServerIcon class="fm-node__icon" :size="15" :stroke-width="1.8" aria-hidden="true" />
          <span class="small muted">{{ t("files.browser.nodeLabel") }}</span>
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

    <FilesEditor v-if="files.canReadContent" :server-id="serverId" />
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

/* Agent-node switcher (JUS-119/134): names the node on screen and lets the user
   jump to another node's files. It rides in the browser card header. */
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
    flex: 0 1 120px;
  }
}
</style>
