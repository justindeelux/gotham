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
import type { FormInst } from "naive-ui";
import { RouterLink, useRoute } from "vue-router";
import { onUnmounted, ref } from "vue";

import { createVisibleValidation } from "@/features/auth";

import ProjectBreadcrumb from "@/features/projects/components/ProjectBreadcrumb.vue";
import ProjectIcon from "@/features/projects/components/ProjectIcon.vue";
import ProjectIconPicker from "@/features/projects/components/ProjectIconPicker.vue";
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
  isProjectDescriptionValid,
  isProjectNameValid,
  projectFormRules,
} from "@/features/projects/schemas/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { activeLocale, i18n, onLocaleChange } from "@/shared/i18n";
import { submitOnEnter } from "@/features/projects/utils/submitOnEnter";

const route = useRoute();
const projectsStore = useProjectsStore();
const page = useProjectPage(() => String(route.params.projectId ?? ""));
const projectVariables = useSharedVariables(() => ({
  kind: "project",
  projectId: String(route.params.projectId ?? ""),
}));
const {
  tab,
  configName,
  configDescription,
  configBusy,
  configError,
  configConflict,
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
  handleConfigSave,
  applyProject,
  handleDelete,
  openEnvCreate,
  handleEnvCreate,
  openEnvRename,
  handleEnvRename,
  openEnvDelete,
  handleEnvDelete,
  reload,
} = page;
const configVisible = createVisibleValidation();
const envCreateVisible = createVisibleValidation();
const envRenameVisible = createVisibleValidation();
const configRules = configVisible.trackRules(projectFormRules());
const envCreateRules = envCreateVisible.trackRules(environmentNameRules());
const envRenameRules = envRenameVisible.trackRules(environmentNameRules());
const configFormRef = ref<FormInst | null>(null);
const envCreateFormRef = ref<FormInst | null>(null);
const envRenameFormRef = ref<FormInst | null>(null);

const stopDetailLocaleWatch = onLocaleChange(() => {
  configVisible.refreshVisible(configFormRef);
  envCreateVisible.refreshVisible(envCreateFormRef);
  envRenameVisible.refreshVisible(envRenameFormRef);
});

onUnmounted(() => {
  stopDetailLocaleWatch();
});

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
}
</script>

