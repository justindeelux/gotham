<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NInput,
  NSpace,
  NSpin,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { describeApplicationError, listApplications } from "@/features/applications/api/applications";
import type { Application } from "@/features/applications/api/applications";
import CreateAppWizard from "@/features/applications/components/CreateAppWizard.vue";
import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { useProvidersStore } from "@/features/applications/stores/providers";

const router = useRouter();
const message = useMessage();
const providersStore = useProvidersStore();

const wizardOpen = ref(false);
const listError = ref<string | null>(null);
// Start loading so the first paint shows the spinner, never the empty state
// before the initial list response lands (C4-16).
const listLoading = ref(true);
const knownApps = ref<Application[]>([]);
const openById = ref("");

/**
 * fetchKnownApplications reads the mounted list route. A rejection renders
 * explicitly — the page never fabricates rows.
 */
async function fetchKnownApplications(): Promise<void> {
  listLoading.value = true;
  listError.value = null;
  try {
    knownApps.value = await listApplications();
  } catch (error) {
    knownApps.value = [];
    listError.value = describeApplicationError(error);
  } finally {
    listLoading.value = false;
  }
}

/** openApplication navigates to the detail page for a pasted id. */
function openApplication(): void {
  const id = openById.value.trim();
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

/** refreshProviders reloads the provider connections. */
async function refreshProviders(): Promise<void> {
  try {
    await providersStore.fetchProviders();
    message.success("Providers refreshed");
  } catch {
    // The store already exposes the error; no extra toast needed.
  }
}

onMounted(() => {
  void fetchKnownApplications();
  void providersStore.fetchProviders().catch(() => undefined);
});
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
        <NButton secondary @click="() => void refreshProviders()">
          Refresh providers
        </NButton>
        <NButton type="primary" @click="wizardOpen = true">
          Create application
        </NButton>
      </div>
    </div>

    <NCard title="Source providers">
      <template #header-extra>
        <NText depth="3">Each provider uses its own OAuth app</NText>
      </template>
      <NAlert
        v-if="providersStore.error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ providersStore.error }}
      </NAlert>
      <NSpace vertical :size="8">
        <div
          v-for="provider in providersStore.providers"
          :key="provider.id"
          class="provider-row"
        >
          <GothamIcon name="box" />
          <NText strong>{{ provider.provider }}</NText>
          <NTag :type="provider.connected ? 'success' : 'warning'" size="small" round>
            {{ provider.connected ? "Connected" : "Not connected" }}
          </NTag>
          <NText depth="3" class="mono">{{ provider.base_url || "—" }}</NText>
        </div>
        <NEmpty
          v-if="!providersStore.loading && providersStore.providers.length === 0"
          description="No source providers connected yet."
        />
      </NSpace>
    </NCard>

    <NCard title="Applications">
      <NAlert
        v-if="listError"
        type="warning"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ listError }} Create an application to get started, or open one by ID below.
      </NAlert>

      <NSpace v-if="knownApps.length > 0" vertical :size="8">
        <div
          v-for="app in knownApps"
          :key="app.id"
          class="provider-row"
        >
          <GothamIcon name="box" />
          <NText strong class="mono">{{ app.name }}</NText>
          <NText depth="3" class="mono">{{ app.id.slice(0, 8) }}</NText>
          <NText depth="3" class="mono">{{ app.branch || "—" }}</NText>
          <NButton
            quaternary
            size="small"
            @click="() => router.push({ name: 'application-detail', params: { id: app.id } })"
          >
            Open
          </NButton>
        </div>
      </NSpace>

      <!-- Render the initial load as a spinner instead of flashing the empty
           state before the first response lands (C4-16). -->
      <div v-else-if="listLoading" class="apps-loading">
        <NSpin size="small" />
        <NText depth="3">Loading applications…</NText>
      </div>

      <NEmpty
        v-else
        class="apps-empty"
        description="No applications yet"
      >
        <template #icon>
          <NIcon>
            <GothamIcon name="box" />
          </NIcon>
        </template>
        <template #extra>
          <p class="apps-empty-hint">
            Create the first application to get a deployment pipeline with
            environment, volumes and rollback per release.
          </p>
          <NSpace justify="center" :size="8">
            <NButton type="primary" @click="wizardOpen = true">
              Create application
            </NButton>
          </NSpace>
          <div class="open-by-id">
            <NInput
              v-model:value="openById"
              class="mono"
              placeholder="Paste an application ID to open it"
              aria-label="Application ID"
              @keyup.enter="openApplication"
            />
            <NButton secondary :disabled="openById.trim() === ''" @click="openApplication">
              Open
            </NButton>
          </div>
        </template>
      </NEmpty>
    </NCard>

    <CreateAppWizard v-model:show="wizardOpen" @created="handleCreated" />
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

.provider-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--border);
}

.provider-row:last-child {
  border-bottom: 0;
}

.apps-empty {
  padding: var(--space-8) 0;
}

.apps-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-8) 0;
}

.apps-empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
  max-width: 62ch;
}

.open-by-id {
  display: flex;
  gap: var(--space-2);
  max-width: 520px;
  margin: var(--space-4) auto 0;
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>
