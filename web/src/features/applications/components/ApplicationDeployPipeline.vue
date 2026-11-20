<script setup lang="ts">
import { Box, Check, Clock, Download, Hammer, LoaderCircle, Play, Upload, X } from "@lucide/vue";
import { NIcon } from "naive-ui";
import type { Component } from "vue";
import { useI18n } from "vue-i18n";

import type { Deployment, DeploymentState } from "@/features/applications/api/applications";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import type { PipelineStep } from "@/features/applications/utils/deployPipeline";
import { commitLineText } from "@/features/applications/utils/deploymentCommit";
import { durationText } from "@/features/applications/utils/deploymentDuration";

interface Props {
  latest: Deployment | null;
  steps: PipelineStep[];
}

const props = defineProps<Props>();

const { t } = useI18n();

const stepIcons: Record<DeploymentState, Component> = {
  queued: Clock,
  cloning: Download,
  building: Hammer,
  pushing: Upload,
  starting: Play,
  running: Box,
  failed: X,
};
</script>

<template>
  <div v-if="props.latest" class="pipeline-bar">
    <ol class="pipeline-bar__steps" role="list" :aria-label="t('applications.overview.pipeline')">
      <li
        v-for="step in props.steps"
        :key="step.name"
        class="step"
        :class="step.mood"
        :aria-label="t(`applications.status.${step.name}`)"
        :aria-current="step.mood === 'is-active' ? 'step' : undefined"
      >
        <span class="step__line" aria-hidden="true" />
        <span class="step__node">
          <NIcon :size="14" aria-hidden="true">
            <Check v-if="step.mood === 'is-done'" :stroke-width="2.4" />
            <X v-else-if="step.mood === 'is-failed'" :stroke-width="2.4" />
            <LoaderCircle
              v-else-if="step.mood === 'is-active'"
              class="step__spinner"
              :stroke-width="2"
            />
            <component :is="stepIcons[step.name]" v-else :stroke-width="1.6" />
          </NIcon>
        </span>
        <span class="step__label">{{ t(`applications.status.${step.name}`) }}</span>
      </li>
    </ol>
    <div class="pipeline-bar__meta">
      <div class="pipeline-bar__row">
        <DeploymentStatusTag :state="props.latest.state" />
        <span class="mono meta-id">#{{ props.latest.id.slice(0, 8) }}</span>
        <span class="mono" :title="t('applications.overview.duration')">
          {{ durationText(props.latest) }}
        </span>
      </div>
      <div class="mono meta-commit" :title="props.latest.commit_sha ?? ''">
        {{ commitLineText(props.latest.commit_sha, props.latest.commit_message) }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.pipeline-bar {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  min-height: 56px;
  min-width: 0;
  padding: var(--space-3) var(--space-4);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.pipeline-bar__steps {
  display: flex;
  align-items: center;
  flex: 1 1 auto;
  min-width: 0;
  margin: 0;
  padding: 0;
  list-style: none;
  overflow-x: auto;
}

.step {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 1 0 auto;
  min-width: 0;
}

.step:first-child {
  flex-grow: 0;
}

.step:last-child {
  flex-grow: 0;
}

.step__line {
  flex: 1 1 var(--space-6);
  min-width: var(--space-4);
  height: 1px;
  background: var(--border);
}

.step:first-child .step__line {
  display: none;
}

.step.is-done .step__line {
  background: var(--success);
}

.step.is-active .step__line {
  background: var(--accent);
}

.step__node {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  width: 24px;
  height: 24px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-pill);
  color: var(--meta);
}

.step__label {
  font-size: var(--text-xs);
  font-weight: 500;
  color: var(--muted);
  white-space: nowrap;
}

.step.is-done .step__node {
  background: var(--success);
  border-color: var(--success);
  color: var(--accent-on);
}

.step.is-done .step__label {
  color: var(--fg);
}

.step.is-active .step__node {
  border-color: var(--accent);
  color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
  animation: pipeline-ring 1.6s ease-in-out infinite;
}

.step.is-active .step__label {
  color: var(--fg-2);
  font-weight: 600;
}

.step__spinner {
  animation: pipeline-spin 1s linear infinite;
}

.step.is-failed .step__node {
  background: var(--danger);
  border-color: var(--danger);
  color: var(--accent-on);
}

.step.is-failed .step__label {
  color: var(--danger-ink);
}

.pipeline-bar__meta {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 0 0 auto;
  min-width: 0;
  padding-left: var(--space-4);
  border-left: 1px solid var(--border);
}

.pipeline-bar__row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.mono {
  font-family: var(--font-mono);
  color: var(--muted);
}

.meta-id {
  color: var(--fg);
}

.meta-commit {
  max-width: 34ch;
  overflow: hidden;
  font-size: var(--text-xs);
  white-space: nowrap;
  text-overflow: ellipsis;
}

@keyframes pipeline-ring {
  0%,
  100% {
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  50% {
    box-shadow: 0 0 0 5px var(--accent-soft);
  }
}

@keyframes pipeline-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 900px) {
  .pipeline-bar__meta {
    display: none;
  }

  .step__label {
    display: none;
  }

  .step.is-active .step__label,
  .step.is-failed .step__label {
    display: inline;
  }
}

@media (max-width: 600px) {
  .pipeline-bar__steps {
    justify-content: space-between;
  }

  .step {
    flex: 0 0 auto;
  }

  .step__line {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .step.is-active .step__node {
    animation: none;
  }

  .step__spinner {
    animation: none;
  }
}
</style>
