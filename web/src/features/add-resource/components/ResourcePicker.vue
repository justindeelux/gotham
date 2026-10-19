<script setup lang="ts">
import { NButton, NEmpty, NInput, NSpin, NTab, NTabs } from "naive-ui";
import { computed, nextTick, onMounted, ref, useId, watch } from "vue";
import { useRouter } from "vue-router";

import CreateAppWizard from "@/features/applications/components/CreateAppWizard.vue";
import type { Application } from "@/features/applications/api/applications";
import CreateDatabaseWizard from "@/features/databases/components/CreateDatabaseWizard.vue";
import type { CreatedDatabase } from "@/features/databases/api/databases";
import { ENGINES, engineByValue } from "@/features/databases/utils/databaseEngines";
import TemplateWizard from "@/features/templates/components/TemplateWizard.vue";
import {
  templateOverlayDescription,
} from "@/features/templates/api/templates";
import type { TemplateSummary } from "@/features/templates/api/templates";
import { useTemplatesStore } from "@/features/templates/stores/templates";
import { activeLocale, i18n } from "@/shared/i18n";
import BrandIcon from "./BrandIcon.vue";
import { brandFor, templateBrand } from "../utils/brands";

// Each resource type opens its existing wizard with the environment scope preselected.
const props = defineProps<{
  projectId: string;
  environmentId: string;
}>();

const emit = defineEmits<{
  created: [];
  wizardOpen: [value: boolean];
  composeImport: [];
}>();

const router = useRouter();
const templatesStore = useTemplatesStore();

/**
 * t renders page copy in the active locale (tracks language switches).
 * Template and engine names stay raw values interpolated as parameters.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

const scopeProjectId = computed<string>(() => props.projectId);
const scopeEnvironmentId = computed<string>(() => props.environmentId);

const RESOURCE_TABS = ["application", "service", "database"] as const;

type ResourceTab = (typeof RESOURCE_TABS)[number];

const activeTab = ref<ResourceTab>("application");
const tabsId = useId();

/**
 * tabPropsFor carries the tab semantics NTabs omits (2.45.3 renders plain
 * divs): each tab is a roving-tabindex tab owned by the tablist.
 */
function tabPropsFor(tab: ResourceTab): Record<string, string | number | boolean> {
  const selected = tab === activeTab.value;
  return {
    id: `${tabsId}-${tab}`,
    "aria-controls": `${tabsId}-panel`,
    role: "tab",
    "aria-selected": selected,
    tabindex: selected ? 0 : -1,
  };
}

/**
 * handleTabKeydown moves and activates tabs with arrows/Home/End
 * (NTabs 2.45.3 has no key handling of its own). Keys pressed inside a
 * tab panel belong to the panel, so only a focused tab is handled.
 */
function handleTabKeydown(event: KeyboardEvent): void {
  const target = event.target as HTMLElement | null;
  const tab = target?.closest<HTMLElement>(".n-tabs-tab");
  if (!tab) {
    return;
  }
  const tabs = Array.from(
    (event.currentTarget as HTMLElement | null)?.querySelectorAll<HTMLElement>(
      ".n-tabs-tab",
    ) ?? [],
  );
  const current = tabs.indexOf(tab);
  if (current < 0) {
    return;
  }
  let next = -1;
  switch (event.key) {
    case "ArrowRight":
      next = (current + 1) % tabs.length;
      break;
    case "ArrowLeft":
      next = (current - 1 + tabs.length) % tabs.length;
      break;
    case "Home":
      next = 0;
      break;
    case "End":
      next = tabs.length - 1;
      break;
    default:
      return;
  }
  event.preventDefault();
  if (next === current) {
    return;
  }
  activeTab.value = RESOURCE_TABS[next];
  // Roving tabindex: focus follows the selection after re-render (the
  // tab list is captured now: it reads null once dispatch ends).
  const tablist = event.currentTarget as HTMLElement | null;
  nextTick(() => {
    tablist?.querySelectorAll<HTMLElement>(".n-tabs-tab")[next]?.focus();
  });
}

/** serviceQuery filters the service cards by name or description. */
const serviceQuery = ref("");

/** filteredTemplates matches the service query against name and description. */
const filteredTemplates = computed<TemplateSummary[]>(() => {
  const needle = serviceQuery.value.trim().toLowerCase();
  if (needle === "") {
    return templatesStore.templates;
  }
  return templatesStore.templates.filter((template) => {
    const haystack =
      `${template.name} ${template.description} ${templateOverlayDescription(template)}`.toLowerCase();
    return haystack.includes(needle);
  });
});

