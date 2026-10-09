<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { computed } from "vue";
import { RouterLink, useRouter } from "vue-router";

import ResourcePicker from "@/features/add-resource/components/ResourcePicker.vue";
import ImportComposeDialog from "@/features/services/components/ImportComposeDialog.vue";
import type { Service } from "@/features/services/api/services";
import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import SharedVariablesEditor from "@/features/projects/components/SharedVariablesEditor.vue";
import { useEnvironmentPage } from "@/features/projects/composables/useEnvironmentPage";
import type { EnvironmentResourceTab } from "@/features/projects/composables/useEnvironmentPage";
import { useSharedVariables } from "@/features/projects/composables/useSharedVariables";
import type { InheritedVariable } from "@/features/projects/schemas/variables";
import { activeLocale, i18n } from "@/shared/i18n";

/**
 * Environment page (`/projects/:projectId/environments/:environmentId`,
 * PE-5 Linear JUS-34, shared variables PE-6 Linear JUS-35), ported from docs/design/environment.html: a
 * `Projects / <project> / <environment>` breadcrumb, a unified resource
 * table with type tabs and a preview switch, and an Add resource dialog
 * that opens the existing create wizards with this route's scope.
 *
 * The Shared variables section below the table edits this environment's
 * variables with the project's ones shown read-only above for context
 * (precedence: application overrides environment overrides project).
 *
 * Thin route component: table state lives in `useEnvironmentPage`; the
 * create wizards own their forms and report back through `created`.
 */
const router = useRouter();
const page = useEnvironmentPage();

/** projectVariables renders read-only above the environment editor. */
const projectVariables = useSharedVariables(() => ({
  kind: "project",
  projectId: page.projectId.value,
}));
/** environmentVariables owns the environment editor (one scope per mount). */
const environmentVariables = useSharedVariables(() => ({
  kind: "environment",
  environmentId: page.environmentId.value,
}));

/** projectInherited maps the project draft onto read-only context rows. */
const projectInherited = computed<InheritedVariable[]>(() =>
  projectVariables.draft.value.map((row) => ({
    key: row.key,
    value: row.secret ? undefined : row.value,
    secret: row.secret,
    origin: "project" as const,
  })),
);

/**
 * t renders page copy in the active locale (tracks language switches).
 * Called during render, so tabs, tables and dialogs refresh without
 * losing the search query, the selected tab or the preview switch.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

/** tabs lists the resource type tabs with localized labels. */
const tabs = computed<Array<{ key: EnvironmentResourceTab; label: string }>>(() => [
  { key: "all", label: t("projects.environment.tabs.all") },
  { key: "applications", label: t("projects.environment.tabs.applications") },
  { key: "services", label: t("projects.environment.tabs.services") },
  { key: "databases", label: t("projects.environment.tabs.databases") },
]);

/** scrollToVariables jumps to the Shared variables section. */
function scrollToVariables(): void {
  document
    .querySelector(".variables-section")
    ?.scrollIntoView({ behavior: "smooth", block: "start" });
}

/** tabCount renders the per-tab total next to its label. */
function tabCount(tab: EnvironmentResourceTab): number {
  return page.counts.value[tab];
}

/** kindLabel renders the resource kind as display text. */
function kindLabel(kind: string): string {
  if (kind === "application") {
    return t("projects.environment.kinds.application");
  }
  if (kind === "service") {
    return t("projects.environment.kinds.service");
  }
  return t("projects.environment.kinds.database");
}

/** siblingOptions lists the project's environments for the switcher. */
const siblingOptions = computed<Array<{ label: string; value: string }>>(() =>
  page.siblings.value.map((environment) => ({
    label: environment.name,
    value: environment.id,
  })),
);

/** switchEnvironment navigates to a sibling environment of the project. */
function switchEnvironment(environmentId: string): void {
  if (environmentId === "" || environmentId === page.environmentId.value) {
    return;
  }
  void router.push({
    name: "environment-detail",
    params: { projectId: page.projectId.value, environmentId },
  });
}

/**
 * handleTabKey moves the active type tab with the arrow keys (and Home/End),
 * following the tablist pattern: the tab activates on focus.
 */
