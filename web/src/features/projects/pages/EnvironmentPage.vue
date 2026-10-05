<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NInput,
  NModal,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { computed } from "vue";
import { RouterLink, useRouter } from "vue-router";

import CreateAppWizard from "@/features/applications/components/CreateAppWizard.vue";
import type { Application } from "@/features/applications/api/applications";
import CreateDatabaseWizard from "@/features/databases/components/CreateDatabaseWizard.vue";
import type { CreatedDatabase } from "@/features/databases/api/databases";
import ImportComposeDialog from "@/features/services/components/ImportComposeDialog.vue";
import type { Service } from "@/features/services/api/services";
import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import { useEnvironmentPage } from "@/features/projects/composables/useEnvironmentPage";
import type { EnvironmentResourceTab } from "@/features/projects/composables/useEnvironmentPage";

/**
 * Environment page (`/projects/:projectId/environments/:environmentId`,
 * PE-5 Linear JUS-34), ported from docs/design/environment.html: a
 * `Projects / <project> / <environment>` breadcrumb, a unified resource
 * table with type tabs and a preview switch, and an Add resource dialog
 * that opens the existing create wizards with this route's scope.
 *
 * Thin route component: table state lives in `useEnvironmentPage`; the
 * create wizards own their forms and report back through `created`.
 */
const router = useRouter();
const page = useEnvironmentPage();

const tabs: Array<{ key: EnvironmentResourceTab; label: string }> = [
  { key: "all", label: "All" },
  { key: "applications", label: "Applications" },
  { key: "services", label: "Services" },
  { key: "databases", label: "Databases" },
];

/** tabCount renders the per-tab total next to its label. */
function tabCount(tab: EnvironmentResourceTab): number {
  return page.counts.value[tab];
}

/** kindLabel renders the resource kind as display text. */
function kindLabel(kind: string): string {
  if (kind === "application") {
    return "Application";
  }
  if (kind === "service") {
    return "Service";
  }
  return "Database";
}

/** emptyHint names what can be added when the table (or filter) is empty. */
const emptyHint = computed<string>(() =>
  page.rows.value.length === 0
    ? "Nothing here yet. Deploy an application, run a service from a template, or create a database."
    : "No resources match this filter.",
);

/** openCreate opens one create wizard from the Add resource dialog. */
function openCreate(kind: "application" | "service" | "database"): void {
  page.addOpen.value = false;
  if (kind === "application") {
    page.appWizardOpen.value = true;
  } else if (kind === "service") {
    page.importOpen.value = true;
  } else {
    page.dbWizardOpen.value = true;
  }
}

/** afterApplicationCreate reloads and opens the nested application page. */
function afterApplicationCreate(application: Application): void {
  page.appWizardOpen.value = false;
  void page.reload();
  void router.push({
    name: "application-detail",
    params: {
      projectId: page.projectId.value,
      environmentId: page.environmentId.value,
      id: application.id,
    },
  });
}

/** afterServiceCreate reloads and opens the nested service page. */
function afterServiceCreate(service: Service): void {
  page.importOpen.value = false;
  void page.reload();
  void router.push({
    name: "service-detail",
    params: {
      projectId: page.projectId.value,
      environmentId: page.environmentId.value,
      id: service.id,
    },
  });
}

/** afterDatabaseCreate reloads and opens the nested database page. */
function afterDatabaseCreate(created: CreatedDatabase): void {
  page.dbWizardOpen.value = false;
  void page.reload();
  void router.push({
    name: "database-detail",
    params: {
      projectId: page.projectId.value,
      environmentId: page.environmentId.value,
      id: created.database.id,
    },
  });
}
</script>

