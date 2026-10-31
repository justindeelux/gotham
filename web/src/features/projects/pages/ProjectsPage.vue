<script setup lang="ts">
import {
  NAlert,
  NButton,
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
import { onUnmounted, ref } from "vue";

import { createVisibleValidation } from "@/features/auth";
import ProjectCard from "@/features/projects/components/ProjectCard.vue";
import { useProjectsPage } from "@/features/projects/composables/useProjectsPage";
import {
  isProjectDescriptionValid,
  isProjectNameValid,
  projectCreateRules,
} from "@/features/projects/schemas/projects";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { activeLocale, i18n, onLocaleChange } from "@/shared/i18n";
import { submitOnEnter } from "@/features/projects/utils/submitOnEnter";

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
const createVisible = createVisibleValidation();
const createRules = createVisible.trackRules(projectCreateRules());
const createFormRef = ref<FormInst | null>(null);

const stopCreateLocaleWatch = onLocaleChange(() => {
  createVisible.refreshVisible(createFormRef);
});

onUnmounted(() => {
  stopCreateLocaleWatch();
});

function t(key: string, params?: Record<string, string | number>): string {
  void activeLocale.value;
  return String(i18n.global.t(key, params ?? {}));
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
        <ProjectCard
          v-for="project in filtered"
          :key="project.id"
          :project="project"
        />
      </div>
    </template>

    <NModal
      v-model:show="createOpen"
      preset="card"
      :title="t('projects.create.title')"
      @after-leave="createVisible.reset()"
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

@container (max-width: 700px) {
  .project-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@container (max-width: 480px) {
  .project-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
