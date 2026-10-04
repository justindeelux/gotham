<script setup lang="ts">
import {
  NAlert,
  NButton,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { RouterLink, useRouter } from "vue-router";

import { describeServiceError, serviceStatusTagType } from "@/features/services/api/services";
import type { Service } from "@/features/services/api/services";
import { TemplateGallery } from "@/features/templates";
import { TemplateWizard } from "@/features/templates";
import { useServersStore } from "@/features/servers";
import { useServicesStore } from "@/features/services/stores/services";
import { useTemplatesStore } from "@/features/templates";
import { relativeTime } from "@/shared/utils/format";

/**
 * Services page: compose services (list, compose import) and the template
 * gallery in two tabs. Everything renders live API data; a card never shows a
 * count, a status or a container the backend did not report.
 */

type StatusFilter = "all" | "running" | "stopped";

const router = useRouter();
const message = useMessage();
const servicesStore = useServicesStore();
const templatesStore = useTemplatesStore();
const serversStore = useServersStore();

const activeTab = ref("compose");
const statusFilter = ref<StatusFilter>("all");
const search = ref("");

const importOpen = ref(false);
const importName = ref("");
const importServerId = ref("");
const importYaml = ref("");
const importAttempted = ref(false);
const importing = ref(false);
const importError = ref<string | null>(null);

/** envReference is the compose `${VAR}` substitution form shown in copy. */
const envReference = "${VAR}";

const wizardOpen = ref(false);
const wizardSlug = ref("");

/** templatePlaceholder is the engine's only supported placeholder form. */
const templatePlaceholder = "{{ .field }}";

const filters: Array<{ key: StatusFilter; label: string }> = [
  { key: "all", label: "All" },
  { key: "running", label: "Running" },
  { key: "stopped", label: "Stopped" },
];

const counts = computed<Record<StatusFilter, number>>(() => ({
  all: servicesStore.services.length,
  running: servicesStore.services.filter((item) => item.status === "running").length,
  stopped: servicesStore.services.filter((item) => item.status === "stopped").length,
}));

/** filteredServices applies the status chip and the free-text search. */
const filteredServices = computed<Service[]>(() => {
  const term = search.value.trim().toLowerCase();
  return servicesStore.services.filter((service) => {
    if (statusFilter.value !== "all" && service.status !== statusFilter.value) {
      return false;
    }
    if (term === "") {
      return true;
    }
    const haystack = [
      service.name,
      service.project_name,
      serverNameOf(service),
      ...service.domains.map((route) => route.domain),
    ]
      .join(" ")
      .toLowerCase();
    return haystack.includes(term);
  });
});

const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
  serversStore.servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
  })),
);

/** failedHistories lists services whose deploy history could not be read. */
const failedHistories = computed<Service[]>(() =>
  servicesStore.services.filter(
    (service) => servicesStore.historyOf(service.id)?.error,
  ),
);

const importNameError = computed<string>(() =>
  importAttempted.value && importName.value.trim() === ""
    ? "Enter a service name."
    : "",
);

const importNodeError = computed<string>(() =>
  importAttempted.value && importServerId.value === "" ? "Select a node." : "",
);

/** cardMark derives the card avatar from the service name. */
function cardMark(name: string): string {
  const first = name.trim()[0];
  return first ? first.toUpperCase() : "?";
}

/** serverNameOf resolves a service's node to its display name. */
function serverNameOf(service: Service): string {
  const server = serversStore.servers.find((item) => item.id === service.server_id);
  return server ? server.name : "unknown node";
}

/**
 * deploySummary describes the newest deploy attempt. A history that was never
 * read or failed to read is labelled as such: "no deploys yet" is only claimed
 * after a successful empty read, and cached rows keep their summary with a
 * stale marker when a refresh fails.
 */
function deploySummary(service: Service): string {
  const history = servicesStore.historyOf(service.id);
  const attempts = history?.deploys ?? [];
  if (attempts.length > 0) {
    const latest = attempts[0];
    const finished = latest.finished_at
      ? ` · ${relativeTime(latest.finished_at)}`
      : "";
    const stale = history?.error ? " · stale" : "";
    return `deploy #${attempts.length} · ${latest.state}${finished}${stale}`;
  }
  if (history?.error) {
    return "deploy history unavailable";
  }
  if (history?.loaded) {
    return "no deploys yet";
  }
  return "deploy history loading…";
}

/** deployTagType marks the summary tag when its history is unavailable. */
function deployTagType(service: Service): "default" | "warning" {
  return servicesStore.historyOf(service.id)?.error ? "warning" : "default";
}

/** retryHistories re-reads every failed deploy history. */
async function retryHistories(): Promise<void> {
  await servicesStore.fetchAllDeploys();
}

/** load refreshes services, deploys, templates and the node list. */
async function load(): Promise<void> {
  await Promise.allSettled([
    servicesStore.fetchServices(),
    templatesStore.fetchTemplates(),
    serversStore.fetchServers(),
  ]);
  await servicesStore.fetchAllDeploys();
}

