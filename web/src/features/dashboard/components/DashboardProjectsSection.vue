<script setup lang="ts">
import { NAlert, NButton, NSkeleton, NText } from "naive-ui";
import { computed, onMounted } from "vue";
import { RouterLink } from "vue-router";

import ProjectCard from "@/features/projects/components/ProjectCard.vue";
import { useProjectsStore } from "@/features/projects/stores/projects";

const projectLimit = 6;

const store = useProjectsStore();
const visibleProjects = computed(() => store.projects.slice(0, projectLimit));
const showSection = computed<boolean>(
  () => !store.loaded || store.error !== null || store.projects.length > 0,
);

onMounted(() => {
  if (!store.loaded) {
    void store.fetchProjects().catch(() => {
    });
  }
});

function retry(): void {
  void store.fetchProjects().catch(() => {
  });
}
</script>

<template>
  <section v-if="showSection" class="projects-section" data-test="projects">
    <div class="section-title">
      <h2>{{ $t("dashboard.projects.title") }}</h2>
      <RouterLink
        v-if="store.loaded && store.projects.length > 0"
        class="section-link"
        :to="{ name: 'projects' }"
      >
        {{ $t("dashboard.projects.viewAll") }}
      </RouterLink>
    </div>

    <template v-if="!store.loaded && !store.error">
      <NText depth="3" class="meta">{{ $t("dashboard.projects.loading") }}</NText>
      <div class="project-grid" aria-busy="true">
        <NSkeleton v-for="index in 2" :key="index" height="116px" />
      </div>
    </template>

    <NAlert v-else-if="store.error" type="error" :show-icon="true">
      <div class="alert-body">
        <span>{{ store.error }}</span>
        <NButton size="small" @click="retry">
          {{ $t("common.actions.retry") }}
        </NButton>
      </div>
    </NAlert>

    <div v-else class="project-grid">
      <ProjectCard
        v-for="project in visibleProjects"
        :key="project.id"
        :project="project"
      />
    </div>
  </section>
</template>

<style scoped>
.projects-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  min-width: 0;
}

.section-title {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.section-title h2 {
  font-size: var(--text-xl);
  color: var(--fg-2);
  margin: 0;
}

.section-link {
  font-size: var(--text-xs);
}

.meta {
  font-size: var(--text-xs);
}

.alert-body {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.project-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
  gap: var(--space-4);
}

@media (max-width: 640px) {
  .project-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
