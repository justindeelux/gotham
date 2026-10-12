<script setup lang="ts">
import { NSpace } from "naive-ui";
import { computed, ref, watch } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";

import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import type { Server } from "@/features/servers/api/servers";
import { getServer } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers";

const route = useRoute();
const { t } = useI18n();
const serversStore = useServersStore();

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
    // The browser surfaces its own errors; the crumb just shows the id.
    server.value = null;
  }
}

watch(serverId, () => void loadServer(), { immediate: true });
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" :aria-label="t('servers.detail.breadcrumbNav')">
      <RouterLink to="/servers">{{ t("servers.detail.serversBreadcrumb") }}</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <RouterLink :to="{ name: 'server-detail', params: { id: serverId } }">
        {{ serverName }}
      </RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted" aria-current="page">{{ t("files.title") }}</span>
    </nav>

    <FilesBrowser :server-id="serverId" />
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}

.muted {
  color: var(--muted);
}
</style>