/** openImport resets the import dialog. */
function openImport(): void {
  importName.value = "";
  importServerId.value = serversStore.servers[0]?.id ?? "";
  importYaml.value = "";
  importAttempted.value = false;
  importError.value = null;
  importOpen.value = true;
}

/** handleImport stores the pasted document as a new service. */
async function handleImport(): Promise<void> {
  importAttempted.value = true;
  if (importName.value.trim() === "" || importServerId.value === "") {
    return;
  }
  importing.value = true;
  importError.value = null;
  try {
    const created = await servicesStore.create({
      name: importName.value.trim(),
      server_id: importServerId.value,
      compose_yaml: importYaml.value,
    });
    message.success(`Service ${created.name} created.`);
    importOpen.value = false;
    await router.push({ name: "service-detail", params: { id: created.id } });
  } catch (error) {
    importError.value = describeServiceError(error);
  } finally {
    importing.value = false;
  }
}

/** openWizard opens the template wizard for one gallery card. */
function openWizard(slug: string): void {
  wizardSlug.value = slug;
  wizardOpen.value = true;
}

/** handleDelete soft-deletes a service; named volumes stay on the node. */
async function handleDelete(service: Service): Promise<void> {
  try {
    await servicesStore.remove(service.id);
    message.success(`Service ${service.name} deleted. Named volumes were kept.`);
  } catch (error) {
    message.error(describeServiceError(error));
  }
}

onMounted(() => {
  void load();
});
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
        <div class="toolbar">
          <div class="chips" role="group" aria-label="Filter services by status">
            <button
              v-for="filter in filters"
              :key="filter.key"
              type="button"
              class="chip"
              :class="{ 'is-active': statusFilter === filter.key }"
              :aria-pressed="statusFilter === filter.key"
              @click="statusFilter = filter.key"
            >
              {{ filter.label }}
              <span class="chip-count">{{ counts[filter.key] }}</span>
            </button>
          </div>
          <NInput
            v-model:value="search"
            clearable
            placeholder="Search service, node or domain…"
            aria-label="Search services"
            class="toolbar__search"
          />
        </div>

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
            <article
              v-for="service in filteredServices"
              :key="service.id"
              class="svc-card"
              :data-service="service.name"
            >
              <header class="svc-card__head">
                <span class="avatar" aria-hidden="true">{{ cardMark(service.name) }}</span>
                <div class="svc-card__title">
                  <h3 class="mono">
                    <RouterLink
                      :to="{ name: 'service-detail', params: { id: service.id } }"
                    >
                      {{ service.name }}
                    </RouterLink>
                  </h3>
                  <p class="small muted">
                    {{ serverNameOf(service) }} ·
                    <span class="mono">{{ service.project_name }}</span>
                  </p>
                </div>
                <NTag size="small" :type="serviceStatusTagType(service.status)">
                  {{ service.status }}
                </NTag>
              </header>

              <div class="svc-card__body">
                <div class="tagrow">
                  <NTag
                    size="small"
                    :type="deployTagType(service)"
                    :data-history="deploySummary(service)"
                  >
                    {{ deploySummary(service) }}
                  </NTag>
                  <NTag
                    v-for="route in service.domains"
                    :key="route.domain"
                    size="small"
                    class="mono"
                  >
                    {{ route.domain }}:{{ route.port }}
                  </NTag>
                  <NTag v-if="service.domains.length === 0" size="small">
                    no routed domain
                  </NTag>
                </div>
                <p v-if="service.domains.length > 0" class="small muted">
                  Routed through the node's Traefik proxy by the
                  <span class="mono">gotham.domain</span> label.
                </p>
              </div>

              <footer class="svc-card__foot">
                <span class="small muted grow">
                  updated {{ relativeTime(service.updated_at) }}
                </span>
                <NButton
                  size="small"
                  @click="router.push({ name: 'service-detail', params: { id: service.id } })"
                >
                  Open detail
                </NButton>
                <NPopconfirm
                  :positive-button-props="{ type: 'error' }"
                  @positive-click="handleDelete(service)"
                >
                  <template #trigger>
                    <NButton size="small" type="error" ghost>Delete</NButton>
                  </template>
                  Delete the service {{ service.name }}? The project is taken
                  down; its named volumes stay on the node.
                </NPopconfirm>
              </footer>
            </article>
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
      </NTabPane>

      <NTabPane name="templates" tab="Template gallery">
        <div class="section-head">
          <h2>Template library</h2>
          <span class="small muted">
            source: <span class="mono">GET /api/v1/templates</span> ·
            {{ templatesStore.templates.length }} templates
          </span>
        </div>
        <p class="page-desc section-desc">
          Pick a template, fill its form, and the control plane renders the
          compose document before deploying it through the agent. The form comes
          from the template schema, so a new template needs no UI change.
        </p>
        <TemplateGallery
          :templates="templatesStore.templates"
          :loading="templatesStore.loading"
          :error="templatesStore.error"
          @select="openWizard"
        />
        <div class="callout">
          <h4>How a template is structured</h4>
          <p>
            <span class="mono">templates/{slug}/template.yaml</span> declares the
            name, icon, description and the form fields (type, default,
            required, validation).
          </p>
          <p>
            <span class="mono">templates/{slug}/compose.yaml</span> uses
            <span class="mono">{{ templatePlaceholder }}</span> placeholders. The
            engine only substitutes strings and never executes code, so a
            template cannot run commands on a node. Editing a template does not
            affect a service that is already deployed.
          </p>
        </div>
      </NTabPane>
    </NTabs>

    <NModal
      v-model:show="importOpen"
      preset="card"
      title="Import compose"
      style="width: 640px; max-width: 96vw"
    >
      <NSpin :show="importing">
        <NAlert v-if="importError" type="error" :show-icon="true" class="mb-3">
          {{ importError }}
        </NAlert>
        <p class="small muted mb-3">
          Paste an existing compose document. The control plane stores it
          verbatim in <span class="mono">services.compose_yaml</span> and
          validates it before saving.
        </p>
        <NForm label-placement="top">
          <div class="import-grid">
            <NFormItem
              label="Service name"
              required
              :feedback="importNameError"
              :validation-status="importNameError ? 'error' : undefined"
              class="field-import-name"
            >
              <NInput
                v-model:value="importName"
                placeholder="blog-staging"
                aria-label="Service name"
              />
            </NFormItem>
            <NFormItem
              label="Node"
              required
              :feedback="importNodeError"
              :validation-status="importNodeError ? 'error' : undefined"
              class="field-import-node"
            >
              <NSelect
                v-model:value="importServerId"
                :options="serverOptions"
                placeholder="Select a node"
                aria-label="Node"
              />
            </NFormItem>
          </div>
          <NFormItem label="compose.yaml" class="field-import-yaml">
            <NInput
              v-model:value="importYaml"
              type="textarea"
              class="mono"
              spellcheck="false"
              :autosize="{ minRows: 10, maxRows: 24 }"
              placeholder="services:&#10;  web:&#10;    image: nginx:1.27-alpine"
              aria-label="compose.yaml"
            />
          </NFormItem>
        </NForm>
        <NText depth="3" class="small">
          <span class="mono">{{ envReference }}</span> references are substituted from
          the environment before the agent validates the document. A compose
          service is routed by the
          <span class="mono">gotham.domain</span> label. Files larger than
          1&nbsp;MiB are rejected by the API.
        </NText>
      </NSpin>
      <template #footer>
        <NSpace :size="8" justify="end">
          <NButton @click="importOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="importing"
            :disabled="serversStore.servers.length === 0"
            @click="handleImport"
          >
            Import service
          </NButton>
        </NSpace>
      </template>
    </NModal>

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

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin: var(--space-4) 0;
}

