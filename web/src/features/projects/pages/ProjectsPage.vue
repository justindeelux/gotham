<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSpace,
  NSpin,
  NText,
} from "naive-ui";
import type { FormInst } from "naive-ui";
import { RouterLink } from "vue-router";
import { onUnmounted, ref } from "vue";

import { createVisibleValidation } from "@/features/auth";
import { projectResourceTotal, resourceSummary } from "@/features/projects/api/projects";
import { useProjectsPage } from "@/features/projects/composables/useProjectsPage";
import {
  isProjectDescriptionValid,
  isProjectNameValid,
  projectCreateRules,
} from "@/features/projects/schemas/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { activeLocale, i18n, onLocaleChange } from "@/shared/i18n";
import { submitOnEnter } from "@/features/projects/utils/submitOnEnter";

/**
 * Projects list (`/projects`, PE-4 Linear JUS-33).
 *
 * Ported from docs/design/projects.html: a card per project (name,
 * description, environment count, resource counts per type), a name search,
 * a create dialog, and an empty state inviting the first project. Viewers
 * see the list but no create button. The mockup's health line ("all
 * running") has no contract field behind it, so no health is shown rather
 * than a fabricated one.
 *
 * The create form renders through NForm with schema rules (inline client
 * errors) and visible labels; a 409 taken name renders inline on the name
 * field. Submits run from an Enter keydown (never keyup, which would fire
 * from the keystroke that opened the dialog).
 *
 * Thin route component: UI state lives in `useProjectsPage`, data in the
 * projects store.
 */
const projectsStore = useProjectsStore();
const page = useProjectsPage();
const {
  query,
  filtered,
  createOpen,
  createName,
  createDescription,
  createBusy,
  createError,
  createConflict,
  canCreate,
  openCreate,
  handleCreate,
  reload,
} = page;
/**
 * visible tracks create-form paths with shown feedback; a language switch
 * revalidates exactly those paths so visible errors refresh while pristine
 * fields stay clean (shared visibleValidation pattern via features/auth).
 */
const createVisible = createVisibleValidation();
const createRules = createVisible.trackRules(projectCreateRules());
const createFormRef = ref<FormInst | null>(null);

const stopCreateLocaleWatch = onLocaleChange(() => {
  createVisible.refreshVisible(createFormRef);
});

onUnmounted(() => {
  stopCreateLocaleWatch();
});

/**
 * t renders page copy in the active locale (tracks language switches).
 * Called during render, so labels, counts and dialog text refresh without
 * losing the search query or the create draft.
 */
function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}

/** unit picks the singular/plural unit label for a count. */
function unit(one: string, other: string, count: number): string {
  return t(count === 1 ? one : other, { count });
}
</script>

