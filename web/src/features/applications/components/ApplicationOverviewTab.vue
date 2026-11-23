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
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { Application, Deployment } from "@/features/applications/api/applications";
import ApplicationDeployPipeline from "@/features/applications/components/ApplicationDeployPipeline.vue";
import ApplicationSourceViewer from "@/features/applications/components/ApplicationSourceViewer.vue";
import ComposeEditor from "@/features/applications/components/ComposeEditor.vue";
import DeploymentHistoryTable from "@/features/applications/components/DeploymentHistoryTable.vue";
import DockerfileEditor from "@/features/applications/components/DockerfileEditor.vue";
import GitPrivateSourcePanel from "@/features/applications/components/GitPrivateSourcePanel.vue";
import WorkingDirPanel from "@/features/applications/components/WorkingDirPanel.vue";
import type { PipelineStep } from "@/features/applications/utils/deployPipeline";
import { commitLineText } from "@/features/applications/utils/deploymentCommit";
import { durationText } from "@/features/applications/utils/deploymentDuration";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  application: Application | null;
  latest: Deployment | null;
  pipelineSteps: PipelineStep[];
  deployments: Deployment[];
  descColumns: number;
  acting: boolean;
  canWrite?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  canWrite: false,
});

const emit = defineEmits<{
  deploy: [];
  "view-all": [];
  "show-logs": [deploymentId: string];
  "open-rollback": [deploymentId: string];
}>();

const { t } = useI18n();

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

const commitLine = computed<string>(() =>
  commitLineText(props.latest?.commit_sha, props.latest?.commit_message),
);

const isCompose = computed<boolean>(() => props.application?.source_type === "compose");

// Only a cloned repository has a folder to pick; a pasted compose document, a
// Dockerfile and an image reference build from what they carry.
const hasRepoSource = computed<boolean>(() => {
  const sourceType = props.application?.source_type;
  return (
    sourceType === "git_public" ||
    sourceType === "git_private" ||
    sourceType === "github_app" ||
    sourceType === "gitlab_app"
  );
});

</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <ApplicationDeployPipeline :latest="props.latest" :steps="props.pipelineSteps" />
    <GitPrivateSourcePanel
      v-if="props.application?.source_type === 'git_private'"
      :application-id="props.application.id"
    />
    <WorkingDirPanel
      v-if="props.application && hasRepoSource"
      :application="props.application"
      :can-write="props.canWrite"
    />
    <NCard v-if="props.application" :title="t('applications.overview.title')">
      <NDescriptions :column="props.descColumns" bordered label-placement="left">
        <NDescriptionsItem :label="t('applications.overview.name')">
          <span class="mono">{{ props.application.name }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.source')">
          <span class="mono">{{ sourceLabel }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem v-if="props.application.source_type === 'image'" :label="t('applications.image.ref')">
          <span class="mono">{{ props.application.image_ref || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem v-else-if="props.application.source_type !== 'dockerfile'" :label="t('applications.overview.branch')">
          <span class="mono">{{ props.application.branch || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem v-if="props.application.source_type !== 'dockerfile' && props.application.source_type !== 'image' && !isCompose" :label="t('applications.overview.buildPack')">
          <span class="mono">{{ props.application.build_pack || t("applications.overview.auto") }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem v-if="isCompose" :label="t('applications.overview.composeService')">
          <span class="mono">{{ props.application.compose_service || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem v-if="isCompose" :label="t('applications.overview.composeFile')">
          <span class="mono">{{ props.application.compose_file || t("applications.overview.composePasted") }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem :label="t('applications.overview.domain')">
          <span class="mono">{{ props.application.primary_domain || "—" }}</span>
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
    <ComposeEditor
      v-if="props.application && props.application.source_type === 'compose'"
      :application="props.application"
      :can-write="props.canWrite"
    />
    <ApplicationSourceViewer
      v-if="props.application"
      :application="props.application"
    />
    <NCard v-if="props.latest" :title="t('applications.overview.deployTitle', { id: props.latest.id.slice(0, 8) })">
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
        <NDescriptionsItem :label="t('applications.overview.commit')">
          <span class="mono" :title="props.latest.commit_sha ?? ''">{{ commitLine }}</span>
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
        class="mono error-prewrap"
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

.error-prewrap {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