/** sourceLabelKey maps a wizard source value to its selector label. */
const SOURCE_LABEL_KEYS: Record<string, string> = {
  git_public: "applications.wizard.sourceGitPublic",
  git_private: "applications.wizard.sourceGitPrivate",
  github_app: "applications.wizard.sourceGithubApp",
  gitlab_app: "applications.wizard.sourceGitlabApp",
  dockerfile: "applications.wizard.sourceDockerfile",
  image: "applications.wizard.sourceImage",
  compose: "applications.wizard.sourceCompose",
};

/** sourceValues lists every wizard source type as its own picker card. */
const sourceValues = Object.keys(SOURCE_LABEL_KEYS);

/** sourceLabel renders the wizard's own label for one source value. */
function sourceLabel(value: string): string {
  return t(SOURCE_LABEL_KEYS[value] ?? value);
}

/** sourceDescription renders the one-line catalog copy for one source. */
function sourceDescription(value: string): string {
  const key = `add-resource.sources.${value}`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return value;
}

/** engineDescription renders the one-line catalog copy for one engine. */
function engineDescription(value: string): string {
  const key = `add-resource.engines.${value}`;
  if (i18n.global.te(key)) {
    return String(i18n.global.t(key));
  }
  return engineByValue(value).label;
}

/** engineImage renders the repo:default-tag preview of one engine. */
function engineImage(value: string): string {
  const engine = engineByValue(value);
  return `${engine.repo}:${engine.defaultVersion}`;
}

const appSource = ref("");
const appOpen = ref(false);
const templateSlug = ref("");
const templateOpen = ref(false);
const databaseEngine = ref("postgres");
const databaseOpen = ref(false);

/**
 * wizardOpen is true while any create wizard covers the picker; the host
 * hides the picker modal behind it (the picker stays mounted).
 */
const wizardOpen = computed<boolean>(
  () => appOpen.value || templateOpen.value || databaseOpen.value,
);

watch(wizardOpen, (open) => emit("wizardOpen", open));

/** openApplication opens the application wizard with one source preselected. */
function openApplication(sourceType: string): void {
  appSource.value = sourceType;
  appOpen.value = true;
}

/** openTemplate opens the template wizard with one template preselected. */
function openTemplate(slug: string): void {
  templateSlug.value = slug;
  templateOpen.value = true;
}

/** openDatabase opens the database wizard with one engine preselected. */
function openDatabase(engine: string): void {
  databaseEngine.value = engine;
  databaseOpen.value = true;
}

/**
 * handleGroupKey moves focus inside one card group with the arrow keys
 * (and Home/End): the card activates on Enter as a plain button.
 */
function handleGroupKey(event: KeyboardEvent): void {
  const container = event.currentTarget as HTMLElement | null;
  const cards = container?.querySelectorAll<HTMLElement>("button.res-card");
  if (!cards || cards.length === 0) {
    return;
  }
  const current = Array.from(cards).indexOf(document.activeElement as HTMLElement);
  let next: number;
  if (event.key === "ArrowRight" || event.key === "ArrowDown") {
    next = (current + 1) % cards.length;
  } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
    next = (current + cards.length - 1) % cards.length;
  } else if (event.key === "Home") {
    next = 0;
  } else if (event.key === "End") {
    next = cards.length - 1;
  } else {
    return;
  }
  event.preventDefault();
  cards[next].focus();
}

/** afterApplicationCreate closes the wizard and opens the new application. */
function afterApplicationCreate(application: Application): void {
  appOpen.value = false;
  emit("created");
  void router.push({
    name: "application-detail",
    params: {
      projectId: application.project_id,
      environmentId: application.environment_id,
      id: application.id,
    },
  });
}

/** afterDatabaseCreate closes the wizard and opens the new database. */
function afterDatabaseCreate(created: CreatedDatabase): void {
  databaseOpen.value = false;
  emit("created");
  void router.push({
    name: "database-detail",
    params: {
      projectId: created.database.project_id,
      environmentId: created.database.environment_id,
      id: created.database.id,
    },
  });
}

onMounted(() => {
  void templatesStore.fetchTemplates().catch(() => undefined);
});
</script>

