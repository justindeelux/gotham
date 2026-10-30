<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDrawer,
  NDrawerContent,
} from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import ContainersTable from "@/features/servers/components/ContainersTable.vue";
import ContainersToolbar from "@/features/servers/components/ContainersToolbar.vue";
import LogViewer from "@/features/servers/components/LogViewer.vue";
import ServerVolumesCard from "@/features/servers/components/ServerVolumesCard.vue";
import { useContainersPage } from "@/features/servers/composables/useContainersPage";
import { useAuthStore } from "@/features/auth";

const props = defineProps<{ serverId: string }>();
const serverId = computed<string>(() => props.serverId);
const authStore = useAuthStore();
const { t } = useI18n();

const {
  containers,
  loading,
  error,
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
  <NCard class="containers-tab" :title="$t('servers.containers.title')">
    <template #header-extra>
      <NButton size="small" secondary :loading="loading" @click="fetchContainers(true)">
        {{ $t("servers.containers.refresh") }}
      </NButton>
    </template>

    <NAlert
      v-if="error"
      type="error"
      :show-icon="true"
      class="tab-alert"
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

  <ServerVolumesCard v-if="authStore.isOwner" :server-id="serverId" />

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
</template>

<style scoped>
.containers-tab {
  margin-top: var(--space-4);
}

.tab-alert {
  margin-bottom: var(--space-3);
}
</style>