<template>
  <div class="environment-page">
    <NSpin v-if="page.loading.value && !page.resources.value" description="Loading environment…" />

    <NSpace v-else-if="page.error.value" vertical :size="8">
      <NAlert type="error" :show-icon="true">
        {{ page.error.value }}
      </NAlert>
      <div><NButton size="small" @click="void page.reload()">Retry</NButton></div>
    </NSpace>

    <template v-else-if="page.resources.value">
      <div class="page-head">
        <div class="head-main">
          <ProjectBreadcrumb
            :project-name="page.resources.value.project.name"
            :project-id="page.resources.value.project.id"
            :environment-name="page.resources.value.environment.name"
          />
          <h1 class="title" :title="page.resources.value.environment.name">
            {{ page.resources.value.environment.name }}
          </h1>
          <p class="page-desc">
            Everything that runs for {{ page.resources.value.project.name }} in
            {{ page.resources.value.environment.name }}. Each resource is
            pinned to one server.
          </p>
        </div>
        <div class="page-actions">
          <NButton disabled title="Shared variables land in PE-6.">
            Variables
          </NButton>
          <NButton
            v-if="page.canWrite.value"
            type="primary"
            @click="page.addOpen.value = true"
          >
            Add resource
          </NButton>
        </div>
      </div>

      <div class="tabs" role="tablist" aria-label="Resource types">
        <button
          v-for="entry in tabs"
          :key="entry.key"
          type="button"
          role="tab"
          class="tab"
          :class="{ 'is-active': page.tab.value === entry.key }"
          :aria-selected="page.tab.value === entry.key"
          @click="page.tab.value = entry.key"
        >
          {{ entry.label }} <span class="nav-count">{{ tabCount(entry.key) }}</span>
        </button>
      </div>

      <div class="toolbar">
        <NInput
          v-model:value="page.search.value"
          placeholder="Search name, detail or node"
          aria-label="Search resources"
          clearable
          class="search"
        />
        <label class="preview-switch">
          <NSwitch v-model:value="page.showPreviews.value" aria-label="Show previews" />
          <NText depth="3">Show previews</NText>
        </label>
      </div>

      <NCard v-if="page.visibleRows.value.length > 0" class="resource-card">
        <div class="table-wrap">
          <table class="resource-table">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Type</th>
                <th scope="col">Server</th>
                <th scope="col">Status</th>
                <th scope="col"><span class="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in page.visibleRows.value" :key="`${row.kind}:${row.id}`">
                <td data-label="Name">
                  <span class="resource-name">{{ row.name }}</span>
                  <span class="cell-sub mono">{{ row.subtitle }}</span>
                </td>
                <td data-label="Type"><NTag size="small">{{ kindLabel(row.kind) }}</NTag></td>
                <td data-label="Server" class="mono muted">{{ row.serverName }}</td>
                <td data-label="Status">
                  <NTag size="small" :type="row.statusTag">{{ row.statusText }}</NTag>
                </td>
                <td data-label="Actions" class="actions">
                  <RouterLink :to="row.to">
                    <NButton size="small">Open</NButton>
                  </RouterLink>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </NCard>

      <NEmpty v-else :description="emptyHint">
        <template v-if="page.canWrite.value && page.rows.value.length === 0" #extra>
          <NButton type="primary" @click="page.addOpen.value = true">
            Add resource
          </NButton>
        </template>
      </NEmpty>

      <p class="small muted">
        Preview deployments of an application run in this environment on the
        same server; flip the switch to list them alongside their base.
      </p>
    </template>

    <!-- Add resource -->
    <NModal
      v-model:show="page.addOpen.value"
      preset="card"
      title="Add resource"
      style="width: 640px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          Creating in
          <span class="mono">
            {{ page.resources.value?.project.name }} /
            {{ page.resources.value?.environment.name }} </span>.
        </NText>
        <div class="kind-grid">
          <button type="button" class="kind-card" @click="openCreate('application')">
            <span class="kind-title">Application</span>
            <span class="small muted">From a Git repository</span>
          </button>
          <button type="button" class="kind-card" @click="openCreate('service')">
            <span class="kind-title">Service</span>
            <span class="small muted">Compose or template</span>
          </button>
          <button type="button" class="kind-card" @click="openCreate('database')">
            <span class="kind-title">Database</span>
            <span class="small muted">PostgreSQL, MySQL, Redis…</span>
          </button>
        </div>
        <NText depth="3">
          Prefer a one-click template?
          <RouterLink :to="{ name: 'templates' }">Open the template library</RouterLink>
          — its wizard takes the same project and environment.
        </NText>
      </NSpace>
    </NModal>

    <CreateAppWizard
      v-model:show="page.appWizardOpen.value"
      :project-id="page.projectId.value"
      :environment-id="page.environmentId.value"
      @created="afterApplicationCreate"
    />
    <ImportComposeDialog
      v-model:show="page.importOpen.value"
      :project-id="page.projectId.value"
      :environment-id="page.environmentId.value"
      @created="afterServiceCreate"
    />
    <CreateDatabaseWizard
      v-model:show="page.dbWizardOpen.value"
      :project-id="page.projectId.value"
      :environment-id="page.environmentId.value"
      @created="afterDatabaseCreate"
    />
  </div>
</template>

<style scoped>
.environment-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  container-type: inline-size;
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.head-main {
  min-width: 0;
  flex: 1 1 auto;
}

.title {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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

.tabs {
  display: flex;
  gap: var(--space-1);
  border-bottom: 1px solid var(--border);
  flex-wrap: wrap;
}

.tab {
  appearance: none;
  background: none;
  border: 0;
  border-bottom: 2px solid transparent;
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-sm);
  color: var(--muted);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.tab.is-active {
  color: var(--fg-2);
  border-bottom-color: var(--accent);
  font-weight: 600;
}

.nav-count {
  font-family: var(--font-mono);
  font-size: 11px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 0 6px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.search {
  max-width: 320px;
}

.preview-switch {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  margin-left: auto;
  cursor: pointer;
}

.table-wrap {
  overflow-x: auto;
}

.resource-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-sm);
}

.resource-table th,
.resource-table td {
  text-align: left;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-soft);
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.resource-table th {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--muted);
}

.resource-name {
  color: var(--fg-2);
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cell-sub {
  display: block;
  font-size: var(--text-xs);
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
}

.resource-table .actions {
  text-align: right;
  white-space: normal;
}

.mono {
  font-family: var(--font-mono);
}

.muted {
  color: var(--muted);
}

.small {
  font-size: var(--text-xs);
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
}

.kind-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-3);
}

.kind-card {
  appearance: none;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-3);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  align-items: flex-start;
  text-align: left;
  cursor: pointer;
}

.kind-card:hover {
  border-color: var(--accent);
}

.kind-title {
  font-weight: 600;
  color: var(--fg-2);
}

@container (max-width: 560px) {
  .page-actions {
    margin-left: 0;
  }

  .search {
    max-width: none;
    flex: 1 1 100%;
  }

  .preview-switch {
    margin-left: 0;
  }

  /* Narrow: each resource becomes a stacked card so Open stays reachable
     without horizontal scrolling. */
  .resource-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }

  .resource-table,
  .resource-table tbody,
  .resource-table tr,
  .resource-table td {
    display: block;
    width: auto;
    max-width: none;
  }

  .resource-table tr {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }

  .resource-table td {
    border-bottom: 0;
    padding: 4px 0;
    white-space: normal;
    overflow: visible;
    text-overflow: clip;
  }

  .resource-table td[data-label="Type"]::before,
  .resource-table td[data-label="Server"]::before,
  .resource-table td[data-label="Status"]::before {
    content: attr(data-label) ": ";
    font-family: var(--font-mono);
    font-size: 11px;
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .resource-table .actions {
    text-align: left;
  }

  .kind-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
