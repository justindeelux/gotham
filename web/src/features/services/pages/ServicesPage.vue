<script setup lang="ts">
import { NAlert, NButton, NTabPane, NTabs } from "naive-ui";
import { provide } from "vue";

import ImportComposeDialog from "@/features/services/components/ImportComposeDialog.vue";
import ServiceListPanel from "@/features/services/components/ServiceListPanel.vue";
import ServicesToolbar from "@/features/services/components/ServicesToolbar.vue";
import TemplateGalleryPanel from "@/features/services/components/TemplateGalleryPanel.vue";
import { servicesPageKey, useServicesList } from "@/features/services/composables/useServicesList";
import { useServicesStore } from "@/features/services/stores/services";
import TemplateWizard from "@/features/templates/components/TemplateWizard.vue";

/**
 * Services page: compose services (list, compose import) and the template
 * gallery in two tabs. Everything renders live API data; a card never shows a
 * count, a status or a container the backend did not report.
 *
 * Thin route component: list state lives in `useServicesList` (provided to
 * the panels below), sections render through them.
 */
const servicesStore = useServicesStore();
const page = useServicesList();
provide(servicesPageKey, page);
const { activeTab, openImport, wizardOpen, wizardSlug } = page;
</script>

<template>
  <div class="services-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · compose projects</p>
        <h1>Services</h1>
        <p class="page-desc">
          A service is one docker-compose project on one node. The control plane
          stores <span class="mono">compose_yaml</span> and versions it on every
          deploy; the node agent runs the compose CLI, so the behaviour stays
          Docker's.
        </p>
      </div>
      <div class="page-actions">
        <NButton @click="openImport">Import compose</NButton>
        <NButton type="primary" @click="activeTab = 'templates'">
          Deploy from template
        </NButton>
      </div>
    </div>

    <NAlert v-if="servicesStore.error" type="error" :show-icon="true">
      {{ servicesStore.error }}
    </NAlert>

    <NTabs v-model:value="activeTab" type="line" animated class="tabs">
      <NTabPane name="compose" tab="Compose services">
        <ServicesToolbar />
        <ServiceListPanel />
      </NTabPane>

      <NTabPane name="templates" tab="Template gallery">
        <TemplateGalleryPanel />
      </NTabPane>
    </NTabs>

    <ImportComposeDialog />

    <TemplateWizard v-model:show="wizardOpen" :slug="wizardSlug" />
  </div>
</template>

<style scoped>
.services-page {
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

.mono {
  font-family: var(--font-mono);
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>
