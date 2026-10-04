<script setup lang="ts">
import { NButton } from "naive-ui";
import { RouterLink } from "vue-router";
</script>

<template>
  <div class="page-head">
    <div>
      <p class="eyebrow">Overview</p>
      <h1>Dashboard</h1>
      <p class="page-desc">
        One control plane for every node, application, and database.
      </p>
    </div>
    <div class="page-actions">
      <RouterLink :to="{ name: 'servers' }" custom>
        <template #default="{ navigate }">
          <NButton quaternary @click="navigate">View servers</NButton>
        </template>
      </RouterLink>
      <!-- The add-server wizard lives on the servers page (no dedicated
           route), so this links there with a forward-compatible flag. -->
      <RouterLink :to="{ name: 'servers', query: { add: '1' } }" custom>
        <template #default="{ navigate }">
          <NButton type="primary" @click="navigate">Add server</NButton>
        </template>
      </RouterLink>
    </div>
  </div>
</template>

<style scoped>
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
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
  font-size: var(--text-sm);
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}
</style>
