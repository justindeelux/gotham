<script setup lang="ts">
import { NTag } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { ServerStatus } from "@/features/servers/api/servers";

type TagType = "default" | "primary" | "info" | "success" | "warning" | "error";

interface Props {
  status: ServerStatus;
  size?: "small" | "medium" | "large";
}

const statusTypes: Record<ServerStatus, TagType> = {
  pending: "default",
  validating: "info",
  ready: "success",
  offline: "error",
  error: "error",
};

/**
 * statusDots maps each lifecycle state to its dot color modifier. The Server
 * type exposes no agent-version field, so there is no "agent update needed"
 * state here — it is omitted, never invented.
 */
const statusDots: Record<ServerStatus, string> = {
  pending: "dot--pending",
  validating: "dot--validating",
  ready: "dot--ready",
  offline: "dot--offline",
  error: "dot--error",
};

/** Transient states pulse; steady states render a static dot. */
const pulsing: ReadonlySet<ServerStatus> = new Set(["validating", "offline"]);

const props = withDefaults(defineProps<Props>(), { size: "small" });
const { t, te } = useI18n();

const tagType = computed<TagType>(() => statusTypes[props.status] ?? "default");

const statusKey = computed<string>(() => `servers.status.${props.status}`);

const label = computed<string>(() =>
  te(statusKey.value) ? t(statusKey.value) : props.status,
);

const dotClass = computed<string>(() => statusDots[props.status] ?? "dot--pending");

const dotPulse = computed<boolean>(() => pulsing.has(props.status));
</script>

<template>
  <NTag :type="tagType" :size="size" round>
    <span class="status-dot" :class="[dotClass, { 'dot-pulse': dotPulse }]" aria-hidden="true" />
    {{ label }}
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

.dot--pending {
  background: var(--muted);
}

.dot--validating {
  background: var(--accent);
}

.dot--ready {
  background: var(--success);
}

.dot--offline,
.dot--error {
  background: var(--danger);
}

.dot-pulse {
  animation: status-pulse 1.4s ease-in-out infinite;
}

@keyframes status-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}
</style>
