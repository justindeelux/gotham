<script setup lang="ts">
import { NTag } from "naive-ui";
import { computed } from "vue";

import type { ServerStatus } from "../api/servers";

type TagType = "default" | "primary" | "info" | "success" | "warning" | "error";

interface Props {
  status: ServerStatus;
  size?: "small" | "medium" | "large";
}

const statusTypes: Record<ServerStatus, TagType> = {
  pending: "default",
  validating: "info",
  ready: "success",
  offline: "warning",
  error: "error",
};

const statusLabels: Record<ServerStatus, string> = {
  pending: "Pending",
  validating: "Validating",
  ready: "Ready",
  offline: "Offline",
  error: "Error",
};

const props = withDefaults(defineProps<Props>(), { size: "small" });

const tagType = computed<TagType>(() => statusTypes[props.status] ?? "default");

const label = computed<string>(() => statusLabels[props.status] ?? props.status);
</script>

<template>
  <NTag :type="tagType" :size="size" round>
    {{ label }}
  </NTag>
</template>
