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
  NTabPane,
  NTabs,
  NText,
} from "naive-ui";
import { RouterLink, useRoute } from "vue-router";

import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import SharedVariablesEditor from "@/features/projects/components/SharedVariablesEditor.vue";
import {
  environmentResourceTotal,
  resourceSummary,
} from "@/features/projects/api/projects";
import { useProjectPage } from "@/features/projects/composables/useProjectPage";
import { useSharedVariables } from "@/features/projects/composables/useSharedVariables";
import {
  environmentNameRules,
  isEnvironmentNameValid,
  isProjectNameValid,
  projectNameRules,
} from "@/features/projects/schemas/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { submitOnEnter } from "@/features/projects/utils/submitOnEnter";

/**
 * Project detail (`/projects/:projectId`, PE-4 Linear JUS-33, shared
 * variables PE-6 Linear JUS-35).
 *
 * Ported from docs/design/project-detail.html: a `Projects / <project>`
 * breadcrumb, rename and delete actions, an Environments tab (table with
 * resource counts, open, add, rename, delete) and a Shared variables tab
 * with the precedence hint. Secrets are write-only: stored values are never
 * shown, and saving an untouched secret keeps its sealed value.
 *
 * Delete buttons stay enabled so the block is reachable by keyboard and
 * touch: the dialog explains why ("Move or delete the N resources first")
 * with a disabled confirm while blocked, and the backend's 409 stays the
 * authority. The resource Open buttons route to the environment page.
 *
 * Forms render through NForm with schema rules (inline client errors),
 * visible labels and a 409 taken name inline on the name field. Submits run
 * from an Enter keydown (never keyup, which would fire from the keystroke
 * that opened the dialog).
 *
 * Thin route component: UI state lives in `useProjectPage`, data in the
 * projects store.
 */
const route = useRoute();
const projectsStore = useProjectsStore();
const page = useProjectPage(() => String(route.params.projectId ?? ""));
/** projectVariables owns the Shared variables tab (one scope per mount). */
const projectVariables = useSharedVariables(() => ({
  kind: "project",
  projectId: String(route.params.projectId ?? ""),
}));
const {
  tab,
  renameOpen,
  renameName,
  renameBusy,
  renameError,
  renameConflict,
  deleteOpen,
  deleting,
  deleteError,
  envCreateOpen,
  envName,
  envBusy,
  envError,
  envConflict,
  envRenameTarget,
  envRenameName,
  envRenameBusy,
  envRenameError,
  envRenameConflict,
  envDeleteTarget,
  envDeleting,
  envDeleteError,
  hasResources,
  canWrite,
  openRename,
  handleRename,
  handleDelete,
  openEnvCreate,
  handleEnvCreate,
  openEnvRename,
  handleEnvRename,
  openEnvDelete,
  handleEnvDelete,
  reload,
} = page;
const renameRules = projectNameRules();
const envRules = environmentNameRules();
</script>

