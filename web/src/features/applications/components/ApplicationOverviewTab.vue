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

import type { Application, Deployment } from "@/features/applications/api/applications";
import DeploymentHistoryTable from "@/features/applications/components/DeploymentHistoryTable.vue";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import type { PipelineStep } from "@/features/applications/utils/deployPipeline";
import { durationText } from "@/features/applications/utils/deploymentDuration";
import { relativeTime } from "@/shared/utils/format";

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
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <NCard v-if="props.application" title="Application">
      <NDescriptions :column="props.descColumns" bordered label-placement="left">
        <NDescriptionsItem label="Name">
          <span class="mono">{{ props.application.name }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Branch">
          <span class="mono">{{ props.application.branch || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Build pack">
          <span class="mono">{{ props.application.build_pack || "auto" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Domain">
          <span class="mono">{{ props.application.base_domain || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Port">
          <span class="mono">{{ props.application.port }}:{{ props.application.host_port }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Node">
          <span class="mono">{{ props.application.server_id ? props.application.server_id.slice(0, 8) : "unassigned" }}</span>
        </NDescriptionsItem>
      </NDescriptions>
    </NCard>
    <NCard v-if="props.latest" :title="`Deploy ${props.latest.id.slice(0, 8)}`">
      <template #header-extra>
        <DeploymentStatusTag :state="props.latest.state" />
      </template>
      <div class="pipeline" role="list" aria-label="Deploy pipeline">
        <template v-for="(step, index) in props.pipelineSteps" :key="step.name">
          <span
            v-if="index > 0"
            class="pipeline__arrow"
            aria-hidden="true"
          >→</span>
          <span class="pipeline__node" :class="step.mood" role="listitem">
            <span class="dot" aria-hidden="true" />
            {{ step.name }}
          </span>
        </template>
      </div>
      <NDescriptions :column="props.descColumns" bordered label-placement="left">
        <NDescriptionsItem label="Kind">
          <span class="mono">{{ props.latest.kind }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Image">
          <span class="mono">{{ props.latest.image_tag || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Registry image">
          <span class="mono">{{ props.latest.registry_image || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Container">
          <span class="mono">{{ props.latest.container_id || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Duration">
          {{ durationText(props.latest) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="Created">
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
    <NCard v-else title="No deployments yet">
      <NEmpty description="Queue the first deploy to start the pipeline.">
        <template #extra>
          <NButton
            type="primary"
            :loading="props.acting"
            @click="emit('deploy')"
          >
            Deploy now
          </NButton>
        </template>
      </NEmpty>
    </NCard>

    <NCard title="Recent deployments">
      <template #header-extra>
        <NButton
          v-if="props.deployments.length > 0"
          quaternary
          size="small"
          @click="emit('view-all')"
        >
          View all
        </NButton>
      </template>
      <DeploymentHistoryTable
        v-if="props.deployments.length > 0"
        :deployments="props.deployments.slice(0, 4)"
        @show-logs="emit('show-logs', $event)"
        @open-rollback="emit('open-rollback', $event)"
      />
      <NEmpty v-else description="No deployments recorded for this application." />
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
