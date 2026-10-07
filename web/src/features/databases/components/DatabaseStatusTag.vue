<script setup lang="ts">
import { NTag } from "naive-ui";
import { computed } from "vue";

import type { DatabaseStatus } from "@/features/databases/api/databases";

type TagType = "default" | "primary" | "info" | "success" | "warning" | "error";

interface Props {
  status: DatabaseStatus;
  size?: "small" | "medium" | "large";
}

const statusTypes: Record<DatabaseStatus, TagType> = {
  creating: "info",
  running: "success",
  stopped: "warning",
  error: "error",
  deleting: "default",
};

const statusKeys: Record<DatabaseStatus, string> = {
  creating: "databases.status.creating",
  running: "databases.status.running",
  stopped: "databases.status.stopped",
  error: "databases.status.error",
  deleting: "databases.status.deleting",
};

const statusDots: Record<DatabaseStatus, string> = {
  creating: "dot--creating",
  running: "dot--running",
  stopped: "dot--stopped",
  error: "dot--error",
  deleting: "dot--deleting",
};

/** Transient states pulse; steady states render a static dot. */
const pulsing: ReadonlySet<DatabaseStatus> = new Set(["creating"]);

const props = withDefaults(defineProps<Props>(), { size: "small" });


import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const tagType = computed<TagType>(() => statusTypes[props.status] ?? "default");

/** Unknown statuses render the raw wire value; known ones localize. */
const label = computed<string>(() =>
  props.status in statusKeys
    ? String(t(statusKeys[props.status]))
    : props.status,
);

const dotClass = computed<string>(() => statusDots[props.status] ?? "dot--deleting");

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

.dot--creating {
  background: var(--accent);
}

.dot--running {
  background: var(--success);
}

.dot--stopped {
  background: var(--warn);
}

.dot--error {
  background: var(--danger);
}

.dot--deleting {
  background: var(--muted);
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
