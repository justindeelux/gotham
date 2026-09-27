<script setup lang="ts">
import { NTag } from "naive-ui";
import { computed } from "vue";

import type { DeploymentState } from "../api/applications";

type TagType = "default" | "primary" | "info" | "success" | "warning" | "error";

interface Props {
  state: DeploymentState;
  size?: "small" | "medium" | "large";
}

const stateTypes: Record<DeploymentState, TagType> = {
  queued: "default",
  cloning: "info",
  building: "info",
  pushing: "info",
  starting: "warning",
  running: "success",
  failed: "error",
};

const stateDots: Record<DeploymentState, string> = {
  queued: "dot--queued",
  cloning: "dot--active",
  building: "dot--active",
  pushing: "dot--active",
  starting: "dot--starting",
  running: "dot--running",
  failed: "dot--failed",
};

/** In-flight states pulse; terminal states render a static dot. */
const pulsing: ReadonlySet<DeploymentState> = new Set([
  "queued",
  "cloning",
  "building",
  "pushing",
  "starting",
]);

const props = withDefaults(defineProps<Props>(), { size: "small" });

const tagType = computed<TagType>(() => stateTypes[props.state] ?? "default");

const dotClass = computed<string>(() => stateDots[props.state] ?? "dot--queued");

const dotPulse = computed<boolean>(() => pulsing.has(props.state));
</script>

<template>
  <NTag :type="tagType" :size="size" round>
    <span class="status-dot" :class="[dotClass, { 'dot-pulse': dotPulse }]" aria-hidden="true" />
    {{ state }}
  </NTag>
</template>

<style scoped>
.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  margin-right: 6px;
  vertical-align: baseline;
}

.dot--queued {
  background: var(--muted);
}

.dot--active {
  background: var(--accent);
}

.dot--starting {
  background: var(--warn);
}

.dot--running {
  background: var(--success);
}

.dot--failed {
  background: var(--danger);
}

.dot-pulse {
  animation: deploy-status-pulse 1.4s ease-in-out infinite;
}

@keyframes deploy-status-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}
</style>
