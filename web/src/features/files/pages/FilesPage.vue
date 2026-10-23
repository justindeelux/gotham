<script setup lang="ts">
import { NButton, NCard, NEmpty, NSpace } from "naive-ui";
import { computed, onMounted, watch } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { useServersStore } from "@/features/servers";

const { t } = useI18n();
const router = useRouter();
const serversStore = useServersStore();

const servers = computed(() => serversStore.servers);

watch(
  servers,
  (list) => {
    if (list.length === 1) {
      void router.replace({
        name: "server-files",
        params: { id: list[0].id },
      });
    }
  },
  { immediate: true },
);

onMounted(() => {
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => undefined);
  }
});
</script>

<template>
  <div class="files-chooser">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("files.chooser.eyebrow") }}</p>
        <h1>{{ t("files.chooser.title") }}</h1>
        <p class="page-desc">{{ t("files.chooser.desc") }}</p>
      </div>
    </div>

    <NCard>
      <div v-if="servers.length > 0" class="server-picker">
        <RouterLink
          v-for="server in servers"
          :key="server.id"
          class="server-pick"
          :to="{ name: 'server-files', params: { id: server.id } }"
        >
          <span class="server-pick__name">{{ server.name }}</span>
          <span class="server-pick__meta mono">{{ server.ip }}</span>
          <span class="server-pick__go">{{ t("files.chooser.open") }}</span>
        </RouterLink>
      </div>
      <NSpace v-else justify="center">
        <NEmpty :description="t('files.chooser.empty')">
          <template #extra>
            <p class="empty-hint">{{ t("files.chooser.emptyHint") }}</p>
            <RouterLink :to="{ name: 'servers' }" custom>
              <template #default="{ navigate }">
                <NButton type="primary" @click="navigate">
                  {{ t("files.chooser.server") }}
                </NButton>
              </template>
            </RouterLink>
          </template>
        </NEmpty>
      </NSpace>
    </NCard>
  </div>
</template>

<style scoped>
.files-chooser {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.server-picker {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: var(--space-3);
}

.server-pick {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  text-decoration: none;
  transition: border-color var(--motion-base) var(--ease-standard), background var(--motion-base) var(--ease-standard);
}

.server-pick:hover {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--surface) 88%, var(--accent));
}

.server-pick__name {
  font-weight: 600;
  color: var(--fg-2);
}

.server-pick__meta {
  color: var(--muted);
  font-size: var(--text-xs);
}

.server-pick__go {
  margin-top: var(--space-2);
  color: var(--accent-ink);
  font-size: var(--text-xs);
}

.mono {
  font-family: var(--font-mono);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
}
</style>
