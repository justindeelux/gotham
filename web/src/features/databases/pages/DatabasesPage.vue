<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NSpace,
  NText,
} from "naive-ui";

import CreateDatabaseWizard from "@/features/databases/components/CreateDatabaseWizard.vue";
import DatabasesEmptyState from "@/features/databases/components/DatabasesEmptyState.vue";
import DatabasesFilterBar from "@/features/databases/components/DatabasesFilterBar.vue";
import DatabasesKpiRow from "@/features/databases/components/DatabasesKpiRow.vue";
import DatabasesTable from "@/features/databases/components/DatabasesTable.vue";
import { useDatabasesList } from "@/features/databases/composables/useDatabasesList";

const list = useDatabasesList();
const {
  databasesStore,
  wizardOpen,
  restartingId,
  activeFilter,
  searchQuery,
  filteredDatabases,
  counts,
  breakdown,
} = list;
</script>

<template>
  <div class="databases-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · Managed database</p>
        <h1>Databases</h1>
        <p class="page-desc">
          Each database is a container with its own volume on an agent-managed
          node. Credentials are stored encrypted and never appear on the rows
          below — open a database to reveal them.
        </p>
      </div>
      <div class="page-actions">
        <NButton type="primary" @click="wizardOpen = true">
          Create database
        </NButton>
      </div>
    </div>

    <DatabasesKpiRow :counts="counts" :breakdown="breakdown" />

    <NAlert
      v-if="databasesStore.error"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ databasesStore.error }}
    </NAlert>

    <NCard title="Managed databases">
      <template #header-extra>
        <NText depth="3">{{ databasesStore.databases.length }} databases</NText>
      </template>
      <NSpace vertical :size="12">
        <DatabasesFilterBar
          :active-filter="activeFilter"
          :counts="counts"
          :search-query="searchQuery"
          @update:active-filter="activeFilter = $event"
          @update:search-query="searchQuery = $event"
        />

        <DatabasesTable
          v-if="filteredDatabases.length > 0 || databasesStore.loading"
          :databases="filteredDatabases"
          :loading="databasesStore.loading"
          :restarting-id="restartingId"
          @details="(database) => list.openDetails(database)"
          @restart="(database) => void list.handleRestart(database)"
          @delete="(database) => void list.handleDelete(database)"
        />
        <DatabasesEmptyState
          v-else
          :total-count="databasesStore.databases.length"
          @create="wizardOpen = true"
        />
      </NSpace>
      <template #footer>
        <NText depth="3">
          Deleting a database removes only the container — the volume
          <span class="mono">gotham-db-{id}</span> is kept for 7 days.
        </NText>
      </template>
    </NCard>

    <CreateDatabaseWizard
      v-model:show="wizardOpen"
      @created="() => void databasesStore.refreshDatabases()"
    />
  </div>
</template>

<style scoped>
.databases-page {
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

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>
