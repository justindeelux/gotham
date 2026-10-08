<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NSpace,
} from "naive-ui";
import { useI18n } from "vue-i18n";

import type { Application, Deployment } from "@/features/applications/api/applications";
import DeploymentHistoryTable from "@/features/applications/components/DeploymentHistoryTable.vue";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import DockerfileEditor from "@/features/applications/components/DockerfileEditor.vue";
import type { PipelineStep } from "@/features/applications/utils/deployPipeline";
import { durationText } from "@/features/applications/utils/deploymentDuration";
import { relativeTime } from "@/shared/utils/format";
import { computed } from "vue";

interface Props {
  application: Application | null;
  latest: Deployment | null;
  deployments: Deployment[];
  pipelineSteps: PipelineStep[];
  descColumns: number;
  acting: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  deploy: [];
  "view-all": [];
  "show-logs": [deploymentId: string];
  "open-rollback": [deploymentId: string];
}>();

const { t } = useI18n();

/** sourceLabel renders the GS-2 source type with the wizard's labels. */
const sourceLabels: Record<string, string> = {
  git_public: "applications.wizard.sourceGitPublic",
  git_private: "applications.wizard.sourceGitPrivate",
  github_app: "applications.wizard.sourceGithubApp",
  gitlab_app: "applications.wizard.sourceGitlabApp",
  dockerfile: "applications.wizard.sourceDockerfile",
  compose: "applications.wizard.sourceCompose",
  image: "applications.wizard.sourceImage",
};

const sourceLabel = computed<string>(() => {
  const key = props.application ? sourceLabels[props.application.source_type] : undefined;
  return key ? String(t(key)) : (props.application?.source_type ?? "—");
});
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <NCard v-if="props.application" :title="t('applications.overview.title')">
      <NDescriptions :column="props.descColumns" bordered label-placement="left">
        <NDescriptionsItem :label="t('applications.overview.name')">
          <span class="mono">{{ props.application.name }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.source')">
          <span class="mono">{{ sourceLabel }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem v-if="props.application.source_type !== 'dockerfile'" :label="t('applications.overview.branch')">
          <span class="mono">{{ props.application.branch || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem v-if="props.application.source_type !== 'dockerfile'" :label="t('applications.overview.buildPack')">
          <span class="mono">{{ props.application.build_pack || t("applications.overview.auto") }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.domain')">
          <span class="mono">{{ props.application.base_domain || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.port')">
          <span class="mono">{{ props.application.port }}:{{ props.application.host_port }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.node')">
          <span class="mono">{{ props.application.server_name || (props.application.server_id ? props.application.server_id.slice(0, 8) : t("applications.overview.unassigned")) }}</span>
        </NDescriptionsItem>
      </NDescriptions>
    </NCard>
    <DockerfileEditor
      v-if="props.application && props.application.source_type === 'dockerfile'"
      :application="props.application"
    />
    <NCard v-if="props.latest" :title="t('applications.overview.deployTitle', { id: props.latest.id.slice(0, 8) })">
      <template #header-extra>
        <DeploymentStatusTag :state="props.latest.state" />
      </template>
      <div class="pipeline" role="list" :aria-label="t('applications.overview.pipeline')">
        <template v-for="(step, index) in props.pipelineSteps" :key="step.name">
          <span
            v-if="index > 0"
            class="pipeline__arrow"
            aria-hidden="true"
          >→</span>
          <span class="pipeline__node" :class="step.mood" role="listitem">
            <span class="dot" aria-hidden="true" />
            {{ t(`applications.status.${step.name}`) }}
          </span>
        </template>
      </div>
      <NDescriptions :column="props.descColumns" bordered label-placement="left">
        <NDescriptionsItem :label="t('applications.overview.kind')">
          <span class="mono">{{ props.latest.kind }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.image')">
          <span class="mono">{{ props.latest.image_tag || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.registryImage')">
          <span class="mono">{{ props.latest.registry_image || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.container')">
          <span class="mono">{{ props.latest.container_id || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.duration')">
          {{ durationText(props.latest) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.created')">
          {{ relativeTime(props.latest.created_at) }}
        </NDescriptionsItem>
      </NDescriptions>
      <NAlert
        v-if="props.latest.error"
        type="error"
        :show-icon="true"
        style="margin-top: 12px"
      >
        {{ props.latest.error }}
      </NAlert>
    </NCard>
    <NCard v-else :title="t('applications.overview.emptyTitle')">
      <NEmpty :description="t('applications.overview.emptyHint')">
        <template #extra>
          <NButton
            type="primary"
            :loading="props.acting"
            @click="emit('deploy')"
          >
            {{ t("applications.overview.deployNow") }}
          </NButton>
        </template>
      </NEmpty>
    </NCard>

    <NCard :title="t('applications.overview.recent')">
      <template #header-extra>
        <NButton
          v-if="props.deployments.length > 0"
          quaternary
          size="small"
          @click="emit('view-all')"
        >
          {{ t("applications.overview.viewAll") }}
        </NButton>
      </template>
      <DeploymentHistoryTable
        v-if="props.deployments.length > 0"
        :deployments="props.deployments.slice(0, 4)"
        @show-logs="emit('show-logs', $event)"
        @open-rollback="emit('open-rollback', $event)"
      />
      <NEmpty v-else :description="t('applications.overview.noneRecorded')" />
    </NCard>
  </NSpace>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.pipeline {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}

.pipeline__arrow {
  color: var(--meta);
}

.pipeline__node {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 4px 12px;
}

.pipeline__node .dot {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  background: var(--meta);
}

.pipeline__node.is-done {
  color: var(--fg);
}

.pipeline__node.is-done .dot {
  background: var(--success);
}

.pipeline__node.is-active {
  color: var(--fg-2);
  border-color: var(--accent);
}

.pipeline__node.is-active .dot {
  background: var(--accent);
  animation: pipeline-pulse 1.2s ease-in-out infinite;
}

.pipeline__node.is-failed {
  color: var(--danger);
  border-color: var(--danger);
}

.pipeline__node.is-failed .dot {
  background: var(--danger);
}

@keyframes pipeline-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}
</style>
