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
import { RouterLink } from "vue-router";

import { projectResourceTotal, resourceSummary } from "@/features/projects/api/projects";
import { useProjectsPage } from "@/features/projects/composables/useProjectsPage";
import {
  isProjectDescriptionValid,
  isProjectNameValid,
  projectCreateRules,
} from "@/features/projects/schemas/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";
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
const createRules = projectCreateRules();
</script>

<template>
  <div class="projects-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Workspace</p>
        <h1>Projects</h1>
        <p class="page-desc">
          A project groups the applications, services and databases that make
          up one product. Each project has environments (production, staging,
          …); every resource runs on one server you choose.
        </p>
      </div>
      <div class="page-actions">
        <NButton v-if="canCreate" type="primary" @click="openCreate()">
          New project
        </NButton>
      </div>
    </div>

    <NSpace v-if="projectsStore.error" vertical :size="8">
      <NAlert type="error" :show-icon="true">
        {{ projectsStore.error }}
      </NAlert>
      <div><NButton size="small" @click="void reload()">Retry</NButton></div>
    </NSpace>

    <div class="toolbar">
      <NInput
        v-model:value="query"
        placeholder="Search projects…"
        aria-label="Search projects"
        clearable
        style="max-width: 300px"
      />
    </div>

    <NSpin v-if="projectsStore.loading && !projectsStore.loaded" description="Loading projects…" />

    <template v-else-if="projectsStore.loaded">
      <NEmpty
        v-if="filtered.length === 0 && query.trim() === ''"
        description="Create your first project to deploy an application, service or database."
      >
        <template #extra>
          <NButton v-if="canCreate" type="primary" @click="openCreate()">
            New project
          </NButton>
        </template>
      </NEmpty>

      <NEmpty
        v-else-if="filtered.length === 0"
        description="No project matches this search."
      />

      <div v-else class="project-grid">
        <RouterLink
          v-for="project in filtered"
          :key="project.id"
          class="project-card-link"
          :to="{ name: 'project-detail', params: { projectId: project.id } }"
          :aria-label="`Open project ${project.name}`"
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
                {{ project.environment_count }}
                {{ project.environment_count === 1 ? "environment" : "environments" }}
              </NText>
              <NText depth="3" class="counts">
                {{ resourceSummary(project.resource_counts) }}
              </NText>
              <NText depth="3" class="counts">
                {{ projectResourceTotal(project) }}
                {{ projectResourceTotal(project) === 1 ? "resource" : "resources" }} total
              </NText>
            </NSpace>
          </NCard>
        </RouterLink>
      </div>
    </template>

    <NModal
      v-model:show="createOpen"
      preset="card"
      title="New project"
      style="width: 460px; max-width: 94vw"
    >
      <NForm
        :model="{ name: createName, description: createDescription }"
        :rules="createRules"
      >
        <NSpace vertical :size="12">
          <NText depth="3">
            A <span class="mono">production</span> environment is created with it.
          </NText>
          <NFormItem
            label="Name"
            path="name"
            :feedback="createConflict.feedback.value"
            :validation-status="createConflict.status.value"
          >
            <NInput
              v-model:value="createName"
              placeholder="storefront"
              maxlength="64"
              show-count
              :input-props="{ id: 'project-create-name', 'aria-label': 'Project name' }"
              @update:value="createConflict.clear()"
              @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleCreate)"
            />
          </NFormItem>
          <NText depth="3">Unique within the team. 1-64 characters.</NText>
          <NFormItem label="Description (optional)" path="description">
            <NInput
              v-model:value="createDescription"
              placeholder="What this product is"
              maxlength="500"
              show-count
              :input-props="{ id: 'project-create-description', 'aria-label': 'Project description' }"
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
          <NButton @click="createOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="createBusy"
            :disabled="!isProjectNameValid(createName) || !isProjectDescriptionValid(createDescription)"
            @click="void handleCreate()"
          >
            Create project
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
