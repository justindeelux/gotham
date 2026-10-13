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
    <FilesBrowser class="server-files__browser" :server-id="serverId" />

    <div class="node-bar" role="group" :aria-label="t('files.browser.switchNode')">
      <ServerIcon class="node-bar__icon" :size="16" :stroke-width="1.8" aria-hidden="true" />
      <span class="small muted">{{ t("files.browser.nodeLabel") }}</span>
      <NSelect
        class="node-bar__select"
        size="small"
        :value="serverId"
        :options="nodeOptions"
        :aria-label="t('files.browser.switchNode')"
        @update:value="switchNode"
      />
    </div>

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

/* The browser owns the internal scroll; it takes every row the node bar and
   the page padding leave (JUS-118). */
.server-files__browser {
  flex: 1 1 auto;
  min-height: 0;
}

/* Bottom node switcher (JUS-119): names the agent node on screen and lets the
   user jump to another node's files. */
.node-bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.node-bar__icon {
  color: var(--muted);
}

.node-bar__select {
  /* JUS-125: 3/4 of the width it used to take (it stretched to ~full via the
     select's own width:100%), with a readable floor for node names. */
  width: 75%;
  min-width: 150px;
}

@media (max-width: 640px) {
  .node-bar {
    flex-wrap: wrap;
  }

  .node-bar__select {
    width: auto;
    min-width: 0;
    /* 3/4 of the former 160px basis. */
    flex: 1 1 120px;
  }
}
</style>