<template>
  <div class="resource-picker">
    <NTabs
      v-model:value="activeTab"
      type="line"
      role="tablist"
      :aria-label="t('add-resource.tabs.label')"
      @keydown="handleTabKeydown"
    >
      <NTab
        v-for="tab in RESOURCE_TABS"
        :key="tab"
        :name="tab"
        v-bind="tabPropsFor(tab)"
      >
        {{ t(`add-resource.tabs.${tab}`) }}
      </NTab>
    </NTabs>
    <div
      :id="`${tabsId}-panel`"
      role="tabpanel"
      :aria-labelledby="`${tabsId}-${activeTab}`"
      tabindex="0"
    >
      <div v-if="activeTab === 'application'" class="res-pane">
        <p class="pane-hint">{{ t("add-resource.panels.application") }}</p>
        <div class="res-grid" @keydown="handleGroupKey">
          <button
            v-for="source in sourceValues"
            :key="source"
            type="button"
            class="res-card"
            :data-source="source"
            @click="openApplication(source)"
          >
            <span class="res-head">
              <BrandIcon v-bind="brandFor(source, sourceLabel(source))" />
              <span class="res-name">{{ sourceLabel(source) }}</span>
            </span>
            <span class="res-desc">{{ sourceDescription(source) }}</span>
          </button>
        </div>
      </div>
      <div v-else-if="activeTab === 'service'" class="res-pane">
        <p class="pane-hint">{{ t("add-resource.panels.service") }}</p>
        <p class="compose-hint">
          {{ t("add-resource.compose.hint") }}
          <a href="#" @click.prevent="emit('composeImport')">{{
            t("add-resource.compose.link")
          }}</a
          >.
        </p>
        <div class="toolbar">
          <NInput
            v-model:value="serviceQuery"
            :placeholder="t('add-resource.search.placeholder')"
            :aria-label="t('add-resource.search.label')"
            clearable
            class="search"
          />
        </div>
        <NSpin
          v-if="templatesStore.loading"
          :description="t('add-resource.search.label')"
        />
        <template v-else-if="templatesStore.error">
          <p class="small muted">{{ templatesStore.error }}</p>
          <NButton
            size="small"
            @click="templatesStore.fetchTemplates().catch(() => undefined)"
          >
            {{ t("common.actions.retry") }}
          </NButton>
        </template>
        <div
          v-else-if="filteredTemplates.length > 0"
          class="res-grid"
          @keydown="handleGroupKey"
        >
          <button
            v-for="template in filteredTemplates"
            :key="template.slug"
            type="button"
            class="res-card"
            :data-template="template.slug"
            @click="openTemplate(template.slug)"
          >
            <span class="res-head">
              <BrandIcon v-bind="templateBrand(template.icon, template.name)" />
              <span class="res-name">{{ template.name }}</span>
            </span>
            <span class="res-desc">{{ templateOverlayDescription(template) }}</span>
          </button>
        </div>
        <NEmpty
          v-else
          :description="
            t(
              serviceQuery.trim() === ''
                ? 'add-resource.search.emptyCatalog'
                : 'add-resource.search.empty',
            )
          "
        />
      </div>
      <div v-else class="res-pane">
        <p class="pane-hint">{{ t("add-resource.panels.database") }}</p>
        <div class="res-grid" @keydown="handleGroupKey">
          <button
            v-for="engine in ENGINES"
            :key="engine.value"
            type="button"
            class="res-card"
            :data-engine="engine.value"
            @click="openDatabase(engine.value)"
          >
            <span class="res-head">
              <BrandIcon v-bind="brandFor(engine.value, engine.label)" />
              <span class="res-name">{{ engine.label }}</span>
            </span>
            <span class="res-desc">{{ engineDescription(engine.value) }}</span>
            <span class="res-meta">
              <span class="tag mono">{{ engineImage(engine.value) }}</span>
              <span class="tag mono">:{{ engine.port }}</span>
            </span>
          </button>
        </div>
      </div>
    </div>

    <CreateAppWizard
      v-model:show="appOpen"
      :project-id="scopeProjectId"
      :environment-id="scopeEnvironmentId"
      :source-type="appSource"
      @created="afterApplicationCreate"
    />
    <TemplateWizard
      v-model:show="templateOpen"
      :slug="templateSlug"
      :project-id="scopeProjectId"
      :environment-id="scopeEnvironmentId"
    />
    <CreateDatabaseWizard
      v-model:show="databaseOpen"
      :project-id="scopeProjectId"
      :environment-id="scopeEnvironmentId"
      :engine="databaseEngine"
      @created="afterDatabaseCreate"
    />
  </div>
</template>

<style scoped>
.resource-picker {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

/* Reserve the same content space for each tab, including on short viewports. */
.res-pane {
  height: min(calc(var(--space-12) * 8), calc(100dvh - var(--space-12) * 5));
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.pane-hint {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--muted);
}

.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.search {
  max-width: calc(var(--space-8) * 10);
}

.res-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, calc(var(--space-12) * 5)), 1fr));
  gap: var(--space-4);
}

.res-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
  cursor: pointer;
  color: inherit;
  font: inherit;
  transition:
    border-color var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard);
}

.res-card:hover {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--surface) 88%, var(--accent));
}

.res-card:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring);
}

.res-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.res-name {
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--fg-2);
}

.res-desc {
  font-size: var(--text-xs);
  color: var(--muted);
}

.res-meta {
  margin-top: auto;
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.tag {
  font-size: var(--text-xs);
  color: var(--muted);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-1) var(--space-2);
}

.mono {
  font-family: var(--font-mono);
}

.compose-hint {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

@media (max-width: 560px) {
  .res-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .search {
    max-width: none;
    flex: 1 1 100%;
  }
}
</style>