<template>
  <div class="project-page">
    <NSpin v-if="projectsStore.detailLoading && !projectsStore.detail" :description="t('projects.detail.loading')" />

    <NSpace v-else-if="projectsStore.detailError" vertical :size="8">
      <NAlert type="error" :show-icon="true">
        {{ projectsStore.detailError }}
      </NAlert>
      <div><NButton size="small" @click="void reload()">{{ t("common.actions.retry") }}</NButton></div>
    </NSpace>

    <template v-else-if="projectsStore.detail">
      <div class="page-head">
        <div class="head-main">
          <ProjectBreadcrumb
            :project-name="projectsStore.detail.name"
            :project-id="projectsStore.detail.id"
          />
          <div class="title-row">
            <ProjectIcon :project="projectsStore.detail" size="lg" />
            <h1 class="title" :title="projectsStore.detail.name">
              {{ projectsStore.detail.name }}
            </h1>
          </div>
          <p v-if="projectsStore.detail.description" class="page-desc">
            {{ projectsStore.detail.description }}
          </p>
        </div>
      </div>

      <NTabs v-model:value="tab" type="line" animated class="tabs">
        <NTabPane name="environments" :tab="t('projects.detail.tabs.environments')">
          <div class="toolbar">
            <NButton v-if="canWrite" type="primary" @click="openEnvCreate()">
              {{ t("projects.environments.add") }}
            </NButton>
          </div>

          <NEmpty
            v-if="projectsStore.environments.length === 0"
            :description="t('projects.detail.noEnvironments')"
          >
            <template v-if="canWrite" #extra>
              <NButton type="primary" @click="openEnvCreate()">
                {{ t("projects.environments.add") }}
              </NButton>
            </template>
          </NEmpty>

          <NCard v-else class="env-card">
            <div class="table-wrap">
            <table class="env-table">
              <thead>
                <tr>
                  <th scope="col">{{ t("projects.detail.table.environment") }}</th>
                  <th scope="col">{{ t("projects.detail.table.applications") }}</th>
                  <th scope="col">{{ t("projects.detail.table.services") }}</th>
                  <th scope="col">{{ t("projects.detail.table.databases") }}</th>
                  <th scope="col"><span class="sr-only">{{ t("projects.detail.table.actions") }}</span></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="environment in projectsStore.environments" :key="environment.id">
                  <td class="mono env-name" :data-label="t('projects.detail.table.environment')" :title="environment.name"><RouterLink
                    class="env-link"
                    :to="{
                      name: 'environment-detail',
                      params: {
                        projectId: projectsStore.detail.id,
                        environmentId: environment.id,
                      },
                    }"
                  >{{ environment.name }}</RouterLink></td>
                  <td class="num" :data-label="t('projects.detail.table.applications')">{{ environment.resource_counts.applications }}</td>
                  <td class="num" :data-label="t('projects.detail.table.services')">{{ environment.resource_counts.services }}</td>
                  <td class="num" :data-label="t('projects.detail.table.databases')">{{ environment.resource_counts.databases }}</td>
                  <td class="actions" :data-label="t('projects.detail.table.actions')">
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
                        <NButton size="small">{{ t("projects.detail.open") }}</NButton>
                      </RouterLink>
                      <template v-if="canWrite">
                        <NButton size="small" @click="openEnvRename(environment)">
                          {{ t("projects.detail.rename") }}
                        </NButton>
                        <NButton size="small" @click="openEnvDelete(environment)">
                          {{ t("projects.detail.delete") }}
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

        <NTabPane
          v-if="canWrite"
          name="configuration"
          :tab="t('projects.detail.tabs.configuration')"
        >
          <NSpace vertical :size="16">
            <NCard :title="t('projects.configuration.generalTitle')">
              <NForm
                ref="configFormRef"
                :model="{ name: configName, description: configDescription }"
                :rules="configRules"
              >
                <NSpace vertical :size="12">
                  <NFormItem
                    :label="t('projects.configuration.nameLabel')"
                    path="name"
                    :feedback="configConflict.feedback.value"
                    :validation-status="configConflict.status.value"
                  >
                    <NInput
                      v-model:value="configName"
                      maxlength="64"
                      show-count
                      :input-props="{ id: 'project-config-name', 'aria-label': t('projects.configuration.nameAria') }"
                      @update:value="configConflict.clear()"
                      @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleConfigSave)"
                    />
                  </NFormItem>
                  <NText depth="3">{{ t("projects.configuration.nameHint") }}</NText>
                  <NFormItem
                    :label="t('projects.configuration.descriptionLabel')"
                    path="description"
                  >
                    <NInput
                      v-model:value="configDescription"
                      type="textarea"
                      maxlength="500"
                      show-count
                      :autosize="{ minRows: 2, maxRows: 5 }"
                      :input-props="{ id: 'project-config-description', 'aria-label': t('projects.configuration.descriptionAria') }"
                    />
                  </NFormItem>
                  <ProjectIconPicker
                    :project="projectsStore.detail"
                    @updated="applyProject"
                  />
                  <NAlert v-if="configError" type="error" :show-icon="true">
                    {{ configError }}
                  </NAlert>
                  <NSpace>
                    <NButton
                      type="primary"
                      :loading="configBusy"
                      :disabled="!isProjectNameValid(configName) || !isProjectDescriptionValid(configDescription)"
                      @click="void handleConfigSave()"
                    >
                      {{ t("common.actions.save") }}
                    </NButton>
                  </NSpace>
                </NSpace>
              </NForm>
            </NCard>

            <NCard :title="t('projects.configuration.dangerTitle')">
              <NSpace vertical :size="12">
                <NText depth="3">{{ t("projects.configuration.dangerHint") }}</NText>
                <NSpace>
                  <NButton type="error" ghost @click="deleteOpen = true">
                    {{ t("projects.delete.submit") }}
                  </NButton>
                </NSpace>
              </NSpace>
            </NCard>
          </NSpace>
        </NTabPane>

        <NTabPane name="variables" :tab="t('projects.detail.tabs.variables')">
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
            :precedence-hint="t('projects.variables.projectPrecedence')"
            @update:draft="projectVariables.draft.value = $event"
            @save="void projectVariables.save()"
            @retry="void projectVariables.retry()"
          />
        </NTabPane>
      </NTabs>
    </template>

    <NModal
      v-model:show="deleteOpen"
      preset="card"
      :title="t('projects.delete.title')"
      style="width: 460px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          <template v-if="hasResources">
            {{ t("projects.delete.blocked") }}
          </template>
          <template v-else>
            {{ t("projects.delete.confirm") }}
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
          <NButton @click="deleteOpen = false">{{ t("common.actions.cancel") }}</NButton>
          <NButton
            type="error"
            :loading="deleting"
            :disabled="hasResources"
            @click="void handleDelete()"
          >
            {{ t("projects.delete.submit") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="envCreateOpen"
      preset="card"
      :title="t('projects.environments.createTitle')"
      @after-leave="envCreateVisible.reset()"
      style="width: 460px; max-width: 94vw"
    >
      <NForm ref="envCreateFormRef" :model="{ name: envName }" :rules="envCreateRules">
        <NSpace vertical :size="12">
          <NFormItem
            :label="t('projects.environments.nameLabel')"
            path="name"
            :feedback="envConflict.feedback.value"
            :validation-status="envConflict.status.value"
          >
            <NInput
              v-model:value="envName"
              :placeholder="t('projects.environments.namePlaceholder')"
              maxlength="64"
              show-count
              :input-props="{ id: 'environment-create-name', 'aria-label': t('projects.environments.nameAria') }"
              @update:value="envConflict.clear()"
              @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, handleEnvCreate)"
            />
          </NFormItem>
          <NText depth="3">{{ t("projects.environments.nameHint") }}</NText>
          <NAlert v-if="envError" type="error" :show-icon="true">
            {{ envError }}
          </NAlert>
        </NSpace>
      </NForm>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="envCreateOpen = false">{{ t("common.actions.cancel") }}</NButton>
          <NButton
            type="primary"
            :loading="envBusy"
            :disabled="!isEnvironmentNameValid(envName)"
            @click="void handleEnvCreate()"
          >
            {{ t("projects.environments.add") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="envRenameTarget !== null"
      preset="card"
      :title="t('projects.environments.renameTitle')"
      @after-leave="envRenameVisible.reset()"
      style="width: 460px; max-width: 94vw"
      @update:show="(show: boolean) => { if (!show) envRenameTarget = null; }"
    >
      <NForm ref="envRenameFormRef" :model="{ name: envRenameName }" :rules="envRenameRules">
        <NSpace vertical :size="12">
          <NFormItem
            :label="t('projects.environments.nameLabel')"
            path="name"
            :feedback="envRenameConflict.feedback.value"
            :validation-status="envRenameConflict.status.value"
          >
            <NInput
              v-model:value="envRenameName"
              maxlength="64"
              show-count
              :input-props="{ id: 'environment-rename-name', 'aria-label': t('projects.environments.nameAria') }"
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
          <NButton @click="envRenameTarget = null">{{ t("common.actions.cancel") }}</NButton>
          <NButton
            type="primary"
            :loading="envRenameBusy"
            :disabled="!isEnvironmentNameValid(envRenameName)"
            @click="void handleEnvRename()"
          >
            {{ t("common.actions.save") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="envDeleteTarget !== null"
      preset="card"
      :title="t('projects.environments.deleteTitle')"
      style="width: 460px; max-width: 94vw"
      @update:show="(show: boolean) => { if (!show) envDeleteTarget = null; }"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          <template v-if="envDeleteTarget && environmentResourceTotal(envDeleteTarget) > 0">
            {{ t("projects.environments.deleteBlocked", { name: envDeleteTarget.name, summary: resourceSummary(envDeleteTarget.resource_counts) }) }}
          </template>
          <template v-else>
            {{ t("projects.environments.deleteConfirm", { name: envDeleteTarget?.name ?? "" }) }}
          </template>
        </NText>
        <NAlert v-if="envDeleteError" type="error" :show-icon="true">
          {{ envDeleteError }}
        </NAlert>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="envDeleteTarget = null">{{ t("common.actions.cancel") }}</NButton>
          <NButton
            type="error"
            :loading="envDeleting"
            :disabled="!!envDeleteTarget && environmentResourceTotal(envDeleteTarget) > 0"
            @click="void handleEnvDelete()"
          >
            {{ t("projects.environments.deleteSubmit") }}
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

.title-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  margin-bottom: var(--space-2);
}

.title {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
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

.env-link:hover {
  text-decoration: underline;
  text-underline-offset: 2px;
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
