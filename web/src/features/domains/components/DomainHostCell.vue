<script setup lang="ts">
import { NTag } from "naive-ui";

interface Props {
  host: string;
  reason?: string;
  statusLabel?: string;
  statusType?: "success" | "warning" | "error" | "info" | "default";
  badge?: string;
  badgeType?: "success" | "warning" | "error" | "info" | "default";
}

withDefaults(defineProps<Props>(), {
  reason: "",
  statusLabel: "",
  statusType: "default",
  badge: "",
  badgeType: "info",
});
</script>

<template>
  <div class="domain-host-cell">
    <span class="host-line">
      <span class="mono domain-name">{{ host }}</span>
      <NTag v-if="statusLabel" size="small" :type="statusType">
        {{ statusLabel }}
      </NTag>
      <NTag v-if="badge" size="small" :type="badgeType">{{ badge }}</NTag>
    </span>
    <span v-if="reason" class="domain-diagnostic">{{ reason }}</span>
  </div>
</template>

<style scoped>
.domain-host-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.host-line {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.domain-name {
  overflow-wrap: anywhere;
}

.domain-diagnostic {
  font-size: var(--text-xs);
  color: var(--warn);
  overflow-wrap: anywhere;
}
</style>
