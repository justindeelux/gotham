<script setup lang="ts">
import { NButton, NCard, NProgress, NTag } from "naive-ui";
import { computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";

import { useTasksStore } from "@/features/tasks/stores/tasks";
import type { TaskStatus } from "@/features/tasks/api/tasks";

const tasksStore = useTasksStore();
const { t } = useI18n();

onMounted(() => {
  tasksStore.connect();
});

/** tagType maps a task status to its Naive UI tag. */
function tagType(status: TaskStatus): "default" | "info" | "success" | "error" {
  switch (status) {
    case "queued":
      return "default";
    case "running":
      return "info";
    case "succeeded":
      return "success";
    case "failed":
      return "error";
  }
}

/** progressOf shows queued work at zero; terminal work is complete. */
function progressOf(status: TaskStatus, progress: number): number {
  if (status === "queued") {
    return 0;
  }
  if (status === "succeeded" || status === "failed") {
    return 100;
  }
  return progress;
}

/** hasLogLink reports whether the card can deep-link to the deploy logs. */
function hasLogLink(task: { appId: string; projectId: string; environmentId: string }): boolean {
  return task.appId !== "" && task.projectId !== "" && task.environmentId !== "";
}

const hasCards = computed<boolean>(() => tasksStore.visible.length > 0);
</script>

<template>
  <div
    v-if="hasCards"
    class="task-cards"
    role="status"
    aria-live="polite"
    :aria-label="t('tasks.card.title')"
  >
    <NCard
      v-for="card in tasksStore.visible"
      :key="card.event.taskId"
      class="task-card"
      size="small"
      :data-task-id="card.event.taskId"
      :data-task-status="card.event.status"
    >
      <div class="task-head">
        <NTag
          :type="tagType(card.event.status)"
          size="small"
          round
        >
          {{ t(`tasks.card.status.${card.event.status}`) }}
        </NTag>
        <span class="task-name">{{ card.event.name }}</span>
        <NButton
          text
          size="tiny"
          class="task-fold"
          :aria-label="card.collapsed ? t('tasks.card.expand') : t('tasks.card.collapse')"
          @click="tasksStore.toggleCollapse(card.event.taskId)"
        >
          {{ card.collapsed ? "+" : "–" }}
        </NButton>
        <NButton
          text
          size="tiny"
          class="task-close"
          :aria-label="t('tasks.card.dismiss')"
          @click="tasksStore.dismiss(card.event.taskId)"
        >
          ✕
        </NButton>
      </div>
      <template v-if="!card.collapsed">
        <div
          v-if="card.event.step"
          class="task-step"
        >
          {{ card.event.step }}
        </div>
        <div
          v-if="card.event.status === 'failed' && card.event.error"
          class="task-error"
        >
          {{ card.event.error }}
        </div>
        <NProgress
          type="line"
          :percentage="progressOf(card.event.status, card.event.progress)"
          :show-indicator="false"
          :status="card.event.status === 'failed' ? 'error' : undefined"
        />
        <div class="task-foot">
          <RouterLink
            v-if="hasLogLink(card.event)"
            class="task-logs"
            :to="{
              name: 'application-detail',
              params: {
                projectId: card.event.projectId,
                environmentId: card.event.environmentId,
                id: card.event.appId,
              },
              query: { logs: card.event.deploymentId },
            }"
          >
            {{ t("tasks.card.viewLogs") }}
          </RouterLink>
        </div>
      </template>
    </NCard>
  </div>
</template>

<style scoped>
.task-cards {
  position: fixed;
  right: var(--space-5);
  bottom: var(--space-5);
  z-index: 90;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  width: min(360px, calc(100vw - var(--space-8)));
  max-height: 60vh;
  overflow-y: auto;
  pointer-events: none;
}

.task-card {
  pointer-events: auto;
}

.task-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.task-name {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-step {
  margin-top: var(--space-1);
  color: var(--muted);
  font-size: 12px;
}

.task-error {
  margin-top: var(--space-1);
  color: var(--danger-ink);
  font-size: 12px;
  word-break: break-word;
}

.task-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-1);
}

.task-logs {
  font-size: 12px;
}
</style>