<template>
  <div class="project-page">
    <NSpin v-if="projectsStore.detailLoading && !projectsStore.detail" description="Loading project…" />

    <NSpace v-else-if="projectsStore.detailError" vertical :size="8">
      <NAlert type="error" :show-icon="true">
        {{ projectsStore.detailError }}
      </NAlert>
      <div><NButton size="small" @click="void reload()">Retry</NButton></div>
    </NSpace>

    <template v-else-if="projectsStore.detail">
      <div class="page-head">
        <div class="head-main">
          <ProjectBreadcrumb
            :project-name="projectsStore.detail.name"
            :project-id="projectsStore.detail.id"
          />
          <h1 class="title" :title="projectsStore.detail.name">
            {{ projectsStore.detail.name }}
          </h1>
          <p v-if="projectsStore.detail.description" class="page-desc">
            {{ projectsStore.detail.description }}
          </p>
        </div>
        <div v-if="canWrite" class="page-actions">
          <NButton @click="openRename()">Rename</NButton>
          <NButton @click="deleteOpen = true">
            Delete project
          </NButton>
        </div>
      </div>

      <NTabs v-model:value="tab" type="line" animated class="tabs">
        <NTabPane name="environments" tab="Environments">
          <div class="toolbar">
            <NButton v-if="canWrite" type="primary" @click="openEnvCreate()">
              Add environment
            </NButton>
          </div>

          <NEmpty
            v-if="projectsStore.environments.length === 0"
            description="No environments yet. Add one to start deploying."
          >
            <template v-if="canWrite" #extra>
              <NButton type="primary" @click="openEnvCreate()">
                Add environment
              </NButton>
            </template>
          </NEmpty>

          <NCard v-else class="env-card">
            <div class="table-wrap">
            <table class="env-table">
              <thead>
                <tr>
                  <th scope="col">Environment</th>
                  <th scope="col">Applications</th>
                  <th scope="col">Services</th>
                  <th scope="col">Databases</th>
                  <th scope="col"><span class="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="environment in projectsStore.environments" :key="environment.id">
                  <td class="mono env-name" data-label="Environment" :title="environment.name">{{ environment.name }}</td>
                  <td class="num" data-label="Applications">{{ environment.resource_counts.applications }}</td>
                  <td class="num" data-label="Services">{{ environment.resource_counts.services }}</td>
                  <td class="num" data-label="Databases">{{ environment.resource_counts.databases }}</td>
                  <td class="actions" data-label="Actions">
                    <NSpace :size="8" justify="end" class="action-buttons">
                      <RouterLink
                        :to="{
                          name: 'environment-detail',
                          params: {
                            projectId: projectsStore.detail.id,
                            environmentId: environment.id,
                          },
                        }"
                      >
                        <NButton size="small">Open</NButton>
                      </RouterLink>
                      <template v-if="canWrite">
                        <NButton size="small" @click="openEnvRename(environment)">
                          Rename
                        </NButton>
                        <NButton size="small" @click="openEnvDelete(environment)">
                          Delete
                        </NButton>
                      </template>
                    </NSpace>
                  </td>
                </tr>
              </tbody>
            </table>
            </div>
          </NCard>
        </NTabPane>

        <NTabPane name="variables" tab="Shared variables">
          <SharedVariablesEditor
            :draft="projectVariables.draft.value"
            :loading="projectVariables.loading.value"
            :load-error="projectVariables.loadError.value"
            :save-error="projectVariables.saveError.value"
            :saving="projectVariables.saving.value"
            :save-disabled="projectVariables.saveDisabled.value"
            :can-write="canWrite"
            :stored-secrets="[...projectVariables.storedSecrets.value]"
            :problems="projectVariables.problems.value"
            precedence-hint="Project variables apply to every resource in every environment of this project. An environment variable overrides a project one; an application variable overrides both."
            @update:draft="projectVariables.draft.value = $event"
            @save="void projectVariables.save()"
            @retry="void projectVariables.retry()"
          />
        </NTabPane>
      </NTabs>
    </template>

    <!-- Rename project -->
    <NModal
      v-model:show="renameOpen"
      preset="card"
      title="Rename project"
      style="width: 460px; max-width: 94vw"
    >
      <NForm :model="{ name: renameName }" :rules="renameRules">
        <NSpace vertical :size="12">
          <NFormItem
            label="Name"
            path="name"
            :feedback="renameConflict.feedback.value"
            :validation-status="renameConflict.status.value"
          >
            <NInput
              v-model:value="renameName"
              maxlength="64"
              show-count
              :input-props="{ id: 'project-rename-name', 'aria-label': 'Project name' }"
              @update:value="renameConflict.clear()"
              @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleRename)"
            />
          </NFormItem>
          <NText depth="3">Unique within the team. 1-64 characters.</NText>
          <NAlert v-if="renameError" type="error" :show-icon="true">
            {{ renameError }}
          </NAlert>
        </NSpace>
      </NForm>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="renameOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="renameBusy"
            :disabled="!isProjectNameValid(renameName)"
            @click="void handleRename()"
          >
            Save
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Delete project -->
    <NModal
      v-model:show="deleteOpen"
      preset="card"
      title="Delete project"
      style="width: 460px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          <template v-if="hasResources">
            This project still holds resources. Move or delete every resource
            in every environment first — the backend refuses the delete
            otherwise.
          </template>
          <template v-else>
            This removes the project, its environments and its shared
            variables. This cannot be undone.
          </template>
        </NText>
        <NAlert
          v-if="deleteError"
          type="error"
          :show-icon="true"
        >
          {{ deleteError }}
        </NAlert>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="deleteOpen = false">Cancel</NButton>
          <NButton
            type="error"
            :loading="deleting"
            :disabled="hasResources"
            @click="void handleDelete()"
          >
            Delete project
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Add environment -->
    <NModal
      v-model:show="envCreateOpen"
      preset="card"
      title="Add environment"
      style="width: 460px; max-width: 94vw"
    >
      <NForm :model="{ name: envName }" :rules="envRules">
        <NSpace vertical :size="12">
          <NFormItem
            label="Name"
            path="name"
            :feedback="envConflict.feedback.value"
            :validation-status="envConflict.status.value"
          >
            <NInput
              v-model:value="envName"
              placeholder="staging"
              maxlength="64"
              show-count
              :input-props="{ id: 'environment-create-name', 'aria-label': 'Environment name' }"
              @update:value="envConflict.clear()"
              @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleEnvCreate)"
            />
          </NFormItem>
          <NText depth="3">Unique within the project. 1-64 characters.</NText>
          <NAlert v-if="envError" type="error" :show-icon="true">
            {{ envError }}
          </NAlert>
        </NSpace>
      </NForm>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="envCreateOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="envBusy"
            :disabled="!isEnvironmentNameValid(envName)"
            @click="void handleEnvCreate()"
          >
            Add environment
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Rename environment -->
    <NModal
      :show="envRenameTarget !== null"
      preset="card"
      title="Rename environment"
      style="width: 460px; max-width: 94vw"
      @update:show="(show: boolean) => { if (!show) envRenameTarget = null; }"
    >
      <NForm :model="{ name: envRenameName }" :rules="envRules">
        <NSpace vertical :size="12">
          <NFormItem
            label="Name"
            path="name"
            :feedback="envRenameConflict.feedback.value"
            :validation-status="envRenameConflict.status.value"
          >
            <NInput
              v-model:value="envRenameName"
              maxlength="64"
              show-count
              :input-props="{ id: 'environment-rename-name', 'aria-label': 'Environment name' }"
              @update:value="envRenameConflict.clear()"
              @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleEnvRename)"
            />
          </NFormItem>
          <NAlert v-if="envRenameError" type="error" :show-icon="true">
            {{ envRenameError }}
          </NAlert>
        </NSpace>
      </NForm>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="envRenameTarget = null">Cancel</NButton>
          <NButton
            type="primary"
            :loading="envRenameBusy"
            :disabled="!isEnvironmentNameValid(envRenameName)"
            @click="void handleEnvRename()"
          >
            Save
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <!-- Delete environment -->
    <NModal
      :show="envDeleteTarget !== null"
      preset="card"
      title="Delete environment"
      style="width: 460px; max-width: 94vw"
      @update:show="(show: boolean) => { if (!show) envDeleteTarget = null; }"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          <template v-if="envDeleteTarget && environmentResourceTotal(envDeleteTarget) > 0">
            {{ envDeleteTarget.name }} still holds
            {{ resourceSummary(envDeleteTarget.resource_counts) }}. Move or
            delete them first.
          </template>
          <template v-else>
            This removes the environment
            <span v-if="envDeleteTarget" class="mono">{{ envDeleteTarget.name }}</span>.
            This cannot be undone.
          </template>
        </NText>
        <NAlert v-if="envDeleteError" type="error" :show-icon="true">
          {{ envDeleteError }}
        </NAlert>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="envDeleteTarget = null">Cancel</NButton>
          <NButton
            type="error"
            :loading="envDeleting"
            :disabled="!!envDeleteTarget && environmentResourceTotal(envDeleteTarget) > 0"
            @click="void handleEnvDelete()"
          >
            Delete environment
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.project-page {
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

.toolbar {
  display: flex;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
  flex-wrap: wrap;
}

.table-wrap {
  overflow-x: auto;
}

.env-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-sm);
}

.env-table th,
.env-table td {
  text-align: left;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-soft);
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.env-table th {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--muted);
}

.env-table .num {
  font-family: var(--font-mono);
}

.env-table .actions {
  text-align: right;
  white-space: normal;
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

  /* Narrow: each environment becomes a stacked card so the actions stay
     reachable without horizontal scrolling. */
  .env-table thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }

  .env-table,
  .env-table tbody,
  .env-table tr,
  .env-table td {
    display: block;
    width: auto;
    max-width: none;
  }

  .env-table tr {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }

  .env-table td {
    border-bottom: 0;
    padding: 4px 0;
    white-space: normal;
    overflow: visible;
    text-overflow: clip;
  }

  .env-table td.num::before {
    content: attr(data-label) ": ";
    font-family: var(--font-mono);
    font-size: 11px;
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .env-table .env-name {
    font-size: var(--text-base);
    font-weight: 600;
  }

  .env-table .actions {
    text-align: left;
  }

  .action-buttons {
    justify-content: flex-start !important;
  }
}
</style>