function handleTabKey(event: KeyboardEvent, index: number): void {
  const entries = tabs.value;
  let next: number;
  if (event.key === "ArrowRight") {
    next = (index + 1) % entries.length;
  } else if (event.key === "ArrowLeft") {
    next = (index + entries.length - 1) % entries.length;
  } else if (event.key === "Home") {
    next = 0;
  } else if (event.key === "End") {
    next = entries.length - 1;
  } else {
    return;
  }
  event.preventDefault();
  page.tab.value = entries[next].key;
  const button = event.currentTarget as HTMLElement | null;
  const list = button?.parentElement?.querySelectorAll<HTMLElement>(".tab");
  list?.[next]?.focus();
}

/** emptyHint names what can be added when the table (or filter) is empty. */
const emptyHint = computed<string>(() =>
  page.rows.value.length === 0
    ? t("projects.environment.emptyFresh")
    : t("projects.environment.emptyFiltered"),
);

/** openImport closes the picker and opens the compose import dialog. */
function openImport(): void {
  page.addOpen.value = false;
  page.importOpen.value = true;
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

</script>

<template>
  <div class="environment-page">
    <NSpin v-if="page.loading.value && !page.resources.value" :description="t('projects.environment.loading')" />

    <NSpace v-else-if="page.error.value" vertical :size="8">
      <NEmpty
        v-if="page.notFound.value"
        :description="t('projects.environment.notFound')"
      >
        <template #extra>
          <RouterLink :to="{ name: 'projects' }">
            <NButton size="small">{{ t("projects.environment.backToProjects") }}</NButton>
          </RouterLink>
        </template>
      </NEmpty>
      <template v-else>
        <NAlert type="error" :show-icon="true">
          {{ page.error.value }}
        </NAlert>
        <div><NButton size="small" @click="void page.reload()">{{ t("common.actions.retry") }}</NButton></div>
      </template>
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
            {{ t("projects.environment.description", { project: page.resources.value.project.name, environment: page.resources.value.environment.name }) }}
          </p>
          <div v-if="siblingOptions.length > 1" class="sibling-switch">
            <NText depth="3">{{ t("projects.environment.environmentSwitcher") }}</NText>
            <NSelect
              :value="page.environmentId.value"
              :options="siblingOptions"
              :aria-label="t('projects.environment.siblingAria')"
              class="sibling-select"
              @update:value="switchEnvironment"
            />
          </div>
        </div>
        <div class="page-actions">
          <NButton @click="scrollToVariables()">
            {{ t("projects.environment.variablesButton") }}
          </NButton>
          <NButton
            v-if="page.canWrite.value"
            type="primary"
            @click="page.addOpen.value = true"
          >
            {{ t("projects.environment.addResource") }}
          </NButton>
        </div>
      </div>

      <div class="tabs" role="tablist" :aria-label="t('projects.environment.tabs.label')">
        <button
          v-for="(entry, index) in tabs"
          :key="entry.key"
          type="button"
          role="tab"
          :id="`env-tab-${entry.key}`"
          :aria-selected="page.tab.value === entry.key"
          :aria-controls="`env-panel-${entry.key}`"
          :tabindex="page.tab.value === entry.key ? 0 : -1"
          class="tab"
          :class="{ 'is-active': page.tab.value === entry.key }"
          @click="page.tab.value = entry.key"
          @keydown="handleTabKey($event, index)"
        >
          {{ entry.label }} <span class="nav-count">{{ tabCount(entry.key) }}</span>
        </button>
      </div>

      <div class="toolbar">
        <NInput
          v-model:value="page.search.value"
          :placeholder="t('projects.environment.searchPlaceholder')"
          :aria-label="t('projects.environment.searchLabel')"
          clearable
          class="search"
        />
        <label class="preview-switch">
          <NSwitch v-model:value="page.showPreviews.value" :aria-label="t('projects.environment.showPreviews')" />
          <NText depth="3">{{ t("projects.environment.showPreviews") }}</NText>
        </label>
      </div>

      <NCard
        v-if="page.visibleRows.value.length > 0"
        class="resource-card"
        role="tabpanel"
        :id="`env-panel-${page.tab.value}`"
        :aria-labelledby="`env-tab-${page.tab.value}`"
      >
        <div class="table-wrap">
          <table class="resource-table">
            <thead>
              <tr>
                <th scope="col">{{ t("projects.environment.table.name") }}</th>
                <th scope="col">{{ t("projects.environment.table.type") }}</th>
                <th scope="col">{{ t("projects.environment.table.server") }}</th>
                <th scope="col">{{ t("projects.environment.table.status") }}</th>
                <th scope="col"><span class="sr-only">{{ t("projects.environment.table.actions") }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in page.visibleRows.value"
                :key="`${row.kind}:${row.id}`"
                :class="{ 'preview-row': row.preview }"
              >
                <td :data-label="t('projects.environment.table.name')">
                  <span class="resource-name">{{ row.name }}</span>
                  <span class="cell-sub mono">{{ row.subtitle }}</span>
                </td>
                <td :data-label="t('projects.environment.table.type')">
                  <NSpace :size="4" align="center">
                    <NTag size="small">{{ kindLabel(row.kind) }}</NTag>
                    <NTag v-if="row.preview" size="small" type="info">{{ t("projects.environment.preview") }}</NTag>
                  </NSpace>
                </td>
                <td :data-label="t('projects.environment.table.server')" class="mono muted">{{ row.serverName }}</td>
                <td :data-label="t('projects.environment.table.status')">
                  <NTag size="small" :type="row.statusTag">{{ row.statusText }}</NTag>
                </td>
                <td :data-label="t('projects.environment.table.actions')" class="actions">
                  <RouterLink :to="row.to">
                    <NButton size="small">{{ t("projects.detail.open") }}</NButton>
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
            {{ t("projects.environment.addResource") }}
          </NButton>
        </template>
      </NEmpty>

      <p class="small muted">
        {{ t("projects.environment.previewNote") }}
      </p>

      <section class="variables-section" :aria-label="t('projects.variables.title')">
        <SharedVariablesEditor
          :draft="environmentVariables.draft.value"
          :loading="environmentVariables.loading.value"
          :load-error="environmentVariables.loadError.value"
          :save-error="environmentVariables.saveError.value"
          :saving="environmentVariables.saving.value"
          :save-disabled="environmentVariables.saveDisabled.value"
          :can-write="page.canWrite.value"
          :stored-secrets="[...environmentVariables.storedSecrets.value]"
          :problems="environmentVariables.problems.value"
          :inherited="projectInherited"
          :inherited-loading="projectVariables.loading.value"
          :card-title="t('projects.variables.title')"
          :precedence-hint="t('projects.variables.environmentPrecedence')"
          @update:draft="environmentVariables.draft.value = $event"
          @save="void environmentVariables.save()"
          @retry="void environmentVariables.retry()"
        />
      </section>
    </template>

    <!-- Add resource -->
    <NModal
      v-model:show="page.addOpen.value"
      preset="card"
      :title="t('projects.environment.addResource')"
      style="width: 1040px; max-width: 96vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          {{ t("projects.environment.creatingIn", { project: page.resources.value?.project.name ?? "", environment: page.resources.value?.environment.name ?? "" }) }}
        </NText>
        <ResourcePicker
          :project-id="page.projectId.value"
          :environment-id="page.environmentId.value"
          @created="page.addOpen.value = false"
        />
        <NText depth="3">
          {{ t("projects.environment.composeHintPrefix") }}
          <a href="#" @click.prevent="openImport">{{ t("projects.environment.composeHintLink") }}</a>
          {{ t("projects.environment.composeHintSuffix") }}
        </NText>
      </NSpace>
    </NModal>

    <ImportComposeDialog
      v-model:show="page.importOpen.value"
      :project-id="page.projectId.value"
      :environment-id="page.environmentId.value"
      @created="afterServiceCreate"
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

.sibling-switch {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-3);
}

.sibling-select {
  min-width: 200px;
  max-width: 320px;
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

/* Preview rows nest under their base application. */
.preview-row .resource-name {
  padding-left: var(--space-4);
  position: relative;
}

.preview-row .resource-name::before {
  content: "↳";
  position: absolute;
  left: var(--space-1);
  color: var(--muted);
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

.variables-section {
  scroll-margin-top: var(--space-4);
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
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

  .resource-table td:nth-child(2)::before,
  .resource-table td:nth-child(3)::before,
  .resource-table td:nth-child(4)::before {
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
}
</style>
