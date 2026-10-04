<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NPagination,
  NSpin,
} from "naive-ui";

import AddServerWizard from "@/features/servers/components/AddServerWizard.vue";
import EditServerModal from "@/features/servers/components/EditServerModal.vue";
import ServerCard from "@/features/servers/components/ServerCard.vue";
import ServersToolbar from "@/features/servers/components/ServersToolbar.vue";
import GothamIcon from "@/shared/ui/GothamIcon.vue";
import {
  serversPageSize,
  useServersPage,
} from "@/features/servers/composables/useServersPage";

const {
  serversStore,
  wizardOpen,
  editOpen,
  editTarget,
  validatingId,
  checkingAll,
  activeFilter,
  searchQuery,
  readyCount,
  offlineCount,
  updateCount,
  filteredServers,
  page,
  currentPage,
  pagedServers,
  handleValidate,
  handleCheckAll,
  openNode,
  openContainers,
  openEdit,
  handleUpdated,
  handleDelete,
} = useServersPage();
</script>

<template>
  <div class="servers-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · Node registry</p>
        <h1>Servers</h1>
        <p class="page-desc">
          Each node runs a <code class="inline-code">gotham-agent</code> connected to
          the gRPC gateway <span class="mono">:9442</span> over server-authenticated
          TLS. The control plane never calls Docker directly — every command goes
          through the agent.
        </p>
      </div>
      <div class="page-actions">
        <NButton
          secondary
          :loading="checkingAll"
          :disabled="serversStore.servers.length === 0"
          @click="() => void handleCheckAll()"
        >
          Check SSH
        </NButton>
        <NButton type="primary" @click="wizardOpen = true">
          Add server
        </NButton>
      </div>
    </div>

    <NCard>
      <NAlert
        v-if="serversStore.error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ serversStore.error }}
      </NAlert>

      <ServersToolbar
        v-model:active-filter="activeFilter"
        v-model:search-query="searchQuery"
        :ready-count="readyCount"
        :offline-count="offlineCount"
        :update-count="updateCount"
        :total-count="serversStore.servers.length"
      />

      <NSpin v-if="serversStore.loading && filteredServers.length === 0" class="servers-loading" />
      <template v-else-if="filteredServers.length > 0">
        <!-- Node usage bars render through usageView in ServerCard, keeping
             the shared 80% danger threshold (no hard-coded comparisons). -->
        <div class="grid cols-2 node-list" data-od-id="node-list">
          <ServerCard
            v-for="server in pagedServers"
            :key="server.id"
            :server="server"
            :validating="validatingId === server.id"
            @open="openNode(server.id)"
            @edit="openEdit(server)"
            @validate="handleValidate(server)"
            @containers="openContainers(server.id)"
            @delete="handleDelete(server)"
          />
        </div>
        <NPagination
          v-if="filteredServers.length > serversPageSize"
          class="servers-pagination"
          :page="currentPage"
          :page-size="serversPageSize"
          :item-count="filteredServers.length"
          @update:page="page = $event"
        />
      </template>
      <NEmpty
        v-else
        class="servers-empty"
        :description="
          serversStore.servers.length === 0
            ? 'No servers yet'
            : 'No nodes match the current filters'
        "
      >
        <template #icon>
          <NIcon>
            <GothamIcon name="server" />
          </NIcon>
        </template>
        <template #extra>
          <p class="servers-empty-hint">
            {{
              serversStore.servers.length === 0
                ? "Add your first server over SSH to begin."
                : "Adjust the filters or add a new server over SSH."
            }}
          </p>
          <NButton type="primary" @click="wizardOpen = true">
            Add server
          </NButton>
        </template>
      </NEmpty>
    </NCard>

    <AddServerWizard v-model:show="wizardOpen" />
    <EditServerModal
      v-model:show="editOpen"
      :server="editTarget"
      @updated="handleUpdated"
    />
  </div>
</template>

<style scoped>
.servers-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
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

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.inline-code {
  font-family: var(--font-mono);
  font-size: 0.92em;
  background: var(--surface-warm);
  border-radius: 3px;
  padding: 1px 5px;
  color: var(--fg);
}

.mono {
  font-family: var(--font-mono);
}

.servers-empty {
  padding: var(--space-8) 0;
}

.servers-empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}

/* ── Node grid (docs/design/servers.html) ──
   The list region is a grid of node cards, not a data table. These rules are
   ported from docs/design/assets/gotham-views.css (.node-card family) and
   docs/design/assets/gotham-ui.css (grid/avatar/tag/meter/kv utilities) that
   the app does not carry globally; tokens come from styles/tokens.css. */
.servers-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-4);
}

.grid {
  display: grid;
  gap: var(--space-4);
  margin-top: var(--space-4);
}

.cols-2 {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

@media (max-width: 940px) {
  .cols-2 {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