<template>
  <div class="projects-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("projects.list.eyebrow") }}</p>
        <h1>{{ t("projects.list.title") }}</h1>
        <p class="page-desc">
          {{ t("projects.list.description") }}
        </p>
      </div>
      <div class="page-actions">
        <NButton v-if="canCreate" type="primary" @click="openCreate()">
          {{ t("projects.list.newProject") }}
        </NButton>
      </div>
    </div>

    <NSpace v-if="projectsStore.error" vertical :size="8">
      <NAlert type="error" :show-icon="true">
        {{ projectsStore.error }}
      </NAlert>
      <div><NButton size="small" @click="void reload()">{{ t("common.actions.retry") }}</NButton></div>
    </NSpace>

    <div class="toolbar">
      <NInput
        v-model:value="query"
        :placeholder="t('projects.list.searchPlaceholder')"
        :aria-label="t('projects.list.searchLabel')"
        clearable
        style="max-width: 300px"
      />
    </div>

    <NSpin v-if="projectsStore.loading && !projectsStore.loaded" :description="t('projects.list.loading')" />

    <template v-else-if="projectsStore.loaded">
      <NEmpty
        v-if="filtered.length === 0 && query.trim() === ''"
        :description="t('projects.list.emptyDescription')"
      >
        <template #extra>
          <NButton v-if="canCreate" type="primary" @click="openCreate()">
            {{ t("projects.list.newProject") }}
          </NButton>
        </template>
      </NEmpty>

      <NEmpty
        v-else-if="filtered.length === 0"
        :description="t('projects.list.noMatch')"
      />

      <div v-else class="project-grid">
        <RouterLink
          v-for="project in filtered"
          :key="project.id"
          class="project-card-link"
          :to="{ name: 'project-detail', params: { projectId: project.id } }"
          :aria-label="t('projects.list.openProject', { name: project.name })"
        >
          <NCard :title="project.name" hoverable>
            <template #header-extra>
              <span class="avatar" aria-hidden="true">{{
                project.name.charAt(0).toUpperCase()
              }}</span>
            </template>
            <NSpace vertical :size="8">
              <NText v-if="project.description" depth="3" class="desc">
                {{ project.description }}
              </NText>
              <NText depth="3" class="counts">
                {{ unit("projects.list.environmentsOne", "projects.list.environmentsOther", project.environment_count) }}
              </NText>
              <NText depth="3" class="counts">
                {{ resourceSummary(project.resource_counts) }}
              </NText>
              <NText depth="3" class="counts">
                {{ unit("projects.list.resourcesTotalOne", "projects.list.resourcesTotalOther", projectResourceTotal(project)) }}
              </NText>
            </NSpace>
          </NCard>
        </RouterLink>
      </div>
    </template>

    <NModal
      v-model:show="createOpen"
      preset="card"
      :title="t('projects.create.title')"
      style="width: 460px; max-width: 94vw"
    >
      <NForm
        ref="createFormRef"
        :model="{ name: createName, description: createDescription }"
        :rules="createRules"
      >
        <NSpace vertical :size="12">
          <NText depth="3">
            {{ t("projects.create.productionNote") }}
          </NText>
          <NFormItem
            :label="t('projects.create.nameLabel')"
            path="name"
            :feedback="createConflict.feedback.value"
            :validation-status="createConflict.status.value"
          >
            <NInput
              v-model:value="createName"
              :placeholder="t('projects.create.namePlaceholder')"
              maxlength="64"
              show-count
              :input-props="{ id: 'project-create-name', 'aria-label': t('projects.create.nameAria') }"
              @update:value="createConflict.clear()"
              @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleCreate)"
            />
          </NFormItem>
          <NText depth="3">{{ t("projects.create.nameHint") }}</NText>
          <NFormItem :label="t('projects.create.descriptionLabel')" path="description">
            <NInput
              v-model:value="createDescription"
              :placeholder="t('projects.create.descriptionPlaceholder')"
              maxlength="500"
              show-count
              :input-props="{ id: 'project-create-description', 'aria-label': t('projects.create.descriptionAria') }"
              @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleCreate)"
            />
          </NFormItem>
          <NAlert v-if="createError" type="error" :show-icon="true">
            {{ createError }}
          </NAlert>
        </NSpace>
      </NForm>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="createOpen = false">{{ t("common.actions.cancel") }}</NButton>
          <NButton
            type="primary"
            :loading="createBusy"
            :disabled="!isProjectNameValid(createName) || !isProjectDescriptionValid(createDescription)"
            @click="void handleCreate()"
          >
            {{ t("projects.create.submit") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.projects-page {
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
  gap: var(--space-3);
  flex-wrap: wrap;
}

.project-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-4);
}

.project-card-link {
  text-decoration: none;
  color: inherit;
  min-width: 0;
}

.avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-pill);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-weight: 600;
}

.desc,
.counts {
  overflow: hidden;
  text-overflow: ellipsis;
}

@container (max-width: 700px) {
  .project-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@container (max-width: 480px) {
  .project-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .page-actions {
    margin-left: 0;
  }
}
</style>