.toolbar__search {
  margin-left: auto;
  max-width: 300px;
}

.chips {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: var(--radius-pill);
  border: 1px solid var(--border-soft);
  background: var(--surface);
  color: var(--muted);
  font: inherit;
  font-size: var(--text-xs);
  cursor: pointer;
}

.chip:hover {
  color: var(--fg-2);
}

.chip.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  border-color: var(--accent);
}

.chip-count {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
}

.svc-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: var(--space-4);
}

.svc-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
}

.svc-card__head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
}

.avatar {
  width: 34px;
  height: 34px;
  border-radius: var(--radius-md);
  display: grid;
  place-items: center;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-xs);
  color: var(--fg-2);
  flex: 0 0 auto;
}

.svc-card__title {
  flex: 1 1 auto;
  min-width: 0;
}

.svc-card__title h3 {
  margin: 0;
  font-size: var(--text-base);
  font-weight: 600;
}

.svc-card__body {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.tagrow {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.svc-card__foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  border-top: 1px solid var(--border);
  padding-top: var(--space-3);
}

.grow {
  flex: 1 1 auto;
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
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

.section-head {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
  margin-top: var(--space-4);
}

.section-head h2 {
  margin: 0;
  font-size: var(--text-lg);
}

.section-desc {
  margin: var(--space-2) 0 var(--space-4);
}

.callout {
  margin-top: var(--space-5);
  border: 1px solid var(--border);
  border-left: 4px solid var(--accent);
  border-radius: var(--radius-sm);
  background: var(--surface);
  padding: var(--space-3) var(--space-4);
}

.callout h4 {
  font-size: var(--text-sm);
  margin: 0 0 var(--space-2);
}

.callout p {
  font-size: var(--text-xs);
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.callout p:last-child {
  margin-bottom: 0;
}

.import-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--form-item-gap) var(--space-4);
}

.mono {
  font-family: var(--font-mono);
}

.mb-3 {
  margin-bottom: var(--space-3);
}

@media (max-width: 860px) {
  .import-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>
