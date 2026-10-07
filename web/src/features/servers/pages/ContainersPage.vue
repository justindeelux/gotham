<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NSpace,
  NDrawer,
  NDrawerContent,
  NText,
} from "naive-ui";
import { computed } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";

import ContainersTable from "@/features/servers/components/ContainersTable.vue";
import ContainersToolbar from "@/features/servers/components/ContainersToolbar.vue";
import LogViewer from "@/features/servers/components/LogViewer.vue";
import { useContainersPage } from "@/features/servers/composables/useContainersPage";

const route = useRoute();
const serverId = computed<string>(() => String(route.params.id ?? ""));
const { t } = useI18n();

const {
  containers,
  loading,
  error,
  serverName,
  loaded,
  activeFilter,
  searchQuery,
  filteredContainers,
  filterCounts,
  drawerWidth,
  selected,
  drawerOpen,
  isPending,
  isBusy,
  openLogs,
  fetchContainers,
  runAction,
} = useContainersPage(serverId);

/** emptyDescription renders the table empty state for each data condition. */
const emptyDescription = computed<string>(() => {
  if (!loaded.value) {
    return t("servers.containers.emptyLoading");
  }
  if (containers.value.length === 0) {
    return t("servers.containers.emptyNone");
  }
  return t("servers.containers.emptyFiltered");
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" :aria-label="$t('servers.detail.breadcrumbNav')">
      <RouterLink to="/servers">{{ $t("servers.detail.serversBreadcrumb") }}</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted" aria-current="page">{{ serverName || serverId }}</span>
    </nav>

    <NCard>
      <template #header>
        <NSpace align="center" justify="space-between">
          <NSpace align="center" :size="10">
            <NText strong>{{ $t("servers.containers.title") }}</NText>
            <NText v-if="serverName" depth="3">{{ serverName }}</NText>
          </NSpace>
          <NButton secondary :loading="loading" @click="fetchContainers(true)">
            {{ $t("servers.containers.refresh") }}
          </NButton>
        </NSpace>
      </template>

      <NAlert
        v-if="error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ error }}
      </NAlert>

      <ContainersToolbar
        v-model:active-filter="activeFilter"
        v-model:search-query="searchQuery"
        :filter-counts="filterCounts"
      />

      <ContainersTable
        :containers="filteredContainers"
        :loading="loading"
        :empty-description="emptyDescription"
        :is-pending="isPending"
        :is-busy="isBusy"
        @action="(row, action, label) => void runAction(row, action, label)"
        @open-logs="openLogs"
      />
    </NCard>

    <NDrawer v-model:show="drawerOpen" :width="drawerWidth" placement="right">
      <NDrawerContent closable :native-scrollbar="false">
        <LogViewer
          v-if="selected"
          :server-id="serverId"
          :container-id="selected.id"
          :title="selected.name"
          :subtitle="selected.image"
          :auto-start-stream="true"
        />
      </NDrawerContent>
    </NDrawer>
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
