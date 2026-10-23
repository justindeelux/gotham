<script setup lang="ts">
import { NTag } from "naive-ui";
import { computed } from "vue";

import type { ToolchainTool } from "@/features/toolchain/api/toolchain";

const props = defineProps<{ tool: ToolchainTool }>();

type BadgeKind = "success" | "warning" | "default";

const badgeKind = computed<BadgeKind>(() => {
  if (!props.tool.installed) {
    return "default";
  }
  return props.tool.version_ok ? "success" : "warning";
});
</script>

<template>
  <NTag :type="badgeKind" size="small" round>
    <template v-if="tool.installed && tool.version_ok && tool.version">
      {{ $t("toolchain.status.installed", { version: tool.version }) }}
    </template>
    <template v-else-if="tool.installed && tool.version_ok">
      {{ $t("toolchain.status.installedUnknown") }}
    </template>
    <template v-else-if="tool.installed">
      {{ $t("toolchain.status.outdated") }}
    </template>
    <template v-else>{{ $t("toolchain.status.missing") }}</template>
  </NTag>
</template>
