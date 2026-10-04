<script setup lang="ts">
import { NButton } from "naive-ui";
import { useRouter } from "vue-router";

import type { Application } from "@/features/applications/api/applications";
import ApplicationListCard from "@/features/applications/components/ApplicationListCard.vue";
import CreateAppWizard from "@/features/applications/components/CreateAppWizard.vue";
import ProviderListCard from "@/features/applications/components/ProviderListCard.vue";
import { useApplicationsList } from "@/features/applications/composables/useApplicationsList";

const router = useRouter();
const list = useApplicationsList();

/** openApplication navigates to the detail page for a pasted id. */
function openApplication(id: string): void {
  if (id === "") {
    return;
  }
  void router.push({ name: "application-detail", params: { id } });
}

/** handleCreated navigates to the detail page after the wizard succeeds. */
function handleCreated(application: Application): void {
  // The wizard already announced the create (and whether the first deploy
  // queued), so this handler only navigates — a list refetch here would run
  // against a component being unmounted and its result discarded (C4-15).
  void router.push({
    name: "application-detail",
    params: { id: application.id },
  });
}
</script>

<template>
  <div class="applications-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · Deploy from Git</p>
        <h1>Applications</h1>
        <p class="page-desc">
          From repo to container: the control plane clones the source, the
          agent builds on the node (Dockerfile, Railpack, Buildpacks, static),
          and the image is pushed to the internal registry as
          <span class="mono">gotham/{appID}:{deployID}</span>.
        </p>
      </div>
      <div class="page-actions">
        <NButton secondary @click="() => void list.refreshProviders()">
          Refresh providers
        </NButton>
        <NButton type="primary" @click="list.wizardOpen.value = true">
          Create application
        </NButton>
      </div>
    </div>

    <ProviderListCard />

    <ApplicationListCard
      :known-apps="list.knownApps.value"
      :list-loading="list.listLoading.value"
      :list-error="list.listError.value"
      :open-by-id="list.openById.value"
      @update:open-by-id="list.openById.value = $event"
      @open="openApplication"
      @create="list.wizardOpen.value = true"
    />

    <CreateAppWizard v-model:show="list.wizardOpen.value" @created="handleCreated" />
  </div>
</template>

<style scoped>
.applications-page {
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
