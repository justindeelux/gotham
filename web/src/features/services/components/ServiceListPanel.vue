<script setup lang="ts">
import { NAlert, NButton, NEmpty, NSpin } from "naive-ui";

import ServiceCard from "@/features/services/components/ServiceCard.vue";
import { useServicesPageContext } from "@/features/services/composables/useServicesList";
import { useServicesStore } from "@/features/services/stores/services";

/** ServiceListPanel renders the history alert, the card grid and empties. */
const servicesStore = useServicesStore();
const { activeTab, filteredServices, failedHistories, retryHistories } =
  useServicesPageContext();
</script>

<template>
  <NSpin :show="servicesStore.loading && servicesStore.services.length === 0">
    <NAlert
      v-if="failedHistories.length > 0"
      type="warning"
      :show-icon="true"
      class="history-alert"
      data-testid="list-history-unavailable"
    >
      <div class="history-alert__body">
        <span>
          Deploy history could not be read for
          {{ failedHistories.length }}
          service{{ failedHistories.length === 1 ? "" : "s" }}. The cards
          mark those histories as unavailable instead of reporting them as
          empty.
        </span>
        <NButton size="small" @click="retryHistories">Retry</NButton>
      </div>
    </NAlert>
    <div v-if="filteredServices.length > 0" class="svc-grid">
      <ServiceCard
        v-for="service in filteredServices"
        :key="service.id"
        :service="service"
      />
    </div>

    <NEmpty
      v-else-if="servicesStore.services.length === 0 && !servicesStore.loading"
      description="No compose services yet."
      class="empty"
    >
      <template #extra>
        <p class="empty-hint">
          Import an existing compose document, or start from a template in
          the gallery.
        </p>
        <NButton type="primary" @click="activeTab = 'templates'">
          Open the template gallery
        </NButton>
      </template>
    </NEmpty>

    <NEmpty
      v-else-if="!servicesStore.loading"
      description="No service matches the filter."
      class="empty"
    />
  </NSpin>
</template>

<style scoped>
.svc-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: var(--space-4);
}

.empty {
  padding: var(--space-6) 0;
}

.history-alert {
  margin-bottom: var(--space-4);
}

.history-alert__body {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
  max-width: 60ch;
}
</style>
